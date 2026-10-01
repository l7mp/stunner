package object

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/dialer"
	"github.com/l7mp/stunner/v2/internal/router"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Cluster is a cluster config, the router over its endpoints and the dialer that reaches them.
type Cluster struct {
	name string

	conf       atomic.Pointer[stnrv2.ClusterConfig]
	router     atomic.Pointer[api.Router]
	dialer     atomic.Pointer[api.Dialer]
	registered []string // the domains registered with the resolver

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

var (
	_ api.Router = &Cluster{}
	_ api.Dialer = &Cluster{}
)

// NewCluster creates a cluster object.
func NewCluster(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.ClusterConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	c := &Cluster{
		name: req.Name,
		rt:   rt,
		log:  rt.Logger.NewLogger(fmt.Sprintf("cluster-%s", req.Name)),
	}
	if err := c.Reconcile(req); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) Name() string             { return c.name }
func (c *Cluster) Type() runtime.ObjectType { return runtime.TypeCluster }

// Inspect restarts on a dialer change; the router and the addresses reconcile in place.
func (c *Cluster) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.ClusterConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return runtime.ActionNone, err
	}
	cur := old.(*stnrv2.ClusterConfig)
	if cur.DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	if cur.Protocol != req.Protocol || !reflect.DeepEqual(cur.Tunnel, req.Tunnel) {
		return runtime.ActionRestart, nil
	}
	return runtime.ActionReconcile, nil
}

// Reconcile rebuilds the router, and moves the resolver registrations of a running cluster to
// the new domains.
func (c *Cluster) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.ClusterConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	cp := &stnrv2.ClusterConfig{}
	req.DeepCopyInto(cp)

	r, err := router.New(cp, c.rt, c.log)
	if err != nil {
		return err
	}
	if c.dialer.Load() != nil {
		if err := c.register(domains(cp)); err != nil {
			return err
		}
	}
	c.conf.Store(cp)
	c.router.Store(&r)
	return nil
}

// register registers ds with the resolver and unregisters the domains registered before.
func (c *Cluster) register(ds []string) error {
	for _, d := range ds {
		if err := c.rt.Resolver.Register(d); err != nil {
			return err
		}
	}
	for _, d := range c.registered {
		c.rt.Resolver.Unregister(d)
	}
	c.registered = ds
	return nil
}

func (c *Cluster) GetConfig() stnrv2.Config {
	cp := &stnrv2.ClusterConfig{Name: c.name}
	if conf := c.conf.Load(); conf != nil {
		conf.DeepCopyInto(cp)
	}
	return cp
}

// Start runs the dialer and registers the domains with the resolver.
func (c *Cluster) Start() error {
	d, err := dialer.New(c.conf.Load(), c.rt)
	if err != nil {
		return err
	}
	if err := d.Start(); err != nil {
		return err
	}
	c.dialer.Store(&d)
	return c.register(domains(c.conf.Load()))
}

// Close unregisters the domains and stops the dialer. Dialed conns stay up.
func (c *Cluster) Close(shutdown bool) error {
	_ = c.register(nil)
	if old := c.dialer.Swap(nil); old != nil {
		return (*old).Close(shutdown)
	}
	return nil
}

func (c *Cluster) Status() stnrv2.Status {
	status := &stnrv2.ClusterStatus{ClusterConfig: c.GetConfig().(*stnrv2.ClusterConfig)}
	if offloadStatus, ok := c.rt.GetStatus(runtime.TypeOffload, "").(*stnrv2.OffloadStatus); ok {
		status.Stats = offloadStatus.Clusters[c.name]
	}
	return status
}

func (c *Cluster) Route(dst netip.AddrPort) (netip.AddrPort, bool, error) {
	return (*c.router.Load()).Route(dst)
}

func (c *Cluster) Protocol() stnrv2.Protocol {
	p, _ := stnrv2.NewClusterProtocol(c.conf.Load().Protocol)
	return p
}

var errNotRunning = errors.New("cluster is not running")

func (c *Cluster) Dial(ctx context.Context, local net.Addr, addr string) (api.Conn, error) {
	if d := c.dialer.Load(); d != nil {
		return (*d).Dial(ctx, local, addr)
	}
	return nil, fmt.Errorf("%s: %w", c.name, errNotRunning)
}

func (c *Cluster) ListenPacket(network string, port int) (net.PacketConn, net.Addr, error) {
	if d := c.dialer.Load(); d != nil {
		return (*d).ListenPacket(network, port)
	}
	return nil, nil, fmt.Errorf("%s: %w", c.name, errNotRunning)
}

func (c *Cluster) Listen(network string, port int) (net.Listener, net.Addr, error) {
	if d := c.dialer.Load(); d != nil {
		return (*d).Listen(network, port)
	}
	return nil, nil, fmt.Errorf("%s: %w", c.name, errNotRunning)
}

// domains returns the domain endpoints of a config, whatever its type.
func domains(conf *stnrv2.ClusterConfig) []string {
	ret := []string{}
	for _, ep := range conf.Endpoints {
		if e, err := stnrv2.ParseEndpoint(ep); err == nil && e.Domain != "" {
			ret = append(ret, e.Domain)
		}
	}
	slices.Sort(ret)
	return slices.Compact(ret)
}
