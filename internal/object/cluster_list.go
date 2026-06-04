package object

import (
	"fmt"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ClusterList is the singleton collection Object for Clusters. Mirror of ListenerList.
type ClusterList struct {
	reg Registry
	log logging.LeveledLogger
}

// ClusterListConfig wraps the slice of ClusterConfigs from the parent StunnerConfig.
type ClusterListConfig struct {
	Clusters []stnrv1.ClusterConfig
}

func (c *ClusterListConfig) Validate() error    { return nil }
func (c *ClusterListConfig) ConfigName() string { return DefaultClusterListName }
func (c *ClusterListConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*ClusterListConfig)
	if !ok {
		return false
	}
	if len(c.Clusters) != len(o.Clusters) {
		return false
	}
	for i := range c.Clusters {
		a, b := c.Clusters[i], o.Clusters[i]
		if !a.DeepEqual(&b) {
			return false
		}
	}
	return true
}
func (c *ClusterListConfig) DeepCopyInto(dst stnrv1.Config) {
	d, ok := dst.(*ClusterListConfig)
	if !ok {
		return
	}
	d.Clusters = append([]stnrv1.ClusterConfig(nil), c.Clusters...)
}
func (c *ClusterListConfig) String() string {
	return fmt.Sprintf("ClusterListConfig{n=%d}", len(c.Clusters))
}

// NewClusterList creates the singleton ClusterList object.
func NewClusterList(_ stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	return &ClusterList{
		reg: reg,
		log: rt.Logger.NewLogger("clusters"),
	}, nil
}

func (l *ClusterList) ObjectName() string { return DefaultClusterListName }
func (l *ClusterList) ObjectType() string { return TypeClusterList }

func (l *ClusterList) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	out := append([]stnrv1.ClusterConfig(nil), c.Clusters...)
	return &ClusterListConfig{Clusters: out}, nil
}

func (l *ClusterList) GetConfig() stnrv1.Config {
	conf := &ClusterListConfig{Clusters: []stnrv1.ClusterConfig{}}
	if l.reg == nil {
		return conf
	}

	for _, o := range l.reg.LookupAll(TypeCluster) {
		conf.Clusters = append(conf.Clusters, *o.GetConfig().(*stnrv1.ClusterConfig))
	}

	return conf
}

func (l *ClusterList) Status() stnrv1.Status { return l.GetConfig() }

func (l *ClusterList) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	return ActionNone, nil
}
func (l *ClusterList) Reconcile(_ stnrv1.Config) error { return nil }
func (l *ClusterList) Start() error                    { return nil }
func (l *ClusterList) Close(_ bool) error              { return nil }
