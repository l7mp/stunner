package object

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"

	objectturn "github.com/l7mp/stunner/internal/object/turn"
	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// Listener implements a STUNner listener. The TURN server it drives lives in the listener's
// ListenerServer child; the listener itself holds only the reconciled config, published as an
// atomic snapshot for the TURN request path.
type Listener struct {
	name, Realm            string
	Proto                  stnrv1.ListenerProtocol
	Addr                   net.IP
	Port, MinPort, MaxPort int
	PublicAddr             string
	PublicPort             int
	rawAddr                string
	Cert, Key              []byte
	Routes                 []string

	// conf is the atomic snapshot read by the TURN handlers on the request path.
	conf atomic.Pointer[stnrv1.ListenerConfig]

	rt           *runtime.Runtime
	telemetry    *telemetry.Telemetry
	udpThreadNum int
	Net          transport.Net
	logger       logger.LoggerFactory
	log          logging.LeveledLogger
}

// NewListener creates a Listener object.
func NewListener(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	if conf == nil {
		return &Listener{
			rt:           rt,
			telemetry:    rt.Telemetry,
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
		name:         name,
		rt:           rt,
		telemetry:    rt.Telemetry,
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

func (l *Listener) Name() string             { return l.name }
func (l *Listener) Type() runtime.ObjectType { return runtime.TypeListener }

func (l *Listener) Inspect(old, new stnrv1.Config, full *stnrv1.StunnerConfig) (runtime.Action, error) {
	req := new.(*stnrv1.ListenerConfig)
	if err := req.Validate(); err != nil {
		return runtime.ActionNone, err
	}

	cur := old.(*stnrv1.ListenerConfig)
	changed := !cur.DeepEqual(req)

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
	cert, err := base64.StdEncoding.DecodeString(req.Cert)
	if err != nil {
		return runtime.ActionNone, fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil {
		return runtime.ActionNone, fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
	}

	// A restart is only avoidable when Routes and/or PublicIP/PublicPort are the only changes.
	restart := !(l.name == req.Name &&
		l.Proto == proto &&
		l.rawAddr == req.Addr &&
		l.Port == req.Port &&
		bytes.Equal(l.Cert, cert) &&
		bytes.Equal(l.Key, key))

	curRealm := l.Realm
	if a := l.lookupAuthConfig(); a != nil {
		curRealm = a.Realm
	}
	desiredRealm := full.Auth.Realm
	if curRealm != desiredRealm {
		l.log.Tracef("listener %s restarts due to changing auth realm", l.name)
		changed = true
		restart = true
	}
	if !changed {
		return runtime.ActionNone, nil
	}
	if restart {
		return runtime.ActionRestart, nil
	}
	return runtime.ActionReconcile, nil
}

func (l *Listener) Reconcile(conf stnrv1.Config) error {
	req := conf.(*stnrv1.ListenerConfig)
	l.log.Tracef("reconcile: %s", req.String())
	if err := req.Validate(); err != nil {
		return err
	}
	if l.name == "" {
		l.name = req.Name
	}
	if l.name != req.Name {
		return fmt.Errorf("cannot rename listener %q to %q", l.name, req.Name)
	}

	proto, _ := stnrv1.NewListenerProtocol(req.Protocol)
	ipAddr := net.ParseIP(req.Addr)
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
	l.Realm = stnrv1.DefaultRealm
	if a := l.lookupAuthConfig(); a != nil {
		l.Realm = a.Realm
	}
	l.PublicAddr = req.PublicAddr
	l.PublicPort = req.PublicPort

	l.Routes = make([]string, len(req.Routes))
	copy(l.Routes, req.Routes)

	// Publish the snapshot for the TURN request path.
	l.conf.Store(l.buildConfig())

	l.rt.Router.InvalidateCache()
	return nil
}

// buildConfig renders the listener's live config from its fields. Only called from Reconcile;
// readers go through the snapshot.
func (l *Listener) buildConfig() *stnrv1.ListenerConfig {
	routes := make([]string, len(l.Routes))
	copy(routes, l.Routes)
	sort.Strings(routes)
	c := &stnrv1.ListenerConfig{
		Name:       l.name,
		Protocol:   l.Proto.String(),
		Addr:       l.rawAddr,
		Port:       l.Port,
		PublicAddr: l.PublicAddr,
		PublicPort: l.PublicPort,
		Routes:     routes,
	}
	c.Cert = string(l.Cert)
	c.Key = string(l.Key)
	return c
}

// String returns a short stable representation, safe as a map key.
func (l *Listener) String() string {
	return fmt.Sprintf("%s: [%s://%s:%d<%d:%d>]", l.name, strings.ToLower(l.Proto.String()),
		l.Addr, l.Port, l.MinPort, l.MaxPort)
}

// GetConfig returns a copy of the live listener config. Safe for concurrent use.
func (l *Listener) GetConfig() stnrv1.Config {
	snap := l.conf.Load()
	if snap == nil {
		return &stnrv1.ListenerConfig{Name: l.name}
	}
	cp := *snap
	cp.Routes = make([]string, len(snap.Routes))
	copy(cp.Routes, snap.Routes)
	return &cp
}

func (l *Listener) Start() error {
	return nil
}

func (l *Listener) Close(_ bool) error {
	l.rt.Router.InvalidateCache()
	return nil
}

func (l *Listener) Status() stnrv1.Status {
	conf := l.GetConfig().(*stnrv1.ListenerConfig)
	status := &stnrv1.ListenerStatus{
		ListenerConfig: conf,
	}
	if offloadStatus, ok := l.rt.GetStatus(runtime.TypeOffload, "").(*stnrv1.OffloadStatus); ok {
		status.Stats = offloadStatus.Listeners[conf.Name]
	}
	return status
}

// AllocationCount returns the number of active allocations on the listener's TURN server.
func (l *Listener) AllocationCount() int {
	o, ok := l.rt.Registry.Get(runtime.TypeListenerServer, l.name)
	if !ok {
		return 0
	}
	s, ok := o.(interface {
		AllocationCount() int
	})
	if !ok {
		return 0
	}
	return s.AllocationCount()
}

// lookupAuthConfig is the runtime-backed cross-reference used at reconcile time to track auth
// realm changes.
func (l *Listener) lookupAuthConfig() *stnrv1.AuthConfig {
	a, _ := l.rt.GetConfig(runtime.TypeAuth, "").(*stnrv1.AuthConfig)
	return a
}

func (l *Listener) getOffload() objectturn.OffloadHandler {
	o, ok := l.rt.Registry.Get(runtime.TypeOffload, stnrv1.DefaultOffloadName)
	if !ok {
		return objectturn.NewOffloadHandlerStub()
	}
	p, ok := o.(interface {
		Handler() objectturn.OffloadHandler
	})
	if !ok {
		return objectturn.NewOffloadHandlerStub()
	}
	return p.Handler()
}
