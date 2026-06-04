package object

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// Server is the listener-facing interface implemented by TURN server wrappers (currently
// TURNServer). Kept as an interface so tests can swap in stubs.
type Server interface {
	Close() error
	AllocationCount() int
}

// Listener implements a STUNner listener.
type Listener struct {
	Name, Realm            string
	Proto                  stnrv1.ListenerProtocol
	Addr                   net.IP
	Port, MinPort, MaxPort int
	PublicAddr             string
	PublicPort             int
	rawAddr                string
	Cert, Key              []byte
	Routes                 []string

	reg          Registry
	telemetry    *telemetry.Telemetry
	udpThreadNum int
	server       Server
	quotaStore   quota.Store
	Net          transport.Net
	logger       logger.LoggerFactory
	log          logging.LeveledLogger
}

// NewListener creates a Listener object.
func NewListener(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	if conf == nil {
		return &Listener{
			reg:          reg,
			telemetry:    rt.Telemetry,
			quotaStore:   rt.QuotaStore,
			udpThreadNum: rt.UdpThreadNum,
			Net:          rt.Net,
			logger:       rt.Logger,
			log:          rt.Logger.NewLogger("listener"),
		}, nil
	}
	req := conf.(*stnrv1.ListenerConfig)
	if err := req.Validate(); err != nil {
		return nil, err
	}
	name := req.Name
	l := &Listener{
		Name:         name,
		reg:          reg,
		telemetry:    rt.Telemetry,
		quotaStore:   rt.QuotaStore,
		udpThreadNum: rt.UdpThreadNum,
		Net:          rt.Net,
		logger:       rt.Logger,
		log:          rt.Logger.NewLogger(fmt.Sprintf("listener-%s", name)),
	}
	if err := l.Reconcile(req); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Listener) ObjectName() string { return l.Name }
func (l *Listener) ObjectType() string { return TypeListener }

// Extract pulls the listener's own ListenerConfig out of the full config.
func (l *Listener) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	lc, err := c.GetListenerConfig(l.Name)
	if err != nil {
		return nil, err
	}
	return &lc, nil
}

func (l *Listener) Inspect(old, new stnrv1.Config, full *stnrv1.StunnerConfig) (Action, error) {
	req := new.(*stnrv1.ListenerConfig)
	if err := req.Validate(); err != nil {
		return ActionNone, err
	}

	cur := old.(*stnrv1.ListenerConfig)
	changed := !cur.DeepEqual(req)

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
	cert, err := base64.StdEncoding.DecodeString(req.Cert)
	if err != nil {
		return ActionNone, fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil {
		return ActionNone, fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
	}

	// A restart is only avoidable when Routes and/or PublicIP/PublicPort are the only changes.
	restart := !(l.Name == req.Name &&
		l.Proto == proto &&
		l.rawAddr == req.Addr &&
		l.Port == req.Port &&
		bytes.Equal(l.Cert, cert) &&
		bytes.Equal(l.Key, key))

	curRealm := l.Realm
	if a := l.lookupAuth(); a != nil {
		curRealm = a.Realm
	}
	desiredRealm := full.Auth.Realm
	if curRealm != desiredRealm {
		l.log.Tracef("listener %s restarts due to changing auth realm", l.Name)
		changed = true
		restart = true
	}
	if !changed {
		return ActionNone, nil
	}
	if restart {
		return ActionRestart, nil
	}
	return ActionReconcile, nil
}

func (l *Listener) Reconcile(conf stnrv1.Config) error {
	req := conf.(*stnrv1.ListenerConfig)
	l.log.Tracef("reconcile: %s", req.String())
	if err := req.Validate(); err != nil {
		return err
	}

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
	ipAddr := net.ParseIP(req.Addr)
	if ipAddr == nil && req.Addr == "localhost" {
		ipAddr = net.ParseIP("127.0.0.1")
	}
	if ipAddr == nil {
		return fmt.Errorf("invalid listener address: %s", req.Addr)
	}

	l.Name = req.Name
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
	l.Realm = stnrv1.DefaultRealm
	if a := l.lookupAuth(); a != nil {
		l.Realm = a.Realm
	}
	l.PublicAddr = req.PublicAddr
	l.PublicPort = req.PublicPort

	l.Routes = make([]string, len(req.Routes))
	copy(l.Routes, req.Routes)
	return nil
}

// String returns a short stable representation, safe as a map key.
func (l *Listener) String() string {
	return fmt.Sprintf("%s: [%s://%s:%d<%d:%d>]", l.Name, strings.ToLower(l.Proto.String()),
		l.Addr, l.Port, l.MinPort, l.MaxPort)
}

func (l *Listener) GetConfig() stnrv1.Config {
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

func (l *Listener) Start() error {
	l.log.Infof("listener %s (re)starting", l.String())
	switch l.Proto {
	case stnrv1.ListenerProtocolTURNUDP, stnrv1.ListenerProtocolTURNTCP,
		stnrv1.ListenerProtocolTURNTLS, stnrv1.ListenerProtocolTURNDTLS:
		t, err := NewTURNServer(l)
		if err != nil {
			return fmt.Errorf("failed to start TURN server for listener %s: %w", l.Name, err)
		}
		l.server = t
	default:
		return fmt.Errorf("internal error: unknown listener protocol %q", l.Proto.String())
	}
	l.log.Infof("listener %s: listener running", l.Name)
	return nil
}

func (l *Listener) Close(_ bool) error {
	l.log.Tracef("closing %s listener at %s", l.Proto.String(), l.Addr)
	if l.server == nil {
		return nil
	}
	if err := l.server.Close(); err != nil && !util.IsClosedErr(err) && !strings.Contains(err.Error(), "already closed") {
		return err
	}
	l.server = nil
	return nil
}

func (l *Listener) Status() stnrv1.Status {
	stats := stnrv1.OffloadDirStat{}
	if a := l.getOffload(); a != nil {
		stats = a.Stats(l.Name, stnrv1.ListenerStat)
	}
	return &stnrv1.ListenerStatus{
		ListenerConfig: l.GetConfig().(*stnrv1.ListenerConfig),
		Stats:          stats,
	}
}

func (l *Listener) AllocationCount() int {
	if l.server != nil {
		return l.server.AllocationCount()
	}
	return 0
}

// Registry-backed cross-references used at TURN handler time. These replace the old Router.GetX
// helpers; they are looked up dynamically so a reconcile that swaps an Auth or a Cluster takes
// effect on the next request without needing a Listener restart for cross-ref reasons.

func (l *Listener) lookupAuth() *Auth {
	if l.reg == nil {
		return nil
	}
	a, ok := l.reg.LookupOne(TypeAuth)
	if !ok {
		return nil
	}
	return a.(*Auth)
}

func (l *Listener) lookupCluster(name string) *Cluster {
	if l.reg == nil {
		return nil
	}
	c, ok := l.reg.Lookup(TypeCluster, name)
	if !ok {
		return nil
	}
	return c.(*Cluster)
}

func (l *Listener) clustersForRoutes() []*Cluster {
	out := []*Cluster{}
	for _, r := range l.Routes {
		if c := l.lookupCluster(r); c != nil {
			out = append(out, c)
		}
	}
	return out
}

func (l *Listener) getOffload() TURNOffloadHandler {
	if l.reg == nil {
		return nil
	}
	o, ok := l.reg.LookupOne(TypeOffload)
	if !ok {
		return nil
	}
	return o.(*Offload).Handler()
}
