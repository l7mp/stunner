package stunner

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/pion/dtls/v3"
	"github.com/pion/turn/v4"
	"golang.org/x/time/rate"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// Number of log events per second reported at ERROR, WARN and INFO loglevel (logging at DEBUG and
// TRACE levels is not rate-limited).
var LogRateLimit rate.Limit = 1.0

// Burst size for rate-limited logging at ERROR, WARN and INFO loglevel (logging at DEBUG and TRACE
// levels is not rate-limited).
var LogBurst = 3

// Start will start the TURN server that belongs to  a listener.
func (s *Stunner) StartServer(l *object.Listener) error {
	s.log.Infof("listener %s (re)starting", l.String())

	// start listeners
	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig

	relay := NewRelayGen(l, s.telemetry, s.logger)
	relay.PortRangeChecker = s.GenPortRangeChecker(relay)

	permissionHandler := s.NewPermissionHandler(l)

	addr := fmt.Sprintf("0.0.0.0:%d", l.Port)

	switch l.Proto {
	case stnrv1.ListenerProtocolTURNUDP:
		socketPool := util.NewPacketConnPool(l.Name, l.Net, s.udpThreadNum, s.telemetry)

		s.log.Infof("setting up UDP listener socket pool at %s with %d readloop threads",
			addr, socketPool.Size())
		conns, err := socketPool.Make("udp", addr)
		if err != nil {
			return err
		}

		for _, c := range conns {
			conn := turn.PacketConnConfig{
				PacketConn:            c,
				RelayAddressGenerator: relay,
				PermissionHandler:     permissionHandler,
			}

			l.Conns = append(l.Conns, conn)
			pConns = append(pConns, conn)
		}

	case stnrv1.ListenerProtocolTURNTCP:
		s.log.Debugf("setting up TCP listener at %s", addr)

		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}

		tcpListener = telemetry.NewListener(tcpListener, l.Name, telemetry.ListenerType, s.telemetry)

		conn := turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

		// cannot test this on vnet, no TLS in vnet.Net
	case stnrv1.ListenerProtocolTURNTLS:
		s.log.Debugf("setting up TLS/TCP listener at %s", addr)

		cer, err := tls.X509KeyPair(l.Cert, l.Key)
		if err != nil {
			return fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s",
				addr, err)
		}
		tlsListener, err := tls.Listen("tcp", addr, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cer},
		})
		if err != nil {
			return fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
		}

		tlsListener = telemetry.NewListener(tlsListener, l.Name, telemetry.ListenerType, s.telemetry)

		conn := turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

	case stnrv1.ListenerProtocolTURNDTLS:
		s.log.Debugf("setting up DTLS/UDP listener at %s", addr)

		cer, err := tls.X509KeyPair(l.Cert, l.Key)
		if err != nil {
			return fmt.Errorf("cannot load cert/key pair for creating DTLS listener at %s: %s",
				addr, err)
		}

		// for some reason dtls.Listen requires a UDPAddr and not an addr string
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return fmt.Errorf("failed to parse DTLS listener address %s: %s", addr, err)
		}
		dtlsListener, err := dtls.Listen("udp", udpAddr, &dtls.Config{
			Certificates: []tls.Certificate{cer},
			// ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		})
		if err != nil {
			return fmt.Errorf("failed to create DTLS listener at %s: %s", addr, err)
		}

		dtlsListener = telemetry.NewListener(dtlsListener, l.Name, telemetry.ListenerType, s.telemetry)

		conn := turn.ListenerConfig{
			Listener:              dtlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

	default:
		return fmt.Errorf("internal error: unknown listener protocol %q", l.Proto.String())
	}

	// start the TURN server if there are actual listeners configured
	if len(pConns) == 0 && len(lConns) == 0 {
		l.Server = nil
		return nil
	}

	t, err := turn.NewServer(turn.ServerConfig{
		Realm:             s.GetRealm(),
		AuthHandler:       s.NewAuthHandler(),
		PacketConnConfigs: pConns,
		ListenerConfigs:   lConns,
		LoggerFactory:     logger.NewRateLimitedLoggerFactory(s.logger, LogRateLimit, LogBurst),
	})
	if err != nil {
		return fmt.Errorf("cannot set up TURN server for listener %s: %w",
			l.Name, err)
	}
	l.Server = t

	s.log.Infof("listener %s: TURN server running", l.Name)

	return nil
}
