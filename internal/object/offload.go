package object

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"

	objectturn "github.com/l7mp/stunner/internal/object/turn"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Offload wraps the TURN offload handler in its own Object so reconciliations don't accidentally
// bounce the eBPF engine.
//
// INVARIANT (intentional, do not "fix"): Close(false) is a NO-OP and Start is idempotent. The
// engine only goes down on process shutdown (Close(true)). Even when Inspect signals a restart
// for an engine/interfaces change, the reconcile-driven Close(false) keeps the engine running:
// swapping engine internals on a config change is the offload handler's concern, not the object
// lifecycle's. The payoff of this split: changes to Health or Metrics endpoints never drag
// Offload through close+start.
type Offload struct {
	engine     stnrv1.OffloadMode
	interfaces []string
	handler    objectturn.OffloadHandler
	started    bool

	// conf is the atomic snapshot read via Admin.GetConfig on the allocation path.
	conf atomic.Pointer[OffloadConfig]

	log logging.LeveledLogger
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
	o := &Offload{
		engine:  stnrv1.OffloadEngineNone,
		handler: objectturn.NewOffloadHandlerStub(),
		log:     rt.Logger.NewLogger("offload"),
	}
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
	if o.handler == nil {
		return status
	}
	runtimeStatus := o.handler.Status()
	for k, v := range runtimeStatus.Listeners {
		status.Listeners[k] = v
	}
	for k, v := range runtimeStatus.Clusters {
		status.Clusters[k] = v
	}
	return status
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
	changed := newEngine != curEngine || !reflect.DeepEqual(req.Interfaces, cur.Interfaces)
	// Only an actual engine/interfaces change requires the engine to be torn down.
	if changed {
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
	o.engine = eng
	o.interfaces = append([]string(nil), req.Interfaces...)
	o.conf.Store(&OffloadConfig{
		Engine:     eng.String(),
		Interfaces: append([]string(nil), req.Interfaces...),
	})
	return nil
}

func (o *Offload) Start() error {
	if o.started {
		return nil
	}
	if o.handler == nil {
		return nil
	}
	if err := o.handler.Start(); err != nil {
		return err
	}
	o.started = true
	return nil
}

// Close stops the object. Offload objects are special: a reconcile-driven Close(false) MUST
// NOT tear the engine down, only an explicit shutdown (Close(true)) really closes it. See the
// type-level invariant comment.
func (o *Offload) Close(shutdown bool) error {
	if !shutdown {
		return nil
	}
	if o.handler == nil {
		return nil
	}
	err := o.handler.Close()
	o.started = false
	return err
}

// Handler exposes the underlying offload handler. Used by the TURN server to wire up
// per-allocation channel offload events.
func (o *Offload) Handler() objectturn.OffloadHandler { return o.handler }
