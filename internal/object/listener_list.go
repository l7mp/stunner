package object

import (
	"fmt"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ListenerList is the singleton collection Object that owns the N × Listener sub-tree. The
// Reconciler walks this Object's sub-manager to drive per-Listener diffs.
type ListenerList struct {
	reg Registry
	log logging.LeveledLogger
}

// ListenerListConfig wraps the slice of ListenerConfigs from the parent StunnerConfig.
type ListenerListConfig struct {
	Listeners []stnrv1.ListenerConfig
}

func (c *ListenerListConfig) Validate() error    { return nil }
func (c *ListenerListConfig) ConfigName() string { return DefaultListenerListName }
func (c *ListenerListConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*ListenerListConfig)
	if !ok {
		return false
	}
	if len(c.Listeners) != len(o.Listeners) {
		return false
	}
	for i := range c.Listeners {
		a, b := c.Listeners[i], o.Listeners[i]
		if !a.DeepEqual(&b) {
			return false
		}
	}
	return true
}
func (c *ListenerListConfig) DeepCopyInto(dst stnrv1.Config) {
	d, ok := dst.(*ListenerListConfig)
	if !ok {
		return
	}
	d.Listeners = append([]stnrv1.ListenerConfig(nil), c.Listeners...)
}
func (c *ListenerListConfig) String() string {
	return fmt.Sprintf("ListenerListConfig{n=%d}", len(c.Listeners))
}

// NewListenerList creates the singleton ListenerList object.
func NewListenerList(_ stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	return &ListenerList{
		reg: reg,
		log: rt.Logger.NewLogger("listeners"),
	}, nil
}

func (l *ListenerList) ObjectName() string { return DefaultListenerListName }
func (l *ListenerList) ObjectType() string { return TypeListenerList }

// Extract returns a snapshot of the listener configs from the full config. Used by the
// Reconciler for diff/inspect on the list as a whole; the per-Listener sub-manager has its own
// list-extractor that produces one ListenerConfig per listener.
func (l *ListenerList) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	out := append([]stnrv1.ListenerConfig(nil), c.Listeners...)
	return &ListenerListConfig{Listeners: out}, nil
}

func (l *ListenerList) GetConfig() stnrv1.Config {
	conf := &ListenerListConfig{Listeners: []stnrv1.ListenerConfig{}}
	if l.reg == nil {
		return conf
	}

	for _, o := range l.reg.LookupAll(TypeListener) {
		conf.Listeners = append(conf.Listeners, *o.GetConfig().(*stnrv1.ListenerConfig))
	}

	return conf
}

func (l *ListenerList) Status() stnrv1.Status {
	// Returns the list config; per-Listener status is aggregated at the Stunner-root level.
	return l.GetConfig()
}

func (l *ListenerList) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	return ActionNone, nil
}
func (l *ListenerList) Reconcile(_ stnrv1.Config) error { return nil }
func (l *ListenerList) Start() error                    { return nil }
func (l *ListenerList) Close(_ bool) error              { return nil }
