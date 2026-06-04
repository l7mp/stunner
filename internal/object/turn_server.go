package object

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// TURNServer wraps a pion/turn server bound to a Listener. The Listener is held as a named field
// (not embedded) because both *turn.Server and *Listener carry AllocationCount and embedding both
// would shadow the underlying server's method.
type TURNServer struct {
	*turn.Server
	listener *Listener
	name     string
	proto    stnrv1.ListenerProtocol
	Conns    []any // either a set of turn.ListenerConfigs or turn.PacketConnConfigs
	log      logging.LeveledLogger
}

// NewTURNServer (re)starts the TURN server for a listener. The handler callbacks consult the
// Registry at request time (via the listener handle) for cross-references like the live Auth
// configuration and the cluster routing tables.
func NewTURNServer(l *Listener) (*TURNServer, error) {
	s := &TURNServer{
		listener: l,
		name:     l.Name,
		proto:    l.Proto,
		log:      l.log,
	}
	s.log.Debugf("TURN server %s (re)starting", s.name)

	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig

	permissionHandler := NewTURNPermissionHandler(l)
	relay := NewTURNRelay(l)

	addr := fmt.Sprintf("0.0.0.0:%d", l.Port)

	switch s.proto {
	case stnrv1.ListenerProtocolTURNUDP:
		socketPool := util.NewPacketConnPool(s.name, l.Net, l.udpThreadNum, l.telemetry)
		s.log.Infof("setting up UDP listener socket pool at %s with %d readloop threads",
			addr, socketPool.Size())
		conns, err := socketPool.ListenPacket("udp4", addr)
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
		tcpListener = telemetry.NewListener(tcpListener, s.name, telemetry.ListenerType, l.telemetry)
		conn := turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}
		lConns = append(lConns, conn)
		s.Conns = append(s.Conns, conn)

	case stnrv1.ListenerProtocolTURNTLS:
		s.log.Debugf("setting up TLS/TCP listener at %s", addr)
		cer, err := tls.X509KeyPair(l.Cert, l.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s",
				addr, err)
		}
		tlsListener, err := tls.Listen("tcp", addr, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cer},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
		}
		tlsListener = telemetry.NewListener(tlsListener, s.name, telemetry.ListenerType, l.telemetry)
		conn := turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}
		lConns = append(lConns, conn)
		s.Conns = append(s.Conns, conn)

	case stnrv1.ListenerProtocolTURNDTLS:
		s.log.Debugf("setting up DTLS/UDP listener at %s", addr)
		cer, err := tls.X509KeyPair(l.Cert, l.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating DTLS listener at %s: %s",
				addr, err)
		}
		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DTLS listener address %s: %s", addr, err)
		}
		dtlsListener, err := dtls.ListenWithOptions("udp4", udpAddr,
			dtls.WithCertificates(cer),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create DTLS listener at %s: %s", addr, err)
		}
		dtlsListener = telemetry.NewListener(dtlsListener, s.name, telemetry.ListenerType, l.telemetry)
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

	if len(pConns) == 0 && len(lConns) == 0 {
		return nil, nil
	}

	q := NewTURNQuotaHandler(l)
	o := l.getOffload()
	if o == nil {
		o = &offloadHandlerStub{}
	}
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:             l.Realm,
		AuthHandler:       NewTURNAuthHandler(l),
		EventHandler:      NewTURNEventHandler(l, q, o),
		QuotaHandler:      q.QuotaHandler(),
		PacketConnConfigs: pConns,
		ListenerConfigs:   lConns,
		LoggerFactory:     l.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot set up TURN server for listener %s: %w", l.Name, err)
	}
	s.Server = server
	l.log.Infof("listener %s: TURN server running", l.Name)
	return s, nil
}

// Close shuts down the TURN server and its underlying transport listeners. Satisfies the Server
// interface consumed by Listener.
func (s *TURNServer) Close() error {
	s.log.Tracef("closing %s listener at %s", s.proto.String(), s.listener.Addr)
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
