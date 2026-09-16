package object

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"
	"github.com/pion/transport/v5"

	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/util"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// Cluster represents a set of upstream peers to which STUNner can relay traffic. Static clusters
// hold IP/CIDR endpoints; strict-DNS clusters resolve domain names in the background. The cluster
// holds the reconciled state as an atomic snapshot for the packet path and owns the strict-DNS
// domain registration lifecycle.
type Cluster struct {
	name     string
	resolver resolver.DnsResolver
	net      transport.Net

	// state is the atomic snapshot of reconciled cluster data, published by Reconcile and read
	// back (via GetConfig) by the Router and config consumers without locking.
	state atomic.Pointer[clusterState]

	// registered holds the strict-DNS domains registered with the resolver by Start, so Close
	// unregisters exactly those. Touched only on the reconcile path.
	registered []string

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// clusterState is the immutable snapshot of reconciled cluster data: the single source of truth
// for a cluster's type, protocol, endpoint set, and (for TURN-* protocol clusters) the upstream
// TURN server.
type clusterState struct {
	clusterType stnrv1.ClusterType
	protocol    stnrv1.ClusterProtocol
	endpoints   []*util.Endpoint
	domains     []string
	turnServer  *stnrv1.TURNServer
}

// NewCluster creates a Cluster object.
func NewCluster(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	if conf == nil {
		return &Cluster{
			resolver: rt.Resolver,
			net:      rt.Net,
			rt:       rt,
			log:      rt.Logger.NewLogger("cluster"),
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
		name:     req.Name,
		resolver: rt.Resolver,
		net:      rt.Net,
		rt:       rt,
		log:      rt.Logger.NewLogger(fmt.Sprintf("cluster-%s", req.Name)),
	}
	if err := c.Reconcile(req); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) Name() string             { return c.name }
func (c *Cluster) Type() runtime.ObjectType { return runtime.TypeCluster }

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

	// TURN-related changes (protocol flips to/from/between TURN-*, upstream server address or
	// credential changes) reconcile rather than restart: live upstream TURN sessions belong to
	// the allocations, not to the cluster object, and new allocations dial from the published
	// snapshot.
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
	// Validate vouched for the type and the protocol
	clusterType, _ := stnrv1.NewClusterType(req.Type)
	protocol, _ := stnrv1.NewClusterProtocol(req.Protocol)

	var endpoints []*util.Endpoint
	var domains []string
	switch clusterType {
	case stnrv1.ClusterTypeStatic:
		for _, e := range req.Endpoints {
			ep, err := util.ParseEndpoint(e)
			if err != nil {
				c.log.Warnf("cluster %q: could not parse endpoint %q (ignoring): %s",
					c.name, e, err.Error())
				continue
			}
			endpoints = append(endpoints, ep)
		}
	case stnrv1.ClusterTypeStrictDNS:
		if c.resolver == nil {
			return fmt.Errorf("sTRICT_DNS cluster %q initialized with no DNS resolver", c.name)
		}
		domains = append([]string(nil), req.Endpoints...)
	}

	// Publish the snapshot for the packet path.
	c.state.Store(&clusterState{
		clusterType: clusterType,
		protocol:    protocol,
		endpoints:   endpoints,
		domains:     domains,
		turnServer:  req.TURNServer,
	})

	c.rt.Router.InvalidateCache()
	return nil
}

func (c *Cluster) GetConfig() stnrv1.Config {
	conf := stnrv1.ClusterConfig{Name: c.name}
	state := c.state.Load()
	if state == nil {
		return &conf
	}
	conf.Protocol = state.protocol.String()
	conf.Type = state.clusterType.String()
	conf.TURNServer = state.turnServer
	switch state.clusterType {
	case stnrv1.ClusterTypeStatic:
		conf.Endpoints = make([]string, len(state.endpoints))
		for i, e := range state.endpoints {
			conf.Endpoints[i] = e.String()
		}
	case stnrv1.ClusterTypeStrictDNS:
		conf.Endpoints = make([]string, len(state.domains))
		copy(conf.Endpoints, state.domains)
		sort.Strings(conf.Endpoints)
	}
	return &conf
}

// Start registers the cluster's strict-DNS domains with the resolver.
func (c *Cluster) Start() error {
	domains := c.strictDNSDomains()
	for _, d := range domains {
		if err := c.resolver.Register(d); err != nil {
			return err
		}
	}
	c.registered = domains
	return nil
}

// Close unregisters the strict-DNS domains registered by Start and drops cached routing state.
func (c *Cluster) Close(_ bool) error {
	for _, d := range c.registered {
		c.resolver.Unregister(d)
	}
	c.registered = nil
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

// Protocol returns the cluster's protocol from the reconciled snapshot.
func (c *Cluster) Protocol() stnrv1.ClusterProtocol {
	if state := c.state.Load(); state != nil {
		return state.protocol
	}
	return stnrv1.ClusterProtocolUnknown
}

// TURNServer returns the upstream TURN server of a TURN-* protocol cluster, nil otherwise.
func (c *Cluster) TURNServer() *stnrv1.TURNServer {
	if state := c.state.Load(); state != nil {
		return state.turnServer
	}
	return nil
}

// Admits reports whether the cluster admits (peer, port), on the cluster's own terms: a TURN-*
// cluster admits every peer, since admission is then the upstream TURN server's job; a static
// cluster admits the peers its endpoints name; a strict-DNS cluster admits the addresses its
// domains resolve to (ports are not checked, domains carry none). port==0 ignores the port.
func (c *Cluster) Admits(peer net.IP, port int) bool {
	state := c.state.Load()
	if state == nil {
		return false
	}

	if state.protocol.IsTURN() {
		return true
	}

	switch state.clusterType {
	case stnrv1.ClusterTypeStatic:
		for _, e := range state.endpoints {
			if e.Match(peer, port) {
				return true
			}
		}
	case stnrv1.ClusterTypeStrictDNS:
		if c.resolver == nil {
			return false
		}
		for _, d := range state.domains {
			hosts, err := c.resolver.Lookup(d)
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

// strictDNSDomains returns the cluster's strict-DNS domains from the current snapshot, or nil
// for non-strict-DNS clusters. Used by Start/Close to (un)register domains with the resolver.
func (c *Cluster) strictDNSDomains() []string {
	state := c.state.Load()
	if state == nil || state.clusterType != stnrv1.ClusterTypeStrictDNS {
		return nil
	}
	return append([]string(nil), state.domains...)
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
