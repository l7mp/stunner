package object

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/object/tcp"
	"github.com/l7mp/stunner/internal/object/udp"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Cluster represents a set of upstream peers to which STUNner can relay traffic. Static clusters
// hold IP/CIDR endpoints; strict-DNS clusters resolve domain names in the background. The actual
// relay implementations live in the cluster's per-protocol RelayNode children; the cluster only
// holds the reconciled endpoint state, published as an atomic snapshot for the packet path.
type Cluster struct {
	name      string
	clusterTy stnrv1.ClusterType
	Protocol  stnrv1.ClusterProtocol
	Endpoints []*util.Endpoint
	Domains   []string
	Resolver  resolver.DnsResolver
	Net       transport.Net

	// state is the atomic snapshot read by Match on the packet path.
	state atomic.Pointer[clusterState]

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// clusterState is the immutable endpoint snapshot published by Reconcile and read by the
// dataplane (Match) without locking.
type clusterState struct {
	clusterTy stnrv1.ClusterType
	endpoints []*util.Endpoint
	domains   []string
}

// NewCluster creates a Cluster object.
func NewCluster(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	if conf == nil {
		return &Cluster{
			Endpoints: []*util.Endpoint{},
			Domains:   []string{},
			Resolver:  rt.Resolver,
			Net:       rt.Net,
			rt:        rt,
			log:       rt.Logger.NewLogger("cluster"),
		}, nil
	}
	req, ok := conf.(*stnrv1.ClusterConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	c := &Cluster{
		name:      req.Name,
		Endpoints: []*util.Endpoint{},
		Domains:   []string{},
		Resolver:  rt.Resolver,
		Net:       rt.Net,
		rt:        rt,
		log:       rt.Logger.NewLogger(fmt.Sprintf("cluster-%s", req.Name)),
	}
	if err := c.Reconcile(req); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) Name() string             { return c.name }
func (c *Cluster) Type() runtime.ObjectType { return runtime.TypeCluster }

// Protocols returns the relay protocols this cluster serves. The relay-node kind derives the
// desired relay set from this, so adding a protocol here spawns a new relay node on the next
// reconcile without bouncing the existing ones.
func (c *Cluster) Protocols() []stnrv1.ClusterProtocol {
	return []stnrv1.ClusterProtocol{c.Protocol}
}

func (c *Cluster) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv1.ClusterConfig)
	if !ok {
		return runtime.ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*stnrv1.ClusterConfig)
	if cur.DeepEqual(req) {
		return runtime.ActionNone, nil
	}

	strictDNS := stnrv1.ClusterTypeStrictDNS.String()
	curStrictDNS := strings.EqualFold(cur.Type, strictDNS)
	reqStrictDNS := strings.EqualFold(req.Type, strictDNS)
	becomesStrictDNS := !curStrictDNS && reqStrictDNS
	leavesStrictDNS := curStrictDNS && !reqStrictDNS
	strictDNSEndpointsChanged := curStrictDNS && reqStrictDNS &&
		!sameStringSet(cur.Endpoints, req.Endpoints)

	if becomesStrictDNS || strictDNSEndpointsChanged || leavesStrictDNS {
		return runtime.ActionRestart, nil
	}

	return runtime.ActionReconcile, nil
}

func (c *Cluster) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.ClusterConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	c.log.Tracef("reconcile: %s", req.String())
	c.name = req.Name
	c.clusterTy, _ = stnrv1.NewClusterType(req.Type)
	c.Protocol, _ = stnrv1.NewClusterProtocol(req.Protocol)
	c.Endpoints = []*util.Endpoint{}
	c.Domains = []string{}

	switch c.Protocol {
	case stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP:
	default:
		return fmt.Errorf("unsupported cluster protocol %q", c.Protocol.String())
	}

	switch c.clusterTy {
	case stnrv1.ClusterTypeStatic:
		for _, e := range req.Endpoints {
			ep, err := util.ParseEndpoint(e)
			if err != nil {
				c.log.Warnf("cluster %q: could not parse endpoint %q (ignoring): %s",
					c.name, e, err.Error())
				continue
			}
			c.Endpoints = append(c.Endpoints, ep)
		}
	case stnrv1.ClusterTypeStrictDNS:
		if c.Resolver == nil {
			return fmt.Errorf("sTRICT_DNS cluster %q initialized with no DNS resolver", c.name)
		}
		c.Domains = append([]string(nil), req.Endpoints...)
	}

	// Publish the endpoint snapshot for the packet path.
	c.state.Store(&clusterState{
		clusterTy: c.clusterTy,
		endpoints: append([]*util.Endpoint(nil), c.Endpoints...),
		domains:   append([]string(nil), c.Domains...),
	})

	c.rt.Router.InvalidateCache()
	return nil
}

func (c *Cluster) GetConfig() stnrv1.Config {
	conf := stnrv1.ClusterConfig{
		Name:     c.name,
		Protocol: c.Protocol.String(),
		Type:     c.clusterTy.String(),
	}
	state := c.state.Load()
	if state == nil {
		return &conf
	}
	switch state.clusterTy {
	case stnrv1.ClusterTypeStatic:
		conf.Endpoints = make([]string, len(state.endpoints))
		for i, e := range state.endpoints {
			conf.Endpoints[i] = e.String()
		}
	case stnrv1.ClusterTypeStrictDNS:
		conf.Endpoints = make([]string, len(state.domains))
		copy(conf.Endpoints, state.domains)
		conf.Endpoints = sort.StringSlice(conf.Endpoints)
	}
	return &conf
}

func (c *Cluster) Start() error {
	return nil
}

func (c *Cluster) Close(_ bool) error {
	c.rt.Router.InvalidateCache()
	return nil
}

func (c *Cluster) Status() stnrv1.Status {
	status := &stnrv1.ClusterStatus{
		ClusterConfig: c.GetConfig().(*stnrv1.ClusterConfig),
	}
	if offloadStatus, ok := c.rt.GetStatus(runtime.TypeOffload, "").(*stnrv1.OffloadStatus); ok {
		status.Stats = offloadStatus.Clusters[c.name]
	}
	return status
}

// Route returns true if peer is in the cluster's endpoint set.
func (c *Cluster) Route(peer net.IP) bool { return c.Match(peer, 0) }

// Match returns true if (peer, port) is in the cluster's endpoint set. If port==0, port is
// ignored. Safe for concurrent use: reads the endpoint snapshot published by Reconcile.
func (c *Cluster) Match(peer net.IP, port int) bool {
	state := c.state.Load()
	if state == nil {
		return false
	}
	c.log.Tracef("match: cluster %q type %s peer %s", c.name, state.clusterTy.String(), peer.String())
	switch state.clusterTy {
	case stnrv1.ClusterTypeStatic:
		for _, e := range state.endpoints {
			if e.Match(peer, port) {
				return true
			}
		}
	case stnrv1.ClusterTypeStrictDNS:
		for _, d := range state.domains {
			hosts, err := c.Resolver.Lookup(d)
			if err != nil {
				continue
			}
			for _, h := range hosts {
				if h.Equal(peer) {
					return true
				}
			}
		}
	}
	return false
}

// Relay returns the relay node for a protocol, resolved from the registry.
func (c *Cluster) Relay(proto stnrv1.ClusterProtocol) (runtime.Relay, bool) {
	return c.rt.GetRelay(c.name, proto)
}

// newRelayImpl builds the protocol-specific relay implementation from the cluster's current
// endpoint snapshot. Called by the cluster's RelayNode children.
func (c *Cluster) newRelayImpl(proto stnrv1.ClusterProtocol) (runtime.ManagedRelay, error) {
	state := c.state.Load()
	if state == nil {
		state = &clusterState{}
	}
	switch proto {
	case stnrv1.ClusterProtocolUDP:
		return udp.NewRelay(c.name, state.clusterTy, state.endpoints, state.domains, c.Resolver, c.Net), nil
	case stnrv1.ClusterProtocolTCP:
		return tcp.NewRelay(c.name, state.clusterTy, state.endpoints, state.domains, c.Resolver, c.Net), nil
	default:
		return nil, fmt.Errorf("unsupported cluster protocol %q", proto.String())
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	for i := range aa {
		aa[i] = strings.ToLower(aa[i])
	}
	for i := range bb {
		bb[i] = strings.ToLower(bb[i])
	}
	sort.Strings(aa)
	sort.Strings(bb)

	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}

	return true
}
