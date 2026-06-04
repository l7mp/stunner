package object

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Offload wraps the TURN offload handler in its own Object so reconciliations don't accidentally
// bounce the eBPF engine. The key invariant: Close(false) is a NO-OP. The engine only goes down on
// process shutdown (Close(true)) or on an explicit Engine/Interfaces change (Inspect signals
// restart, the Reconciler calls Close(false) anyway — that's why the no-op must be defended by
// Inspect rather than by Close itself for the engine-change case; see Close below).
//
// The "centerpiece of the whole refactor": with this split, changes to Health or Metrics endpoints
// no longer drag Offload through close+start.
type Offload struct {
	engine     stnrv1.OffloadMode
	interfaces []string
	handler    TURNOffloadHandler
	started    bool
	reg        Registry
	log        logging.LeveledLogger
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
func (c *OffloadConfig) ConfigName() string { return DefaultOffloadName }
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

// OffloadHandlerCtor builds the underlying offload handler. In the OSS build this defaults to a
// stub; downstream non-OSS builds inject a real eBPF-backed handler.
type OffloadHandlerCtor = func() TURNOffloadHandler

// NewOffload creates an Offload object.
func NewOffload(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	ctor := rt.OffloadHandler
	if ctor == nil {
		ctor = func() TURNOffloadHandler { return &offloadHandlerStub{} }
	}
	o := &Offload{
		engine:  stnrv1.OffloadEngineNone,
		handler: ctor(),
		reg:     reg,
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

func (o *Offload) ObjectName() string { return DefaultOffloadName }
func (o *Offload) ObjectType() string { return TypeOffload }

func (o *Offload) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	return &OffloadConfig{
		Engine:     c.Admin.OffloadEngine,
		Interfaces: append([]string(nil), c.Admin.OffloadInterfaces...),
	}, nil
}

func (o *Offload) GetConfig() stnrv1.Config {
	return &OffloadConfig{
		Engine:     o.engine.String(),
		Interfaces: append([]string(nil), o.interfaces...),
	}
}
func (o *Offload) Status() stnrv1.Status { return o.GetConfig() }

func (o *Offload) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	req, ok := new.(*OffloadConfig)
	if !ok {
		return ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*OffloadConfig)
	newEngine, err := stnrv1.NewOffloadEngine(req.Engine)
	if err != nil {
		return ActionNone, err
	}
	curEngine, err := stnrv1.NewOffloadEngine(cur.Engine)
	if err != nil {
		return ActionNone, err
	}
	changed := newEngine != curEngine || !reflect.DeepEqual(req.Interfaces, cur.Interfaces)
	// Only an actual engine/interfaces change requires the engine to be torn down.
	if changed {
		return ActionRestart, nil
	}
	return ActionNone, nil
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

// Close is the whole point of the refactor: a reconcile-driven Close(false) MUST NOT tear the
// engine down. Only an explicit shutdown closes it. Note that when an engine/interfaces change
// actually requires a restart, the Reconciler will call Close(false) → no-op, then Reconcile (which
// updates the in-memory config), then Start (idempotent — already started). If the underlying
// handler needs to swap interfaces/engine internally on Reconcile, that's the handler's concern;
// from the Object's perspective we keep the engine running across reconfigs.
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
func (o *Offload) Handler() TURNOffloadHandler { return o.handler }
