package object

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/l7mp/stunner/internal/offload"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Offload is the reconciliation representative of the process-wide offload engine
// (rt.OffloadEngine). It owns no engine lifecycle — the engine is created/started at server
// startup and closed at shutdown — it only pushes config changes (engine mode, interfaces) to the
// engine in place and surfaces its statistics as status.
type Offload struct {
	rt *runtime.Runtime

	// conf is the atomic snapshot read via Admin.GetConfig on the allocation path.
	conf atomic.Pointer[OffloadConfig]
}

// OffloadConfig is the typed subconfig consumed by Offload.
type OffloadConfig struct {
	Engine     string   `json:"engine,omitempty"`
	Interfaces []string `json:"interfaces,omitempty"`
}

func (c *OffloadConfig) Validate() error {
	if _, err := stnrv1.NewOffloadEngine(c.Engine); err != nil {
		return err
	}
	return nil
}
func (c *OffloadConfig) ConfigName() string { return stnrv1.DefaultOffloadName }
func (c *OffloadConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*OffloadConfig)
	if !ok {
		return false
	}
	return c.Engine == o.Engine && reflect.DeepEqual(c.Interfaces, o.Interfaces)
}
func (c *OffloadConfig) DeepCopyInto(dst stnrv1.Config) {
	d, ok := dst.(*OffloadConfig)
	if !ok {
		return
	}
	d.Engine = c.Engine
	d.Interfaces = append([]string(nil), c.Interfaces...)
}
func (c *OffloadConfig) String() string {
	return fmt.Sprintf("OffloadConfig{engine=%s,interfaces=[%s]}",
		c.Engine, strings.Join(c.Interfaces, ","))
}

// NewOffload creates an Offload object.
func NewOffload(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	o := &Offload{rt: rt}
	if conf == nil {
		return o, nil
	}
	req, ok := conf.(*OffloadConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := o.Reconcile(req); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *Offload) Name() string             { return stnrv1.DefaultOffloadName }
func (o *Offload) Type() runtime.ObjectType { return runtime.TypeOffload }

// GetConfig returns a copy of the live offload config. Safe for concurrent use.
func (o *Offload) GetConfig() stnrv1.Config {
	if snap := o.conf.Load(); snap != nil {
		return &OffloadConfig{
			Engine:     snap.Engine,
			Interfaces: append([]string(nil), snap.Interfaces...),
		}
	}
	return &OffloadConfig{Engine: stnrv1.OffloadEngineNone.String()}
}

func (o *Offload) Status() stnrv1.Status {
	conf := o.GetConfig().(*OffloadConfig)
	status := &stnrv1.OffloadStatus{
		Engine:     conf.Engine,
		Interfaces: conf.Interfaces,
		Listeners:  map[string]stnrv1.OffloadDirStat{},
		Clusters:   map[string]stnrv1.OffloadDirStat{},
	}
	stats, err := o.rt.OffloadEngine.Stats()
	if err != nil {
		return status
	}
	listeners := o.nameIndex(runtime.TypeListener)
	clusters := o.nameIndex(runtime.TypeCluster)
	for k, v := range stats {
		info := stnrv1.OffloadStatInfo{Pkts: v.Pkts, Bytes: v.Bytes, TimestampLast: v.TimestampLast}
		dst := status.Clusters
		index := clusters
		if offload.IsListener(k.Flags) {
			dst = status.Listeners
			index = listeners
		}
		name, ok := index[k.NameHash]
		if !ok {
			continue
		}
		ds := dst[name]
		if offload.IsDirIn(k.Flags) {
			ds.Rx = info
		} else {
			ds.Tx = info
		}
		dst[name] = ds
	}
	return status
}

// nameIndex maps offload name-hashes back to object names for the given type.
func (o *Offload) nameIndex(typ runtime.ObjectType) map[uint16]string {
	ret := map[uint16]string{}
	for _, n := range o.rt.Registry.List(typ) {
		ret[offload.NameHash(n.Name())] = n.Name()
	}
	return ret
}

func (o *Offload) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*OffloadConfig)
	if !ok {
		return runtime.ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*OffloadConfig)
	newEngine, err := stnrv1.NewOffloadEngine(req.Engine)
	if err != nil {
		return runtime.ActionNone, err
	}
	curEngine, err := stnrv1.NewOffloadEngine(cur.Engine)
	if err != nil {
		return runtime.ActionNone, err
	}
	// An engine/interfaces change requires the engine to be re-pinned: the reconciler drives a
	// Close()+Start() on the singleton via this object's restart. Unrelated reconciles return
	// ActionNone and never touch the engine.
	if newEngine != curEngine || !reflect.DeepEqual(req.Interfaces, cur.Interfaces) {
		return runtime.ActionRestart, nil
	}
	return runtime.ActionNone, nil
}

func (o *Offload) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*OffloadConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	eng, err := stnrv1.NewOffloadEngine(req.Engine)
	if err != nil {
		return err
	}
	o.conf.Store(&OffloadConfig{
		Engine:     eng.String(),
		Interfaces: append([]string(nil), req.Interfaces...),
	})
	return nil
}

// Start (re-)pins the process-wide offload engine with the current config. The reconciler calls it
// only on object create/restart, i.e. on an actual offload-config change.
func (o *Offload) Start() error {
	conf := o.GetConfig().(*OffloadConfig)
	return o.rt.OffloadEngine.Start(conf.Engine, conf.Interfaces)
}

// Close tears the engine down. On a reconcile-driven restart (shutdown=false) the engine is
// closed so the following Start re-pins it; on process shutdown the engine is closed by
// Stunner.Close, so this is a no-op.
func (o *Offload) Close(shutdown bool) error {
	if shutdown {
		return nil
	}
	return o.rt.OffloadEngine.Close()
}
