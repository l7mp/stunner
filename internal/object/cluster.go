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

// Listener implements a STUNner cluster
type Cluster struct {
	Name      string
	Type      stnrv1.ClusterType
	Protocol  stnrv1.ClusterProtocol
	Endpoints []*util.Endpoint
	Domains   []string
	Resolver  resolver.DnsResolver // for strict DNS

	logger logging.LoggerFactory
	log    logging.LeveledLogger
}

// NewCluster creates a new cluster.
func NewCluster(conf stnrv1.Config, resolver resolver.DnsResolver, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*stnrv1.ClusterConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}

	// make sure req.Name is correct
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c := Cluster{
		Name:      req.Name,
		Endpoints: []*util.Endpoint{},
		Domains:   []string{},
		Resolver:  resolver,
		logger:    logger,
		log:       logger.NewLogger(fmt.Sprintf("stunner-cluster-%s", req.Name)),
	}

	c.log.Tracef("NewCluster: %s", req.String())

	if err := c.Reconcile(req); err != nil && err != ErrRestartRequired {
		return nil, err
	}

	return &c, nil
}

// Inspect examines whether a configuration change requires a reconciliation (returns true if it
// does) or restart (returns ErrRestartRequired).
func (c *Cluster) Inspect(old, new, full stnrv1.Config) (bool, error) {
	return !old.DeepEqual(new), nil
}

// Reconcile updates the authenticator for a new configuration.
func (c *Cluster) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.ClusterConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}

	if err := req.Validate(); err != nil {
		return err
	}

	c.log.Tracef("Reconcile: %s", req.String())
	c.Type, _ = stnrv1.NewClusterType(req.Type)
	c.Protocol, _ = stnrv1.NewClusterProtocol(req.Protocol)

	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		// remove existing endpoints and start anew
		c.Endpoints = c.Endpoints[:0]
		for _, e := range req.Endpoints {
			// try to parse as a subnet
			ep, err := util.ParseEndpoint(e)
			if err != nil {
				c.log.Warnf("cluster %q: could not parse endpoint %q ",
					"(ignoring): %s", c.Name, e, err.Error())
			}

			c.Endpoints = append(c.Endpoints, ep)
		}
	case stnrv1.ClusterTypeStrictDNS:
		// TODO: port-range support for DNS clusters
		if c.Resolver == nil {
			return fmt.Errorf("STRICT_DNS cluster %q initialized with no DNS resolver", c.Name)
		}

		deleted, added := util.Diff(c.Domains, req.Endpoints)

		for _, h := range deleted {
			c.Resolver.Unregister(h)
			c.Domains = util.Remove(c.Domains, h)
		}

		for _, h := range added {
			if c.Resolver.Register(h) == nil {
				c.Domains = append(c.Domains, h)
			}
		}
	}

	return nil
}

// ObjectName returns the name of the object.
func (c *Cluster) ObjectName() string {
	// singleton!
	return c.Name
}

// ObjectType returns the type of the object.
func (c *Cluster) ObjectType() string {
	return "cluster"
}

// GetConfig returns the configuration of the running cluster.
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

// Close closes the cluster.
func (c *Cluster) Close() error {
	c.log.Trace("closing cluster")

	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		// do nothing
	case stnrv1.ClusterTypeStrictDNS:
		for _, d := range c.Domains {
			c.Resolver.Unregister(d)
		}
	}

	return nil
}

// Status returns the status of the object.
func (c *Cluster) Status() stnrv1.Status {
	return c.GetConfig()
}

// Route decides whether a peer IP appears among the permitted endpoints of a cluster.
func (c *Cluster) Route(peer net.IP) bool {
	return c.Match(peer, 0)
}

// Match decides whether a peer IP and port matches one of the permitted endpoints of a cluster. If
// port is zero then port-matching is disabled.
func (c *Cluster) Match(peer net.IP, port int) bool {
	c.log.Tracef("Match: cluster %q of type %s, peer IP: %s", c.Name, c.Type.String(),
		peer.String())

	switch c.Type {
	case stnrv1.ClusterTypeStatic:
		// endpoints are IPNets
		c.log.Tracef("route: STATIC cluster with %d endpoints", len(c.Endpoints))

		for _, e := range c.Endpoints {
			c.log.Tracef("considering endpoint %q", e)
			if e.Match(peer, port) {
				return true
			}
		}

	case stnrv1.ClusterTypeStrictDNS:
		// endpoints are obtained from the DNS
		c.log.Tracef("route: STRICT_DNS cluster with domains: [%s]", strings.Join(c.Domains, ", "))

		for _, d := range c.Domains {
			c.log.Tracef("considering domain %q", d)

			hs, err := c.Resolver.Lookup(d)
			if err != nil {
				c.log.Infof("could not resolve domain %q: %s", d, err.Error())
			}

			for _, n := range hs {
				c.log.Tracef("considering IP address %q", n)
				if n.Equal(peer) {
					return true
				}
			}
		}
	}

	return false
}

// ClusterFactory can create now Cluster objects
type ClusterFactory struct {
	resolver resolver.DnsResolver
	logger   logging.LoggerFactory
}

// NewClusterFactory creates a new factory for Cluster objects
func NewClusterFactory(resolver resolver.DnsResolver, logger logging.LoggerFactory) Factory {
	return &ClusterFactory{resolver: resolver, logger: logger}
}

// New can produce a new Cluster object from the given configuration. A nil config will create an
// empty cluster object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *ClusterFactory) New(conf stnrv1.Config) (Object, error) {
	if conf == nil {
		return &Cluster{}, nil
	}

	return NewCluster(conf, f.resolver, f.logger)
}
