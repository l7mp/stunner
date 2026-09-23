// Package turn implements the Server of the TURN-* listeners over pion/turn: it binds the
// listener sockets, wires the authentication, permission, quota and event handlers into pion,
// and hands pion the listener's relay through the RelayAddressGenerator adapter.
package turn

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/netconn/classify"
	"github.com/l7mp/stunner/v2/internal/netconn/socketpool"
	"github.com/l7mp/stunner/v2/internal/netconn/wire"
	"github.com/l7mp/stunner/v2/internal/relay"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	"github.com/l7mp/stunner/v2/internal/util"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// Server wraps a pion/turn server bound to a listener context. The server is created running:
// NewServer binds the transport sockets and starts pion, Close tears everything down.
type Server struct {
	*turn.Server
	listener string
	proto    stnrv1.ListenerProtocol
	log      logging.LeveledLogger
}

// newTLSConfig builds the TLS configuration of a TURN-TLS listener from its config and
// certificate. The open-source build serves the default TLS settings whatever PQC mode the
// listener asks for; a build with the PQC feature replaces the constructor and honours the mode.
var newTLSConfig = func(_ *objruntime.Runtime, conf *stnrv1.ListenerConfig, cert tls.Certificate, log logging.LeveledLogger) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
}

// NewServer starts the TURN server for a listener context on the given TURN-* protocol.
func NewServer(listener string, proto stnrv1.ListenerProtocol, rt *objruntime.Runtime) (*Server, error) {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	log := rt.Logger.NewLogger(fmt.Sprintf("listener-%s", listener))

	s := &Server{
		listener: listener,
		proto:    proto,
		log:      log,
	}
	s.log.Debugf("TURN server %s (re)starting", listener)

	permissionHandler := NewPermissionHandler(listener, rt, log)
	generator := relayAddressGenerator{relay.New(listener, rt)}
	// Empty host is the unspecified address: on dual-stack hosts (Linux default
	// with net.ipv6.bindv6only=0) ":port" binds a single socket reachable via
	// both IPv4 and IPv6, while on single-family hosts it binds the available
	// family. Hardcoding "0.0.0.0" would be IPv4-only and fail on IPv6-only
	// clusters.
	addr := net.JoinHostPort("", strconv.Itoa(conf.Port))

	// Listener sockets carry clients, not peers: every client is in the listener's own class,
	// and the sockets are accounted under the listener.
	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig
	var ln net.Listener
	switch s.proto {
	case stnrv1.ListenerProtocolTURNUDP:
		socks, err := socketpool.ListenPacket(rt.Net, "udp", addr, rt.UdpThreadNum)
		if err != nil {
			return nil, err
		}
		s.log.Infof("setting up UDP listener socket pool at %s with %d sockets", addr, len(socks))
		for _, sock := range socks {
			c := classify.NewPacketConn(wire.Direct(sock), classify.Const(listener))
			pConns = append(pConns, turn.PacketConnConfig{
				PacketConn:            account.PacketConn(rt.Telemetry, c, listener, telemetry.ListenerType),
				RelayAddressGenerator: generator,
				PermissionHandler:     permissionHandler,
			})
		}

	case stnrv1.ListenerProtocolTURNTCP:
		s.log.Debugf("setting up TCP listener at %s", addr)
		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}
		ln = tcpListener

	case stnrv1.ListenerProtocolTURNTLS:
		s.log.Debugf("setting up TLS/TCP listener at %s", addr)
		cer, err := tls.X509KeyPair([]byte(conf.Cert), []byte(conf.Key))
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s", addr, err)
		}
		tlsListener, err := tls.Listen("tcp", addr, newTLSConfig(rt, conf, cer, s.log))
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
		}
		ln = tlsListener

	case stnrv1.ListenerProtocolTURNDTLS:
		s.log.Debugf("setting up DTLS/UDP listener at %s", addr)
		cer, err := tls.X509KeyPair([]byte(conf.Cert), []byte(conf.Key))
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating DTLS listener at %s: %s", addr, err)
		}
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DTLS listener address %s: %s", addr, err)
		}
		dtlsListener, err := dtls.ListenWithOptions("udp", udpAddr, dtls.WithCertificates(cer))
		if err != nil {
			return nil, fmt.Errorf("failed to create DTLS listener at %s: %s", addr, err)
		}
		ln = dtlsListener

	default:
		return nil, fmt.Errorf("internal error: unknown listener protocol %q", s.proto.String())
	}
	if ln != nil {
		c := classify.NewListener(ln, classify.Const(listener), nil)
		lConns = append(lConns, turn.ListenerConfig{
			Listener:              account.Listener(rt.Telemetry, c, telemetry.ListenerType),
			RelayAddressGenerator: generator,
			PermissionHandler:     permissionHandler,
		})
	}

	q := NewQuotaHandler(rt)
	auth := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv1.AuthConfig)
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:             auth.Realm,
		AuthHandler:       NewAuthHandler(rt, log),
		EventHandler:      NewEventHandler(listener, s.proto, rt, log, q),
		QuotaHandler:      q.QuotaHandler(),
		PacketConnConfigs: pConns,
		ListenerConfigs:   lConns,
		LoggerFactory:     rt.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot set up TURN server for listener %s: %w", listener, err)
	}
	s.Server = server
	log.Infof("listener %s: TURN server running", listener)
	return s, nil
}

// Close shuts down the TURN server and its underlying transport listeners; pion closes every
// socket it was given.
func (s *Server) Close() error {
	s.log.Tracef("closing %s listener %s", s.proto.String(), s.listener)
	if s.Server == nil {
		return nil
	}
	err := s.Server.Close()
	s.Server = nil
	if err != nil && !util.IsClosedErr(err) && !strings.Contains(err.Error(), "already closed") {
		return err
	}
	return nil
}
