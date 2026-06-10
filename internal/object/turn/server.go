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

	objruntime "github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Server wraps a pion/turn server bound to a listener context.
type Server struct {
	*turn.Server
	runtime  *objruntime.Runtime
	listener string
	name     string
	proto    stnrv1.ListenerProtocol
	Conns    []any
	log      logging.LeveledLogger
}

// NewServer starts the TURN server for a listener context.
func NewServer(listener string, rt *objruntime.Runtime, offload OffloadHandler) (*Server, error) {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	proto, err := stnrv1.NewListenerProtocol(conf.Protocol)
	if err != nil {
		panic(fmt.Sprintf("turn: invalid listener protocol for %q: %s", listener, err.Error()))
	}
	log := rt.Logger.NewLogger(fmt.Sprintf("listener-%s", listener))

	s := &Server{
		runtime:  rt,
		listener: listener,
		name:     listener,
		proto:    proto,
		log:      log,
	}
	s.log.Debugf("TURN server %s (re)starting", s.name)

	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig

	permissionHandler := NewPermissionHandler(listener, rt, log)
	relay := NewRelay(listener, rt)
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(conf.Port))

	switch s.proto {
	case stnrv1.ListenerProtocolTURNUDP:
		socketPool := util.NewPacketConnPool(s.name, rt.Net, rt.UdpThreadNum, rt.Telemetry)
		s.log.Infof("setting up UDP listener socket pool at %s with %d readloop threads",
			addr, socketPool.Size())
		conns, err := socketPool.ListenPacket("udp", addr)
		if err != nil {
			return nil, err
		}
		for _, c := range conns {
			conn := turn.PacketConnConfig{
				PacketConn:            c,
				RelayAddressGenerator: relay,
				PermissionHandler:     permissionHandler,
			}
			s.Conns = append(s.Conns, conn)
			pConns = append(pConns, conn)
		}

	case stnrv1.ListenerProtocolTURNTCP:
		s.log.Debugf("setting up TCP listener at %s", addr)
		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}
		tcpListener = telemetry.NewListener(tcpListener, s.name, telemetry.ListenerType, rt.Telemetry)
		conn := turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}
		lConns = append(lConns, conn)
		s.Conns = append(s.Conns, conn)

	case stnrv1.ListenerProtocolTURNTLS:
		s.log.Debugf("setting up TLS/TCP listener at %s", addr)
		cer, err := tls.X509KeyPair([]byte(conf.Cert), []byte(conf.Key))
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s", addr, err)
		}
		tlsListener, err := tls.Listen("tcp", addr, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cer},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
		}
		tlsListener = telemetry.NewListener(tlsListener, s.name, telemetry.ListenerType, rt.Telemetry)
		conn := turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}
		lConns = append(lConns, conn)
		s.Conns = append(s.Conns, conn)

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
		dtlsListener = telemetry.NewListener(dtlsListener, s.name, telemetry.ListenerType, rt.Telemetry)
		conn := turn.ListenerConfig{
			Listener:              dtlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}
		lConns = append(lConns, conn)
		s.Conns = append(s.Conns, conn)

	default:
		return nil, fmt.Errorf("internal error: unknown listener protocol %q", s.proto.String())
	}

	q := NewQuotaHandler(rt)
	auth := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv1.AuthConfig)
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:             auth.Realm,
		AuthHandler:       NewAuthHandler(rt, log),
		EventHandler:      NewEventHandler(listener, rt, log, q, offload),
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

// Start is a no-op because the TURN server is fully initialized by NewServer.
func (s *Server) Start() error { return nil }

// Close shuts down the TURN server and its underlying transport listeners.
func (s *Server) Close() error {
	conf := s.runtime.GetConfig(objruntime.TypeListener, s.listener).(*stnrv1.ListenerConfig)
	s.log.Tracef("closing %s listener at %s", s.proto.String(), conf.Addr)
	if s.Server != nil {
		if err := s.Server.Close(); err != nil && !util.IsClosedErr(err) && !strings.Contains(err.Error(), "already closed") {
			return err
		}
	}
	s.Server = nil

	for _, c := range s.Conns {
		switch s.proto {
		case stnrv1.ListenerProtocolTURNUDP:
			conn, ok := c.(turn.PacketConnConfig)
			if !ok {
				continue
			}
			_ = conn.PacketConn.Close()
		case stnrv1.ListenerProtocolTURNTCP, stnrv1.ListenerProtocolTURNTLS, stnrv1.ListenerProtocolTURNDTLS:
			conn, ok := c.(turn.ListenerConfig)
			if !ok {
				continue
			}
			_ = conn.Listener.Close()
		}
	}
	s.Conns = []any{}
	return nil
}
