package stunner

import (
	"crypto/tls"
	"fmt"
	"net"
	// "strings"

	// "github.com/pion/logging"
	"github.com/pion/dtls/v2"
	"github.com/pion/turn/v2"
	// "github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/internal/monitoring"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Start starts the STUNner server and starts listining on all requested server sockets
func (s *Stunner) Start() error {
	s.log.Infof("STUNner server (re)starting with API version %q", s.version)

	auth := s.GetAuth()

	// start listeners
	var pconn []turn.PacketConnConfig
	var conn []turn.ListenerConfig

	listeners := s.listenerManager.Keys()
	for _, name := range listeners {
		l := s.GetListener(name)

		relay := &turn.RelayAddressGeneratorPortRange{
			RelayAddress: l.Addr,
			Address:      l.Addr.String(),
			MinPort:      uint16(l.MinPort),
			MaxPort:      uint16(l.MaxPort),
			Net:          l.Net,
		}

		addr := fmt.Sprintf("%s:%d", l.Addr.String(), l.Port)

		switch l.Proto {
		case v1alpha1.ListenerProtocolUDP:
			s.log.Debugf("setting up UDP listener at %s", addr)
			udpListener, err := l.Net.ListenPacket("udp", addr)
			if err != nil {
				return fmt.Errorf("failed to create UDP listener at %s: %s",
					addr, err)
			}

			l.Conn = turn.PacketConnConfig{
				PacketConn:            udpListener,
				RelayAddressGenerator: relay,
				PermissionHandler:     s.NewPermissionHandler(l),
			}

			pconn = append(pconn, l.Conn.(turn.PacketConnConfig))

			// cannot test this on vnet, no Listen/ListenTCP in vnet.Net
		case v1alpha1.ListenerProtocolTCP:
			s.log.Debugf("setting up TCP listener at %s", addr)
			tcpListener, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
			}
			l.Conn = turn.ListenerConfig{
				Listener:              tcpListener,
				RelayAddressGenerator: relay,
				PermissionHandler:     s.NewPermissionHandler(l),
			}

			conn = append(conn, l.Conn.(turn.ListenerConfig))

			// cannot test this on vnet, no TLS in vnet.Net
		case v1alpha1.ListenerProtocolTLS:
			s.log.Debugf("setting up TLS/TCP listener at %s", addr)
			cer, errTls := tls.LoadX509KeyPair(l.Cert, l.Key)
			if errTls != nil {
				return fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s",
					addr, errTls)
			}

			tlsListener, err := tls.Listen("tcp", addr, &tls.Config{
				MinVersion:   tls.VersionTLS12,
				Certificates: []tls.Certificate{cer},
			})
			if err != nil {
				return fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
			}
			l.Conn = turn.ListenerConfig{
				Listener:              tlsListener,
				RelayAddressGenerator: relay,
				PermissionHandler:     s.NewPermissionHandler(l),
			}

			conn = append(conn, l.Conn.(turn.ListenerConfig))

			// cannot test this on vnet, no DTLS in vnet.Net
		case v1alpha1.ListenerProtocolDTLS:
			s.log.Debugf("setting up DTLS/UDP listener at %s", addr)

			cer, errTls := tls.LoadX509KeyPair(l.Cert, l.Key)
			if errTls != nil {
				return fmt.Errorf("cannot load cert/ley pair for creating DTLS listener at %s: %s",
					addr, errTls)
			}

			// for some reason dtls.Listen requires a UDPAddr and not an addr string
			udpAddr := &net.UDPAddr{IP: l.Addr, Port: l.Port}
			dtlsListener, err := dtls.Listen("udp", udpAddr, &dtls.Config{
				Certificates: []tls.Certificate{cer},
				// ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
			})
			if err != nil {
				return fmt.Errorf("failed to create DTLS listener at %s: %s", addr, err)
			}

			l.Conn = turn.ListenerConfig{
				Listener:              dtlsListener,
				RelayAddressGenerator: relay,
				PermissionHandler:     s.NewPermissionHandler(l),
			}

			conn = append(conn, l.Conn.(turn.ListenerConfig))

		default:
			return fmt.Errorf("internal error: unknown listener protocol " + l.Proto.String())
		}
	}

	// register metrics
	monitoring.RegisterMetrics(s.log, func() float64 { return float64(s.server.AllocationCount()) })

	// start monitoring
	s.monitoringServer = s.GetAdmin().MonitoringServer
	if s.monitoringServer != nil {
		s.monitoringServer.Start()
	}

	// start the DNS resolver threads
	if s.resolver == nil {
		s.resolver.Start()
	}

	// start the TURN server if there are actual listeners configured
	if len(conn) == 0 && len(pconn) == 0 {
		s.server = nil
	} else {
		t, err := turn.NewServer(turn.ServerConfig{
			Realm:             auth.Realm,
			AuthHandler:       s.NewAuthHandler(),
			LoggerFactory:     s.logger,
			PacketConnConfigs: pconn,
			ListenerConfigs:   conn,
		})
		if err != nil {
			return fmt.Errorf("cannot set up TURN server: %s", err)
		}
		s.server = t
	}

	s.log.Infof("TURN server running: %s", s.String())

	return nil
}

// Close stops the TURN server underneath STUNner
func (s *Stunner) Stop() {
	s.log.Info("stopping the STUNner server")

	if s.server != nil {
		s.server.Close()
	}
	s.server = nil

	// shutdown monitoring
	if s.monitoringServer != nil {
		s.monitoringServer.Stop()
	}
}
