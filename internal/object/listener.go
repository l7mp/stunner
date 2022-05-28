package object

import (
	"net"
	"fmt"
	"sort"
	"crypto/tls"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/pion/dtls/v2"
	"github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Listener implements a STUNner listener
type Listener struct {
	Name string
	Proto v1alpha1.ListenerProtocol
	Addr net.IP
	Port, MinPort, MaxPort int
	Cert, Key, rawAddr string  // net.IP.String() may rewrite the string representation
	Conn interface{}  // either turn.ListenerConfig or turn.PacketConnConfig
        Routes []string
	log logging.LeveledLogger
	net *vnet.Net
}

// NewListener creates a new listener. Requires a server restart (returns ErrRestartRequired)
func NewListener(conf v1alpha1.Config, net *vnet.Net, logger logging.LoggerFactory) (Object, error) {
        req, ok := conf.(*v1alpha1.ListenerConfig)
        if !ok {
                return nil, ErrInvalidConf
        }
        
        // make sure req.Name is correct
        if err := req.Validate(); err != nil {
                return nil, err
        }

        l := Listener {
                Routes: []string{},
                log:    logger.NewLogger(fmt.Sprintf("stunner-listener-%s", req.Name)),
                net:    net,
        }

        l.log.Tracef("NewListener: %#v", req)

        if err := l.Reconcile(req); err != nil && err != ErrRestartRequired {
                return nil, err
        }

        return &l, ErrRestartRequired
}      

// Reconcile updates a listener. Requires a server restart (returns ErrRestartRequired)
func (l *Listener) Reconcile(conf v1alpha1.Config) error {
        req, ok := conf.(*v1alpha1.ListenerConfig)
        if !ok {
                return ErrInvalidConf
        }
        
        l.log.Tracef("Reconcile: %#v", req)

        if err := req.Validate(); err != nil {
                return err
        }
        proto, _ := v1alpha1.NewListenerProtocol(req.Protocol)
        ipAddr     := net.ParseIP(req.Addr)

	relay := &turn.RelayAddressGeneratorPortRange{
		RelayAddress: ipAddr,
		Address:      ipAddr.String(),
		MinPort:      uint16(req.MinRelayPort),
		MaxPort:      uint16(req.MaxRelayPort),
		Net:          l.net,
	}
	
	addr := fmt.Sprintf("%s:%d", ipAddr.String(), req.Port)
	switch l.Proto {
	case v1alpha1.ListenerProtocolUdp:
		l.log.Tracef("setting up UDP listener at %s", addr)
		udpListener, err := l.net.ListenPacket("udp", addr)
		if err != nil {
			return fmt.Errorf("failed to create UDP listener at %s: %s",
				addr, err)
		}
		
		l.Conn = turn.PacketConnConfig{
			PacketConn:            udpListener,
			RelayAddressGenerator: relay,
                        PermissionHandler:     nil,    // this will be patched in from Stunner
		}
		
	// cannot test this on vnet, no Listen/ListenTCP in vnet.Net
	case v1alpha1.ListenerProtocolTcp:
		l.log.Tracef("setting up TCP listener at %s", addr)
		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}
		l.Conn = turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
                        PermissionHandler:     nil,    // this will be patched in from Stunner
		}

	// cannot test this on vnet, no TLS in vnet.Net
	case v1alpha1.ListenerProtocolTls:
		l.log.Tracef("setting up TLS/TCP listener at %s", addr)
		cer, errTls := tls.LoadX509KeyPair(req.Cert, req.Key)
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
                        PermissionHandler:     nil,    // this will be patched in from Stunner
		}
		
	// cannot test this on vnet, no DTLS in vnet.Net
	case v1alpha1.ListenerProtocolDtls:
		l.log.Tracef("setting up DTLS/UDP listener at %s", addr)

		cer, errTls := tls.LoadX509KeyPair(req.Cert, req.Key)
		if errTls != nil {
			return fmt.Errorf("cannot load cert/ley pair for creating DTLS listener at %s: %s",
				addr, errTls)
		}

		// for some reason dtls.Listen requires a UDPAddr and not an addr string
		udpAddr := &net.UDPAddr{IP: ipAddr, Port: req.Port}
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
                        PermissionHandler:     nil,    // this will be patched in from Stunner
		}

	default:
		panic("internal error: unknown listener protocol " + l.Proto.String())
	}

        // no error: update
	l.Proto   = proto
	l.Addr    = ipAddr
	l.rawAddr = req.Addr
	l.Port, l.MinPort, l.MaxPort = req.Port, req.MinRelayPort, req.MaxRelayPort
        if proto == v1alpha1.ListenerProtocolTls || proto == v1alpha1.ListenerProtocolDtls {
		l.Cert = req.Cert
		l.Key = req.Key
        }

        copy(l.Routes, req.Routes)
        
	return ErrRestartRequired
}

// String returns a short stable string representation of the listener, safe for applying as a key in a map
func (l *Listener) String() string {
	uri := fmt.Sprintf("%s://%s:%d [%d:%d]", l.Proto, l.Addr, l.Port, l.MinPort, l.MaxPort)
	if l.Cert != "" && l.Key != "" { uri += " (cert/key)" }
	return uri
}

// Name returns the name of the object
func (l *Listener) ObjectName() string {
        // singleton!
        return l.Name
}

// GetConfig returns the configuration of the running listener
func (l *Listener) GetConfig() v1alpha1.Config {
        // must be sorted!
        sort.Strings(l.Routes)
        
	return &v1alpha1.ListenerConfig{
                Name:         l.Name,
		Protocol:     l.Proto.String(),
		Addr:         l.rawAddr,
		Port:         l.Port,
		MinRelayPort: l.MinPort,
		MaxRelayPort: l.MaxPort,
		Cert:         l.Cert,
		Key:          l.Key,
                Routes:       l.Routes,
	}
}

// Close closes the listener
func (l *Listener) Close() {
        l.log.Tracef("closing %s listener at %s", l.Proto.String(), l.Addr)

	switch l.Proto {
	case v1alpha1.ListenerProtocolUdp:
                l.Conn.(turn.PacketConnConfig).PacketConn.Close()
        case v1alpha1.ListenerProtocolTcp:
                l.Conn.(turn.ListenerConfig).Listener.Close()
	case v1alpha1.ListenerProtocolTls:
                l.Conn.(turn.ListenerConfig).Listener.Close()
	case v1alpha1.ListenerProtocolDtls:
                l.Conn.(turn.ListenerConfig).Listener.Close()
	default:
		panic("internal error: unknown listener protocol " + l.Proto.String())
	}
}
