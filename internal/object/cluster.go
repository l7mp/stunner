package object

import (
	"fmt"
	"sort"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Listener implements a STUNner cluster
type Cluster struct {
	Name string
	Type v1alpha1.ClusterType
	Endpoints []string
	log logging.LeveledLogger
}

// NewListener creates a new cluster. Requires a server restart (returns ErrRestartRequired)
func NewCluster(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
        req, ok := conf.(*v1alpha1.ClusterConfig)
        if !ok {
                return nil, ErrInvalidConf
        }
        
        // make sure req.Name is correct
        if err := req.Validate(); err != nil {
                return nil, err
        }

	c := Cluster{
                Name:      req.Name,
                Endpoints: []string{},
                log:       logger.NewLogger(fmt.Sprintf("stunner-cluster-%s", req.Name)),
        }

        c.log.Tracef("NewCluster: %#v", req)

        if err := c.Reconcile(req); err != nil && err != ErrRestartRequired {
                return nil, err
        }

        return &c, ErrRestartRequired
}

// Reconcile updates a cluster. Does not require a server restart
func (c *Cluster) Reconcile(conf v1alpha1.Config) error {
        req, ok := conf.(*v1alpha1.ClusterConfig)
        if !ok {
                return ErrInvalidConf
        }
        
        c.log.Tracef("Reconcile: %#v", req)

        if err := req.Validate(); err != nil {
                return err
        }

	c.Type, _ = v1alpha1.NewClusterType(req.Type)

        switch c.Type {
        case v1alpha1.ClusterTypeStatic:
                copy(c.Endpoints, req.Endpoints)
                
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
        // must be sorted!
        sort.Strings(c.Endpoints)
        copy(conf.Endpoints, c.Endpoints)

        return &conf
}

// Close closes the cluster
func (l *Cluster) Close() {
        l.log.Trace("closing cluster")
}
