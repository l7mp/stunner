package stunner

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/pion/dtls/v2"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Start will start the TURN server that belongs to  a listener.
func (s *Stunner) StartServer(l *object.Listener) error {
	s.log.Infof("listener %s (re)starting", l.String())

	// start listeners
	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig

	// listen on all IPs, relay to the listener address
	relay := &telemetry.RelayAddressGenerator{
		Name:         l.Name,
		RelayAddress: l.Addr,
		Address:      "0.0.0.0",
		MinPort:      uint16(l.MinPort),
		MaxPort:      uint16(l.MaxPort),
		Net:          l.Net,
	}

	permissionHandler := s.NewPermissionHandler(l)

	addr := fmt.Sprintf("0.0.0.0:%d", l.Port)

	switch l.Proto {
	case v1alpha1.ListenerProtocolTURNUDP:
		socketPool := util.NewPacketConnPool(l.Net, s.udpThreadNum)

		s.log.Infof("setting up UDP listener socket pool at %s with %d readloop threads",
			addr, socketPool.Size())
		conns, err := socketPool.Make("udp", addr)
		if err != nil {
			return err
		}

		for _, c := range conns {
			udpListener := telemetry.NewPacketConn(c, l.Name, telemetry.ListenerType)
			conn := turn.PacketConnConfig{
				PacketConn:            udpListener,
				RelayAddressGenerator: relay,
				PermissionHandler:     permissionHandler,
			}

			l.Conns = append(l.Conns, conn)
			pConns = append(pConns, conn)
		}

	case v1alpha1.ListenerProtocolTURNTCP:
		s.log.Debugf("setting up TCP listener at %s", addr)

		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}

		tcpListener = telemetry.NewListener(tcpListener, l.Name, telemetry.ListenerType)

		conn := turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

		// cannot test this on vnet, no TLS in vnet.Net
	case v1alpha1.ListenerProtocolTURNTLS:
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

		tlsListener = telemetry.NewListener(tlsListener, l.Name, telemetry.ListenerType)

		conn := turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

	case v1alpha1.ListenerProtocolTURNDTLS:
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

		dtlsListener = telemetry.NewListener(dtlsListener, l.Name, telemetry.ListenerType)

		conn := turn.ListenerConfig{
			Listener:              dtlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.Conns = append(l.Conns, conn)

	default:
		return fmt.Errorf("internal error: unknown listener protocol " + l.Proto.String())
	}

	// start the TURN server if there are actual listeners configured
	if len(pConns) == 0 && len(lConns) == 0 {
		l.Server = nil
		return nil
	}

	t, err := turn.NewServer(turn.ServerConfig{
		Realm:             s.GetRealm(),
		AuthHandler:       s.NewAuthHandler(),
		LoggerFactory:     s.logger,
		PacketConnConfigs: pConns,
		ListenerConfigs:   lConns,
	})
	if err != nil {
		return fmt.Errorf("cannot set up TURN server for listener %s: %w",
			l.Name, err)
	}
	l.Server = t

	s.log.Infof("listener %s: TURN server running", l.Name)

	return nil
}
