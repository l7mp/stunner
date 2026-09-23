package object

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// Listener implements a STUNner listener. It holds the reconciled config, published as an atomic
// snapshot for the packet path, and owns the Server that Start brings up from it.
type Listener struct {
	name, realm            string
	proto                  stnrv1.ListenerProtocol
	addr                   net.IP
	port, minPort, maxPort int
	publicAddr             string
	publicPort             int
	publicAddrs            []string
	rawAddr                string
	addrs                  []string
	cert, key              []byte
	peerAddr               string
	routes                 []string

	// conf is the atomic snapshot read by the server on the request path.
	conf atomic.Pointer[stnrv1.ListenerConfig]

	// server is the running packet server, nil while the listener is down.
	server server.Server

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewListener creates a Listener object.
func NewListener(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	if conf == nil {
		return &Listener{
			rt:  rt,
			log: rt.Logger.NewLogger("listener"),
		}, nil
	}
	req := conf.(*stnrv1.ListenerConfig)
	if err := req.Validate(); err != nil {
		return nil, err
	}
	name := req.Name
	l := &Listener{
		name: name,
		rt:   rt,
		log:  rt.Logger.NewLogger(fmt.Sprintf("listener-%s", name)),
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

	// A restart is only avoidable when Routes, PublicIP/PublicPort and/or PeerAddr are the
	// only changes. A peer address change reconciles in place: existing flows stay pinned to
	// the peer they were created with, new flows go to the new peer.
	restart := !(l.name == req.Name && //nolint:staticcheck
		l.proto == proto &&
		l.rawAddr == req.Addr &&
		l.port == req.Port &&
		bytes.Equal(l.cert, cert) &&
		bytes.Equal(l.key, key))

	curRealm := l.realm
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
	var ipAddr net.IP
	if proto != stnrv1.ProtocolSTDIN {
		// STDIN listeners have no listener socket and carry no address
		ipAddr = net.ParseIP(req.Addr)
		if ipAddr == nil && req.Addr == "localhost" {
			ipAddr = net.ParseIP("127.0.0.1")
		}
		if ipAddr == nil {
			return fmt.Errorf("invalid listener address: %s", req.Addr)
		}
	}

	l.proto = proto
	l.addr = ipAddr
	l.rawAddr = req.Addr
	l.port = req.Port
	if proto == stnrv1.ListenerProtocolTURNTLS || proto == stnrv1.ListenerProtocolTURNDTLS {
		cert, err := base64.StdEncoding.DecodeString(req.Cert)
		if err != nil {
			return fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
		}
		key, err := base64.StdEncoding.DecodeString(req.Key)
		if err != nil {
			return fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
		}
		l.cert = cert
		l.key = key
	}
	l.realm = stnrv1.DefaultRealm
	if a := l.lookupAuthConfig(); a != nil {
		l.realm = a.Realm
	}
	l.publicAddr = req.PublicAddr
	l.publicPort = req.PublicPort
	l.peerAddr = req.PeerAddr

	l.publicAddrs = make([]string, len(req.PublicAddrs))
	copy(l.publicAddrs, req.PublicAddrs)

	l.addrs = make([]string, len(req.Addrs))
	copy(l.addrs, req.Addrs)

	l.routes = make([]string, len(req.Routes))
	copy(l.routes, req.Routes)

	// Publish the snapshot for the TURN request path.
	l.conf.Store(l.buildConfig())

	l.rt.Router.InvalidateCache()
	return nil
}

// buildConfig renders the listener's live config from its fields. Only called from Reconcile;
// readers go through the snapshot.
func (l *Listener) buildConfig() *stnrv1.ListenerConfig {
	routes := make([]string, len(l.routes))
	copy(routes, l.routes)
	sort.Strings(routes)
	publicAddrs := make([]string, len(l.publicAddrs))
	copy(publicAddrs, l.publicAddrs)
	addrs := make([]string, len(l.addrs))
	copy(addrs, l.addrs)
	c := &stnrv1.ListenerConfig{
		Name:        l.name,
		Protocol:    l.proto.String(),
		Addr:        l.rawAddr,
		Addrs:       addrs,
		Port:        l.port,
		PublicAddr:  l.publicAddr,
		PublicPort:  l.publicPort,
		PublicAddrs: publicAddrs,
		PeerAddr:    l.peerAddr,
		Routes:      routes,
	}
	c.Cert = string(l.cert)
	c.Key = string(l.key)
	return c
}

// String returns a short stable representation, safe as a map key.
func (l *Listener) String() string {
	return fmt.Sprintf("%s: [%s://%s<%d:%d>]", l.name, strings.ToLower(l.proto.String()),
		net.JoinHostPort(l.addr.String(), strconv.Itoa(l.port)), l.minPort, l.maxPort)
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
	cp.PublicAddrs = make([]string, len(snap.PublicAddrs))
	copy(cp.PublicAddrs, snap.PublicAddrs)
	cp.Addrs = make([]string, len(snap.Addrs))
	copy(cp.Addrs, snap.Addrs)
	return &cp
}

// Start brings up the Server matching the listener protocol. Servers read the listener config
// back through the runtime, so the listener must already be registered.
func (l *Listener) Start() error {
	l.log.Infof("listener %s (re)starting", l.String())
	s, err := server.New(l.name, l.proto, l.rt)
	if err != nil {
		return fmt.Errorf("failed to start server for listener %s: %w", l.name, err)
	}
	l.server = s
	l.log.Infof("listener %s: listener running", l.name)
	return nil
}

// Close tears down the server and drops cached routing state.
func (l *Listener) Close(_ bool) error {
	l.rt.Router.InvalidateCache()
	if l.server == nil {
		return nil
	}
	err := l.server.Close()
	l.server = nil
	return err
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

// AllocationCount returns the number of active sessions on the listener's server.
func (l *Listener) AllocationCount() int {
	if l.server == nil {
		return 0
	}
	return l.server.AllocationCount()
}

// lookupAuthConfig is the runtime-backed cross-reference used at reconcile time to track auth
// realm changes.
func (l *Listener) lookupAuthConfig() *stnrv1.AuthConfig {
	a, _ := l.rt.GetConfig(runtime.TypeAuth, "").(*stnrv1.AuthConfig)
	return a
}
