package object

import (
	"fmt"
	"net/netip"
	"sync/atomic"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/filter"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Filter is a filter config and its compiled rules, which every change replaces in place.
type Filter struct {
	name   string
	conf   atomic.Pointer[stnrv2.FilterConfig]
	filter atomic.Pointer[filter.Filter]
}

var _ api.Filter = &Filter{}

// NewFilter creates a filter object.
func NewFilter(conf stnrv2.Config) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.FilterConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	f := &Filter{name: req.Name}
	if err := f.Reconcile(req); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Filter) Name() string             { return f.name }
func (f *Filter) Type() runtime.ObjectType { return runtime.TypeFilter }

// Inspect reconciles every change in place.
func (f *Filter) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.FilterConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return runtime.ActionNone, err
	}
	if old.(*stnrv2.FilterConfig).DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	return runtime.ActionReconcile, nil
}

// Reconcile compiles the rules; the next check uses them.
func (f *Filter) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.FilterConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if f.name != req.Name {
		return fmt.Errorf("cannot rename filter %q to %q", f.name, req.Name)
	}
	cp := &stnrv2.FilterConfig{}
	req.DeepCopyInto(cp)
	f.conf.Store(cp)
	f.filter.Store(filter.New(cp))
	return nil
}

func (f *Filter) GetConfig() stnrv2.Config {
	cp := &stnrv2.FilterConfig{Name: f.name}
	if c := f.conf.Load(); c != nil {
		c.DeepCopyInto(cp)
	}
	return cp
}

func (f *Filter) Start() error     { return nil }
func (f *Filter) Close(bool) error { return nil }

func (f *Filter) Status() stnrv2.Status {
	return &stnrv2.FilterStatus{FilterConfig: f.GetConfig().(*stnrv2.FilterConfig)}
}

func (f *Filter) Match(a netip.AddrPort) (allow, matched bool) { return f.filter.Load().Match(a) }
