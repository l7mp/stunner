package object

import (
	"fmt"
	"net"
	"sort"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Listener implements a STUNner listener
type Listener struct {
	Name                   string
	Proto                  v1alpha1.ListenerProtocol
	Addr                   net.IP
	Port, MinPort, MaxPort int
	Cert, Key, rawAddr     string      // net.IP.String() may rewrite the string representation
	Conn                   interface{} // either turn.ListenerConfig or turn.PacketConnConfig
	Routes                 []string
	log                    logging.LeveledLogger
	Net                    *vnet.Net
}

// NewListener creates a new listener. Requires a server restart (returns v1alpha1.ErrRestartRequired)
func NewListener(conf v1alpha1.Config, net *vnet.Net, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.ListenerConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	// make sure req.Name is correct
	if err := req.Validate(); err != nil {
		return nil, err
	}

	l := Listener{
		Name: req.Name,
		log:  logger.NewLogger(fmt.Sprintf("stunner-listener-%s", req.Name)),
		Net:  net,
	}

	l.log.Tracef("NewListener: %#v", req)

	if err := l.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &l, v1alpha1.ErrRestartRequired
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

	// this is a reconciliaiton event!
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
		l.Cert == req.Cert && // TLS creds unchanged
		l.Key == req.Key {
		restart = false
	}

	return restart
}

// Reconcile updates a listener.
func (l *Listener) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.ListenerConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	l.log.Tracef("Reconcile: %#v", req)

	if err := req.Validate(); err != nil {
		return err
	}

	proto, _ := v1alpha1.NewListenerProtocol(req.Protocol)
	ipAddr := net.ParseIP(req.Addr)

	l.Proto = proto
	l.Addr = ipAddr
	l.rawAddr = req.Addr
	l.Port, l.MinPort, l.MaxPort = req.Port, req.MinRelayPort, req.MaxRelayPort
	if proto == v1alpha1.ListenerProtocolTLS || proto == v1alpha1.ListenerProtocolDTLS {
		l.Cert = req.Cert
		l.Key = req.Key
	}

	l.Routes = make([]string, len(req.Routes))
	copy(l.Routes, req.Routes)

	return nil
}

// String returns a short stable string representation of the listener, safe for applying as a key in a map
func (l *Listener) String() string {
	uri := fmt.Sprintf("%s://%s:%d[%d:%d]", l.Proto, l.Addr, l.Port, l.MinPort, l.MaxPort)
	if l.Cert != "" && l.Key != "" {
		uri += " (cert/key)"
	}
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

	c := &v1alpha1.ListenerConfig{
		Name:         l.Name,
		Protocol:     l.Proto.String(),
		Addr:         l.rawAddr,
		Port:         l.Port,
		MinRelayPort: l.MinPort,
		MaxRelayPort: l.MaxPort,
		Cert:         l.Cert,
		Key:          l.Key,
	}

	c.Routes = make([]string, len(l.Routes))
	copy(c.Routes, l.Routes)

	return c
}

// Close closes the listener, required a restart!
func (l *Listener) Close() error {
	l.log.Tracef("closing %s listener at %s", l.Proto.String(), l.Addr)

	switch l.Proto {
	case v1alpha1.ListenerProtocolUDP:
		if l.Conn != nil {
			l.log.Tracef("closing %s packet socket at %s", l.Proto.String(), l.Addr)
			conn, ok := l.Conn.(turn.PacketConnConfig)
			if !ok {
				return fmt.Errorf("internal error: invalid conversion to turn.PacketConnConfig")
			}

			if err := conn.PacketConn.Close(); err != nil {
				return err
			}
		}
	case v1alpha1.ListenerProtocolTCP, v1alpha1.ListenerProtocolTLS, v1alpha1.ListenerProtocolDTLS:
		if l.Conn != nil {
			l.log.Tracef("closing %s listener socket at %s", l.Proto.String(), l.Addr)
			conn, ok := l.Conn.(turn.ListenerConfig)
			if !ok {
				return fmt.Errorf("internal error: invalid conversion to turn.ListenerConfig")
			}

			if err := conn.Listener.Close(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("internal error: unknown listener protocol %q", l.Proto.String())
	}

	return v1alpha1.ErrRestartRequired
}

// ListenerFactory can create now Listener objects
type ListenerFactory struct {
	net    *vnet.Net
	logger logging.LoggerFactory
}

// NewListenerFactory creates a new factory for Listener objects
func NewListenerFactory(net *vnet.Net, logger logging.LoggerFactory) Factory {
	return &ListenerFactory{net: net, logger: logger}
}

// New can produce a new Listener object from the given configuration. A nil config will create an
// empty listener object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *ListenerFactory) New(conf v1alpha1.Config) (Object, error) {
	if conf == nil {
		return &Listener{}, nil
	}

	return NewListener(conf, f.net, f.logger)
}
