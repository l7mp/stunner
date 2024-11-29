package object

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/v3"
	"github.com/pion/turn/v4"

	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Listener implements a STUNner listener.
type Listener struct {
	Name, Realm            string
	Proto                  stnrv1.ListenerProtocol
	Addr                   net.IP
	Port, MinPort, MaxPort int
	PublicAddr             string // for GetConfig()
	PublicPort             int    // for GetConfig()
	rawAddr                string // net.IP.String() may rewrite the string representation
	Cert, Key              []byte
	Conns                  []any // either a set of turn.ListenerConfigs or turn.PacketConnConfigs
	Server                 *turn.Server
	Routes                 []string
	Net                    transport.Net
	getRealm               RealmHandler
	logger                 logging.LoggerFactory
	log                    logging.LeveledLogger
}

// NewListener creates a new listener. Requires a server restart (returns ErrRestartRequired)
func NewListener(conf stnrv1.Config, net transport.Net, realmHandler RealmHandler, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*stnrv1.ListenerConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}

	// make sure req.Name is correct
	if err := req.Validate(); err != nil {
		return nil, err
	}

	l := Listener{
		Name:       req.Name,
		PublicAddr: req.PublicAddr,
		PublicPort: req.PublicPort,
		Net:        net,
		getRealm:   realmHandler,
		Conns:      []any{},
		logger:     logger,
		log:        logger.NewLogger(fmt.Sprintf("listener-%s", req.Name)),
	}

	l.log.Tracef("NewListener: %s", req.String())

	if err := l.Reconcile(req); err != nil && err != ErrRestartRequired {
		return nil, err
	}

	return &l, ErrRestartRequired
}

// Inspect examines whether a configuration change requires a reconciliation (returns true if it
// does) or restart (returns ErrRestartRequired).
func (l *Listener) Inspect(old, new, full stnrv1.Config) (bool, error) {
	req, ok := new.(*stnrv1.ListenerConfig)
	if !ok {
		return false, stnrv1.ErrInvalidConf
	}

	stunnerConf, ok := full.(*stnrv1.StunnerConfig)
	if !ok {
		return false, stnrv1.ErrInvalidConf
	}

	changed := !old.DeepEqual(req)

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
	cert, err := base64.StdEncoding.DecodeString(req.Cert)
	if err != nil {
		return false, fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil {
		return false, fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
	}

	// the only chance we don't need a restart if only the Routes and/or PublicIP/PublicPort change
	restart := ErrRestartRequired
	if l.Name == req.Name && // name unchanged (should always be true)
		l.Proto == proto && // protocol unchanged
		l.rawAddr == req.Addr && // address unchanged
		l.Port == req.Port && // ports unchanged
		bytes.Equal(l.Cert, cert) && // TLS creds unchanged
		bytes.Equal(l.Key, key) {
		restart = nil
	}

	// if the realm changes then we have to restart
	if l.Realm != stunnerConf.Auth.Realm {
		l.log.Tracef("listener %s restarts due to changing auth realm", l.Name)
		changed = true
		restart = ErrRestartRequired
	}

	return changed, restart
}

// Reconcile updates a listener.
func (l *Listener) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.ListenerConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}

	l.log.Tracef("Reconcile: %s", req.String())

	if err := req.Validate(); err != nil {
		return err
	}

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
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
	l.Port = req.Port
	if proto == stnrv1.ListenerProtocolTURNTLS || proto == stnrv1.ListenerProtocolTURNDTLS {
		cert, err := base64.StdEncoding.DecodeString(req.Cert)
		if err != nil {
			return fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
		}
		key, err := base64.StdEncoding.DecodeString(req.Key)
		if err != nil {
			return fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
		}
		l.Cert = cert
		l.Key = key
	}
	l.Realm = l.getRealm()

	l.PublicAddr = req.PublicAddr
	l.PublicPort = req.PublicPort

	l.Routes = make([]string, len(req.Routes))
	copy(l.Routes, req.Routes)

	return nil
}

// String returns a short stable string representation of the listener, safe for applying as a key in a map.
func (l *Listener) String() string {
	uri := fmt.Sprintf("%s: [%s://%s:%d<%d:%d>]", l.Name, strings.ToLower(l.Proto.String()),
		l.Addr, l.Port, l.MinPort, l.MaxPort)
	return uri
}

// ObjectName returns the name of the object.
func (l *Listener) ObjectName() string {
	return l.Name
}

// ObjectType returns the type of the object.
func (l *Listener) ObjectType() string {
	return "listener"
}

// GetConfig returns the configuration of the running listener.
func (l *Listener) GetConfig() stnrv1.Config {
	// must be sorted!
	sort.Strings(l.Routes)

	c := &stnrv1.ListenerConfig{
		Name:       l.Name,
		Protocol:   l.Proto.String(),
		Addr:       l.rawAddr,
		Port:       l.Port,
		PublicAddr: l.PublicAddr,
		PublicPort: l.PublicPort,
	}

	c.Cert = string(l.Cert)
	c.Key = string(l.Key)

	c.Routes = make([]string, len(l.Routes))
	copy(c.Routes, l.Routes)

	return c
}

// Close closes the TURN server that belongs to the listener.
func (l *Listener) Close() error {
	l.log.Tracef("closing %s listener at %s", l.Proto.String(), l.Addr)

	for _, c := range l.Conns {
		switch l.Proto {
		case stnrv1.ListenerProtocolTURNUDP:
			l.log.Tracef("closing %s packet socket at %s", l.Proto.String(), l.Addr)

			conn, ok := c.(turn.PacketConnConfig)
			if !ok {
				return fmt.Errorf("internal error: invalid conversion to " +
					"turn.PacketConnConfig")
			}

			if err := conn.PacketConn.Close(); err != nil && !util.IsClosedErr(err) {
				return err
			}
		case stnrv1.ListenerProtocolTURNTCP, stnrv1.ListenerProtocolTURNTLS, stnrv1.ListenerProtocolTURNDTLS:
			l.log.Tracef("closing %s listener socket at %s", l.Proto.String(), l.Addr)

			conn, ok := c.(turn.ListenerConfig)
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

	l.Conns = []any{}

	if l.Server != nil {
		l.Server.Close()
	}
	l.Server = nil

	return nil
}

// Status returns the status of the object.
func (l *Listener) Status() stnrv1.Status {
	return l.GetConfig()
}

// ///////////
// ListenerFactory can create now Listener objects
type ListenerFactory struct {
	net          transport.Net
	realmHandler RealmHandler
	logger       logging.LoggerFactory
}

// NewListenerFactory creates a new factory for Listener objects
func NewListenerFactory(net transport.Net, realmHandler RealmHandler, logger logging.LoggerFactory) Factory {
	return &ListenerFactory{
		net:          net,
		realmHandler: realmHandler,
		logger:       logger,
	}
}

// New can produce a new Listener object from the given configuration. A nil config will create an
// empty listener object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *ListenerFactory) New(conf stnrv1.Config) (Object, error) {
	if conf == nil {
		return &Listener{}, nil
	}

	return NewListener(conf, f.net, f.realmHandler, f.logger)
}
