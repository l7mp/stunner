package object

import (
	"fmt"
	"net"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Listener implements a STUNner cluster
type Cluster struct {
	Name string
	Type v1alpha1.ClusterType
	Endpoints []net.IPNet
	log logging.LeveledLogger
}

// NewListener creates a new cluster. Requires a server restart (returns v1alpha1.ErrRestartRequired)
func NewCluster(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
        req, ok := conf.(*v1alpha1.ClusterConfig)
        if !ok {
                return nil, v1alpha1.ErrInvalidConf
        }
        
        // make sure req.Name is correct
        if err := req.Validate(); err != nil {
                return nil, err
        }

	c := Cluster{
                Name:      req.Name,
                Endpoints: []net.IPNet{},
                log:       logger.NewLogger(fmt.Sprintf("stunner-cluster-%s", req.Name)),
        }

        c.log.Tracef("NewCluster: %#v", req)

        if err := c.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
                return nil, err
        }

        return &c, v1alpha1.ErrRestartRequired
}

// Reconcile updates a cluster. Does not require a server restart
func (c *Cluster) Reconcile(conf v1alpha1.Config) error {
        req, ok := conf.(*v1alpha1.ClusterConfig)
        if !ok {
                return v1alpha1.ErrInvalidConf
        }
        
        c.log.Tracef("Reconcile: %#v", req)

        if err := req.Validate(); err != nil {
                return err
        }

	c.Type, _ = v1alpha1.NewClusterType(req.Type)

        switch c.Type {
        case v1alpha1.ClusterTypeStatic:
                // remove existing endpoints and start anew
                c.Endpoints = c.Endpoints[:0]
                for _, e := range req.Endpoints {
                        // try to parse as a subnet
                        _, n, err := net.ParseCIDR(e)
                        if err == nil {
                                c.Endpoints = append(c.Endpoints, *n)
                                continue
                        }

                        // try to parse as an IP address
                        a := net.ParseIP(e)
                        if a == nil {
                                c.log.Warnf("cluster %q: invalid endpoint IP: %q, ignoring", c.Name, e)
                                continue
                        }

                        // add a prefix and reparse
                        if a.To4() == nil {
                                e = e + "/128"
                        } else {
                                e = e + "/32"
                        }

                        _, n2, err := net.ParseCIDR(e)
                        if err != nil {
                                c.log.Warnf("cluster %q: could not convert endpoint %q to CIDR subnet ",
                                        "(ignoring): %s", c.Name, e, err.Error())
                                continue
                        }

                        c.Endpoints = append(c.Endpoints, *n2)
                }        
        case v1alpha1.ClusterTypeStrictDns:
                panic("STRICT_DNS: unimplemented")
        }

        return nil
}

// Name returns the name of the object
func (c *Cluster) ObjectName() string {
        // singleton!
        return c.Name
}

// GetConfig returns the configuration of the running cluster
func (c *Cluster) GetConfig() v1alpha1.Config {
        conf := v1alpha1.ClusterConfig{
		Name:      c.Name,
		Type:      c.Type.String(),
                Endpoints: make([]string, len(c.Endpoints)),
	}

        for i, e := range c.Endpoints {
                conf.Endpoints[i] = e.String()
        }

        return &conf
}

// Close closes the cluster
func (l *Cluster) Close() error {
        l.log.Trace("closing cluster")
        return nil
}
