package object

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Cluster represents a set of upstream peers to which STUNner can relay traffic. Static clusters
// hold IP/CIDR endpoints; strict-DNS clusters resolve domain names in the background.
type Cluster struct {
	Name      string
	Type      stnrv1.ClusterType
	Protocol  stnrv1.ClusterProtocol
	Endpoints []*util.Endpoint
	Domains   []string
	Resolver  resolver.DnsResolver

	reg Registry
	log logging.LeveledLogger
}

// NewCluster creates a Cluster object.
func NewCluster(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	if conf == nil {
		return &Cluster{
			Endpoints: []*util.Endpoint{},
			Domains:   []string{},
			Resolver:  rt.Resolver,
			reg:       reg,
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
		Name:      req.Name,
		Endpoints: []*util.Endpoint{},
		Domains:   []string{},
		Resolver:  rt.Resolver,
		reg:       reg,
		log:       rt.Logger.NewLogger(fmt.Sprintf("cluster-%s", req.Name)),
	}
	if err := c.Reconcile(req); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) ObjectName() string { return c.Name }
func (c *Cluster) ObjectType() string { return TypeCluster }

func (c *Cluster) Extract(full *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	conf, err := full.GetClusterConfig(c.Name)
	if err != nil {
		return nil, err
	}
	return &conf, nil
}

func (c *Cluster) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	req, ok := new.(*stnrv1.ClusterConfig)
	if !ok {
		return ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*stnrv1.ClusterConfig)
	if cur.DeepEqual(req) {
		return ActionNone, nil
	}

	strictDNS := stnrv1.ClusterTypeStrictDNS.String()
	curStrictDNS := strings.EqualFold(cur.Type, strictDNS)
	reqStrictDNS := strings.EqualFold(req.Type, strictDNS)
	becomesStrictDNS := !curStrictDNS && reqStrictDNS
	leavesStrictDNS := curStrictDNS && !reqStrictDNS
	strictDNSEndpointsChanged := curStrictDNS && reqStrictDNS &&
		!sameStringSet(cur.Endpoints, req.Endpoints)

	if becomesStrictDNS || strictDNSEndpointsChanged || leavesStrictDNS {
		return ActionRestart, nil
	}

	return ActionReconcile, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)

	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}

	return true
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
	c.Name = req.Name
	c.Type, _ = stnrv1.NewClusterType(req.Type)
	c.Protocol, _ = stnrv1.NewClusterProtocol(req.Protocol)

	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		c.Endpoints = []*util.Endpoint{}
		for _, e := range req.Endpoints {
			ep, err := util.ParseEndpoint(e)
			if err != nil {
				c.log.Warnf("cluster %q: could not parse endpoint %q (ignoring): %s",
					c.Name, e, err.Error())
				continue
			}
			c.Endpoints = append(c.Endpoints, ep)
		}
	case stnrv1.ClusterTypeStrictDNS:
		if c.Resolver == nil {
			return fmt.Errorf("sTRICT_DNS cluster %q initialized with no DNS resolver", c.Name)
		}
		c.Domains = append([]string(nil), req.Endpoints...)
	}
	return nil
}

func (c *Cluster) GetConfig() stnrv1.Config {
	conf := stnrv1.ClusterConfig{
		Name:     c.Name,
		Protocol: c.Protocol.String(),
		Type:     c.Type.String(),
	}
	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		conf.Endpoints = make([]string, len(c.Endpoints))
		for i, e := range c.Endpoints {
			conf.Endpoints[i] = e.String()
		}
	case stnrv1.ClusterTypeStrictDNS:
		conf.Endpoints = make([]string, len(c.Domains))
		copy(conf.Endpoints, c.Domains)
		conf.Endpoints = sort.StringSlice(conf.Endpoints)
	}
	return &conf
}

func (c *Cluster) Start() error {
	switch c.Type {
	case stnrv1.ClusterTypeStatic:
	case stnrv1.ClusterTypeStrictDNS:
		for _, h := range c.Domains {
			if err := c.Resolver.Register(h); err != nil {
				c.log.Warnf("failed to register domain %s in the DNS resolver: %s",
					h, err.Error())
			}
		}
	}
	return nil
}

func (c *Cluster) Close(_ bool) error {
	c.log.Trace("closing cluster")
	switch c.Type {
	case stnrv1.ClusterTypeStatic:
	case stnrv1.ClusterTypeStrictDNS:
		for _, d := range c.Domains {
			c.Resolver.Unregister(d)
		}
	}
	return nil
}

func (c *Cluster) Status() stnrv1.Status {
	stats := stnrv1.OffloadDirStat{}
	if c.reg != nil {
		if o, ok := c.reg.LookupOne(TypeOffload); ok {
			stats = o.(*Offload).Handler().Stats(c.Name, stnrv1.ClusterStat)
		}
	}
	return &stnrv1.ClusterStatus{
		ClusterConfig: c.GetConfig().(*stnrv1.ClusterConfig),
		Stats:         stats,
	}
}

// Route returns true if peer is in the cluster's endpoint set.
func (c *Cluster) Route(peer net.IP) bool { return c.Match(peer, 0) }

// Match returns true if (peer, port) is in the cluster's endpoint set. If port==0, port is
// ignored.
func (c *Cluster) Match(peer net.IP, port int) bool {
	c.log.Tracef("match: cluster %q type %s peer %s", c.Name, c.Type.String(), peer.String())
	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		for _, e := range c.Endpoints {
			if e.Match(peer, port) {
				return true
			}
		}
	case stnrv1.ClusterTypeStrictDNS:
		c.log.Tracef("route: STRICT_DNS cluster with domains: [%s]", strings.Join(c.Domains, ", "))
		for _, d := range c.Domains {
			hs, err := c.Resolver.Lookup(d)
			if err != nil {
				c.log.Infof("could not resolve domain %q: %s", d, err.Error())
			}
			for _, n := range hs {
				if n.Equal(peer) {
					return true
				}
			}
		}
	}
	return false
}
