package object

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"sync"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/dialer"
	"github.com/l7mp/stunner/v2/internal/router"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Cluster is a cluster object: the router over its endpoints and the dialer that reaches them.
// Reconcile writes the fields under mu, the data path reads them.
type Cluster struct {
	name  string
	rt    *runtime.Runtime
	log   logging.LeveledLogger
	scope string // of the logger

	mu            sync.RWMutex
	typ           string
	endpoints     []string
	routingPolicy string
	protocol      stnrv2.Protocol
	tunnel        *stnrv2.TunnelConfig
	addrs         []string
	filters       []string
	router        api.Router
	dialer        api.Dialer
	running       bool
	registered    []string // the domains registered with the resolver
}

var (
	_ api.Router     = &Cluster{}
	_ api.Dialer     = &Cluster{}
	_ runtime.Object = &Cluster{}
)

// NewCluster creates a cluster object.
func NewCluster(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	req, err := desiredCluster(conf)
	if err != nil {
		return nil, err
	}
	c := &Cluster{
		name:  req.Name,
		rt:    rt,
		scope: fmt.Sprintf("cluster-%s", req.Name),
	}
	c.log = rt.Logger.NewLogger(c.scope)
	if err := c.Reconcile(req); err != nil {
		return nil, err
	}
	return c, nil
}

// desiredCluster returns the validated cluster config of a New, an Inspect or a Reconcile.
func desiredCluster(conf stnrv2.Config) (*stnrv2.ClusterConfig, error) {
	req, ok := conf.(*stnrv2.ClusterConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return req, nil
}

func (c *Cluster) Name() string             { return c.name }
func (c *Cluster) Type() runtime.ObjectType { return runtime.TypeCluster }

// Inspect reconciles every change in place: a dialer change swaps the dialer, live conns keep
// theirs.
func (c *Cluster) Inspect(old, new stnrv2.Config, full *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, err := desiredCluster(new)
	if err != nil {
		return runtime.ActionNone, err
	}
	if full != nil {
		for _, name := range req.Filters {
			if _, err := full.GetFilterConfig(name); err != nil {
				c.log.Warnf("cluster %s: no filter %q: it admits every peer", c.name, name)
			}
		}
	}
	if old.(*stnrv2.ClusterConfig).DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	return runtime.ActionReconcile, nil
}

// Reconcile rebuilds the router, swaps the dialer on a change of its protocol, tunnel or addresses,
// and moves the resolver registrations of a running cluster to the new domains.
func (c *Cluster) Reconcile(conf stnrv2.Config) error {
	req, err := desiredCluster(conf)
	if err != nil {
		return err
	}
	if req.Name != c.name {
		return fmt.Errorf("cannot rename cluster %q to %q", c.name, req.Name)
	}
	proto, _ := stnrv2.NewClusterProtocol(req.Protocol)
	r, err := router.New(req, c.rt, c.log)
	if err != nil {
		return err
	}

	old, d := c.dialer, c.dialer
	if old == nil || c.protocol != proto || !reflect.DeepEqual(c.tunnel, req.Tunnel) ||
		!slices.Equal(c.addrs, req.Addrs) {
		if d, err = dialer.New(req, c.rt, c.admits); err != nil {
			return err
		}
		if c.running {
			if err := d.Start(); err != nil {
				return err
			}
		}
	}

	var tunnel *stnrv2.TunnelConfig
	if req.Tunnel != nil {
		tunnel = req.Tunnel.DeepCopy()
	}
	c.mu.Lock()
	c.typ, c.endpoints, c.routingPolicy = req.Type, slices.Clone(req.Endpoints), req.RoutingPolicy
	c.protocol, c.tunnel, c.addrs = proto, tunnel, slices.Clone(req.Addrs)
	c.filters, c.router, c.dialer = slices.Clone(req.Filters), r, d
	c.mu.Unlock()
	if old != nil && old != d {
		_ = old.Close(false)
	}
	if c.running {
		return c.register(c.domains())
	}
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
	c.mu.RLock()
	conf := &stnrv2.ClusterConfig{
		Name:          c.name,
		Type:          c.typ,
		Endpoints:     slices.Clone(c.endpoints),
		RoutingPolicy: c.routingPolicy,
		Protocol:      c.protocol.String(),
		Addrs:         slices.Clone(c.addrs),
		Filters:       slices.Clone(c.filters),
	}
	if c.tunnel != nil {
		conf.Tunnel = c.tunnel.DeepCopy()
	}
	c.mu.RUnlock()
	_ = conf.Validate()
	return conf
}

// Start runs the dialer and registers the domains with the resolver.
func (c *Cluster) Start() error {
	if err := c.dialer.Start(); err != nil {
		return err
	}
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	return c.register(c.domains())
}

// Close unregisters the domains and stops the dialer. Dialed conns stay up.
func (c *Cluster) Close(shutdown bool) error {
	_ = c.register(nil)
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return c.dialer.Close(shutdown)
}

func (c *Cluster) Status() stnrv2.Status {
	status := &stnrv2.ClusterStatus{ClusterConfig: c.GetConfig().(*stnrv2.ClusterConfig)}
	if offloadStatus, ok := c.rt.GetStatus(runtime.TypeOffload, "").(*stnrv2.OffloadStatus); ok {
		status.Stats = offloadStatus.Clusters[c.name]
	}
	return status
}

func (c *Cluster) Targets() []netip.AddrPort {
	c.mu.RLock()
	r := c.router
	c.mu.RUnlock()
	return r.Targets()
}

func (c *Cluster) Protocol() stnrv2.Protocol {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.protocol
}

var errNotRunning = errors.New("cluster is not running")

// runningDialer returns the dialer of a running cluster.
func (c *Cluster) runningDialer() (api.Dialer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.running {
		return nil, fmt.Errorf("%s: %w", c.name, errNotRunning)
	}
	return c.dialer, nil
}

func (c *Cluster) Dial(ctx context.Context, local net.Addr, addr string) (api.Conn, error) {
	d, err := c.runningDialer()
	if err != nil {
		return nil, err
	}
	return d.Dial(ctx, local, addr)
}

func (c *Cluster) ListenPacket(network string, port int) (net.PacketConn, net.Addr, error) {
	d, err := c.runningDialer()
	if err != nil {
		return nil, nil, err
	}
	return d.ListenPacket(network, port)
}

func (c *Cluster) Listen(network string, port int) (net.Listener, net.Addr, error) {
	d, err := c.runningDialer()
	if err != nil {
		return nil, nil, err
	}
	return d.Listen(network, port)
}

// admits lets through the traffic of the peers the filters of the cluster admit, as they stand at
// the moment: a peer no rule matches passes.
func (c *Cluster) admits(peer net.Addr) bool {
	c.mu.RLock()
	filters := c.filters
	c.mu.RUnlock()
	if len(filters) == 0 {
		return true
	}
	allow, matched := c.rt.Match(filters, peer)
	ok := allow || !matched
	if !ok && c.rt.Tracing(c.scope) {
		c.log.Tracef("cluster %s: filters drop peer %s", c.name, peer.String())
	}
	return ok
}

// domains returns the domain endpoints the cluster resolves.
func (c *Cluster) domains() []string {
	ret := []string{}
	for _, ep := range c.endpoints {
		if e, err := stnrv2.ParseEndpoint(ep); err == nil && e.Domain != "" {
			ret = append(ret, e.Domain)
		}
	}
	slices.Sort(ret)
	return slices.Compact(ret)
}
