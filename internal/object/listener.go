package object

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"

	"github.com/pion/dtls/v2"
	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Listener implements a STUNner listener
type Listener struct {
	Name, Realm            string
	Proto                  v1alpha1.ListenerProtocol
	Addr                   net.IP
	Port, MinPort, MaxPort int
	rawAddr                string // net.IP.String() may rewrite the string representation
	Cert, Key              []byte
	conn                   interface{} // either turn.ListenerConfig or turn.PacketConnConfig
	server                 *turn.Server
	Routes                 []string
	Net                    *vnet.Net
	handlerFactory         HandlerFactory
	dryRun                 bool
	logger                 logging.LoggerFactory
	log                    logging.LeveledLogger
}

// NewListener creates a new listener. Requires a server restart (returns
// ErrRestartRequired)
func NewListener(conf v1alpha1.Config, net *vnet.Net, handlerFactory HandlerFactory, dryRun bool, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.ListenerConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	// make sure req.Name is correct
	if err := req.Validate(); err != nil {
		return nil, err
	}

	l := Listener{
		Name:           req.Name,
		Net:            net,
		handlerFactory: handlerFactory,
		dryRun:         dryRun,
		logger:         logger,
		log:            logger.NewLogger(fmt.Sprintf("stunner-listener-%s", req.Name)),
	}

	l.log.Tracef("NewListener: %s", req.String())

	if err := l.Reconcile(req); err != nil && err != ErrRestartRequired {
		return nil, err
	}

	return &l, ErrRestartRequired
}

// Inspect examines whether a configuration change on the object would require a restart. An empty
// new-config means it is about to be deleted, an empty old-config means it is to be deleted,
// otherwise it will be reconciled from the old configuration to the new one
func (l *Listener) Inspect(old, new v1alpha1.Config) bool {
	// this is the only interesting Inspect function

	// adding a new listener or deleting an existing one triggers a server restart
	if old == nil || new == nil {
		return true
	}

	// this is a reconciliation event!
	req, ok := new.(*v1alpha1.ListenerConfig)
	if !ok {
		// should never happen
		panic("Listener.Inspect called on an unknown configuration")
	}

	if err := req.Validate(); err != nil {
		// should never happen
		panic("Listener.Inspect called with an invalid ListenerConfig")
	}

	proto, _ := v1alpha1.NewListenerProtocol(req.Protocol)

	// the only chance we don't need a restart if only the Routes change
	restart := true
	if l.Name == req.Name && // name unchanged (should always be true)
		l.Proto == proto && // protocol unchanged
		l.rawAddr == req.Addr && // address unchanged
		l.Port == req.Port && // ports unchanged
		l.MinPort == req.MinRelayPort &&
		l.MaxPort == req.MaxRelayPort &&
		bytes.Compare(l.Cert, req.Cert.B) == 0 && // TLS creds unchanged
		bytes.Compare(l.Key, req.Key.B) == 0 {
		restart = false
	}

	// if the realm changes then we have to restart
	if l.Realm != l.handlerFactory.GetRealm() {
		l.log.Tracef("listener %s restarts due to changing auth realm", l.Name)
		restart = true
	}

	return restart
}

// Reconcile updates a listener.
func (l *Listener) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.ListenerConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	l.log.Tracef("Reconcile: %s", req.String())

	if err := req.Validate(); err != nil {
		return err
	}

	// will we need a TURN server restart?
	restart := l.Inspect(l.GetConfig(), conf)

	// close listener and the underlying net.Conn/net.PacketConn
	if restart && !l.dryRun && l.server != nil {
		if err := l.Close(); err != nil && !errors.Is(err, ErrRestartRequired) {
			return err
		}
	}

	proto, _ := v1alpha1.NewListenerProtocol(req.Protocol)
	ipAddr := net.ParseIP(req.Addr)
	// special-case "localhost"
	if ipAddr == nil && req.Addr == "localhost" {
		ipAddr = net.ParseIP("127.0.0.1")
	}
	if ipAddr == nil {
		return fmt.Errorf("invalid listener address: %s", req.Addr)
	}

	l.Proto = proto
	l.Addr = ipAddr
	l.rawAddr = req.Addr
	l.Port, l.MinPort, l.MaxPort = req.Port, req.MinRelayPort, req.MaxRelayPort
	if proto == v1alpha1.ListenerProtocolTLS || proto == v1alpha1.ListenerProtocolDTLS {
		l.Cert = clone(req.Cert.B)
		l.Key = clone(req.Key.B)
	}
	l.Realm = l.handlerFactory.GetRealm()

	l.Routes = make([]string, len(req.Routes))
	copy(l.Routes, req.Routes)

	// start a new TURN server
	if restart && !l.dryRun {
		if err := l.Start(); err != nil {
			return err
		}
	}

	return nil
}

// String returns a short stable string representation of the listener, safe for applying as a key in a map.
func (l *Listener) String() string {
	uri := fmt.Sprintf("%s: [%s://%s:%d<%d:%d>]", l.Name, l.Proto, l.Addr, l.Port, l.MinPort, l.MaxPort)
	return uri
}

// Name returns the name of the object.
func (l *Listener) ObjectName() string {
	// singleton!
	return l.Name
}

// GetConfig returns the configuration of the running listener.
func (l *Listener) GetConfig() v1alpha1.Config {
	// must be sorted!
	sort.Strings(l.Routes)

	c := &v1alpha1.ListenerConfig{
		Name:         l.Name,
		Protocol:     l.Proto.String(),
		Addr:         l.rawAddr,
		Port:         l.Port,
		MinRelayPort: l.MinPort,
		MaxRelayPort: l.MaxPort,
	}

	c.Cert = v1alpha1.Secret{B: clone(l.Cert)}
	c.Key = v1alpha1.Secret{B: clone(l.Key)}

	c.Routes = make([]string, len(l.Routes))
	copy(c.Routes, l.Routes)

	return c
}

// Close closes the listener
func (l *Listener) Close() error {
	l.log.Tracef("closing %s listener at %s", l.Proto.String(), l.Addr)

	if l.conn != nil {
		switch l.Proto {
		case v1alpha1.ListenerProtocolUDP:
			l.log.Tracef("closing %s packet socket at %s", l.Proto.String(), l.Addr)
			conn, ok := l.conn.(turn.PacketConnConfig)
			if !ok {
				return fmt.Errorf("internal error: invalid conversion to " +
					"turn.ListenerConfig")
			}

			if err := conn.PacketConn.Close(); err != nil && !util.IsClosedErr(err) {
				return err
			}
		case v1alpha1.ListenerProtocolTCP, v1alpha1.ListenerProtocolTLS, v1alpha1.ListenerProtocolDTLS:
			l.log.Tracef("closing %s listener socket at %s", l.Proto.String(), l.Addr)
			conn, ok := l.conn.(turn.ListenerConfig)
			if !ok {
				return fmt.Errorf("internal error: invalid conversion to " +
					"turn.ListenerConfig")
			}

			if err := conn.Listener.Close(); err != nil && !util.IsClosedErr(err) {
				return err
			}
		default:
			return fmt.Errorf("internal error: unknown listener protocol %q",
				l.Proto.String())
		}
	}

	if l.server != nil {
		l.server.Close()
	}
	l.server = nil

	return ErrRestartRequired
}

// Start will start the TURN server belonging to the listener.
func (l *Listener) Start() error {
	l.log.Infof("listener %s (re)starting", l.String())

	// start listeners
	var pConns []turn.PacketConnConfig
	var lConns []turn.ListenerConfig

	relay := &telemetry.RelayAddressGenerator{
		Name:         l.Name,
		RelayAddress: l.Addr,
		Address:      l.Addr.String(),
		MinPort:      uint16(l.MinPort),
		MaxPort:      uint16(l.MaxPort),
		DryRun:       l.dryRun,
		Net:          l.Net,
	}

	permissionHandler := l.handlerFactory.GetPermissionHandler(l)

	addr := fmt.Sprintf("%s:%d", l.Addr.String(), l.Port)

	switch l.Proto {
	case v1alpha1.ListenerProtocolUDP:
		l.log.Debugf("setting up UDP listener at %s", addr)
		udpListener, err := l.Net.ListenPacket("udp", addr)
		if err != nil {
			return fmt.Errorf("failed to create UDP listener at %s: %s",
				addr, err)
		}

		if !l.dryRun {
			udpListener = telemetry.NewPacketConn(udpListener, l.Name, telemetry.ListenerType)
		}

		conn := turn.PacketConnConfig{
			PacketConn:            udpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		pConns = append(pConns, conn)
		l.conn = conn

	case v1alpha1.ListenerProtocolTCP:
		l.log.Debugf("setting up TCP listener at %s", addr)

		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}

		if !l.dryRun {
			tcpListener = telemetry.NewListener(tcpListener, l.Name, telemetry.ListenerType)
		}

		conn := turn.ListenerConfig{
			Listener:              tcpListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.conn = conn

		// cannot test this on vnet, no TLS in vnet.Net
	case v1alpha1.ListenerProtocolTLS:
		l.log.Debugf("setting up TLS/TCP listener at %s", addr)
		// cer, errTls := tls.LoadX509KeyPair(l.Cert, l.Key)
		// if errTls != nil {
		// 	return fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s",
		// 		addr, errTls)
		// }

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

		if !l.dryRun {
			tlsListener = telemetry.NewListener(tlsListener, l.Name, telemetry.ListenerType)
		}

		conn := turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.conn = conn

	case v1alpha1.ListenerProtocolDTLS:
		l.log.Debugf("setting up DTLS/UDP listener at %s", addr)

		// cer, errTls := tls.LoadX509KeyPair(l.Cert, l.Key)
		// if errTls != nil {
		// 	return fmt.Errorf("cannot load cert/key pair for creating DTLS listener at %s: %s",
		// 		addr, errTls)
		// }

		cer, err := tls.X509KeyPair(l.Cert, l.Key)
		if err != nil {
			return fmt.Errorf("cannot load cert/key pair for creating DTLS listener at %s: %s",
				addr, err)
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

		if !l.dryRun {
			dtlsListener = telemetry.NewListener(dtlsListener, l.Name, telemetry.ListenerType)
		}

		conn := turn.ListenerConfig{
			Listener:              dtlsListener,
			RelayAddressGenerator: relay,
			PermissionHandler:     permissionHandler,
		}

		lConns = append(lConns, conn)
		l.conn = conn

	default:
		return fmt.Errorf("internal error: unknown listener protocol " + l.Proto.String())
	}

	// start the TURN server if there are actual listeners configured
	if len(pConns) == 0 && len(lConns) == 0 {
		l.server = nil
	} else {
		t, err := turn.NewServer(turn.ServerConfig{
			Realm:             l.handlerFactory.GetRealm(),
			AuthHandler:       l.handlerFactory.GetAuthHandler(),
			LoggerFactory:     l.logger,
			PacketConnConfigs: pConns,
			ListenerConfigs:   lConns,
		})
		if err != nil {
			return fmt.Errorf("cannot set up TURN server for listener %s: %w",
				l.Name, err)
		}
		l.server = t
	}

	l.log.Infof("listener %s: TURN server running", l.Name)

	return nil
}

// ///////////
// ListenerFactory can create now Listener objects
type ListenerFactory struct {
	net            *vnet.Net
	handlerFactory HandlerFactory
	dryRun         bool
	logger         logging.LoggerFactory
}

// NewListenerFactory creates a new factory for Listener objects
func NewListenerFactory(net *vnet.Net, handlerFactory HandlerFactory, dryRun bool, logger logging.LoggerFactory) Factory {
	return &ListenerFactory{
		net:            net,
		handlerFactory: handlerFactory,
		dryRun:         dryRun,
		logger:         logger,
	}
}

// New can produce a new Listener object from the given configuration. A nil config will create an
// empty listener object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *ListenerFactory) New(conf v1alpha1.Config) (Object, error) {
	if conf == nil {
		return &Listener{}, nil
	}

	return NewListener(conf, f.net, f.handlerFactory, f.dryRun, f.logger)
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	tmp := make([]byte, len(b))
	copy(tmp, b)
	return tmp
}
