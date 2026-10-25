package server

import (
	"fmt"
	"net"
	"slices"
	"sync"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Base is the server object of the fields every server has, its spec: the name, the type, the
// clusters and the filters. A server of a type embeds it, calls into it for the spec and adds the
// options of its type. Reconcile writes the clusters and the filters, the data path reads them,
// under mu.
type Base struct {
	name string
	typ  stnrv2.ServerType
	rt   *runtime.Runtime

	mu       sync.RWMutex
	clusters []string
	filters  []string
}

var (
	_ api.Server     = &Base{}
	_ runtime.Object = &Base{}
)

// Init sets the name and the type of the server of a validated config, and the runtime its filters
// are looked up in.
func (b *Base) Init(conf *stnrv2.ServerConfig, rt *runtime.Runtime) {
	b.name = conf.Name
	b.typ, _ = stnrv2.NewServerType(conf.Type)
	b.rt = rt
}

func (b *Base) Name() string             { return b.name }
func (b *Base) Type() runtime.ObjectType { return runtime.TypeServer }

// Desired returns the validated server config of an Inspect or a Reconcile.
func (b *Base) Desired(conf stnrv2.Config) (*stnrv2.ServerConfig, error) {
	req, ok := conf.(*stnrv2.ServerConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return req, nil
}

// Inspect restarts on a type change and reconciles any other change in place.
func (b *Base) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, err := b.Desired(new)
	if err != nil {
		return runtime.ActionNone, err
	}
	cur := old.(*stnrv2.ServerConfig)
	switch {
	case cur.Type != req.Type:
		return runtime.ActionRestart, nil
	case cur.DeepEqual(req):
		return runtime.ActionNone, nil
	}
	return runtime.ActionReconcile, nil
}

// Reconcile applies the spec of a config: the clusters change in place, the name and the type
// never do (a type change restarts).
func (b *Base) Reconcile(conf stnrv2.Config) error {
	req, err := b.Desired(conf)
	if err != nil {
		return err
	}
	if req.Name != b.name {
		return fmt.Errorf("cannot rename server %q to %q", b.name, req.Name)
	}
	if req.Type != b.typ.String() {
		return fmt.Errorf("cannot change the type of server %q in place", b.name)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clusters, b.filters = slices.Clone(req.Clusters), slices.Clone(req.Filters)
	return nil
}

// GetConfig returns the config of the spec the server runs with; a server of a type adds the
// options of its type.
func (b *Base) GetConfig() stnrv2.Config {
	b.mu.RLock()
	c := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: b.name, Type: b.typ.String(),
		Clusters: slices.Clone(b.clusters), Filters: slices.Clone(b.filters)}}
	b.mu.RUnlock()
	_ = c.Validate()
	return c
}

func (b *Base) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: b.GetConfig().(*stnrv2.ServerConfig)}
}

// Clusters returns the clusters the server uses, in order; the caller must not modify it.
func (b *Base) Clusters() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.clusters
}

// Admits tells whether the filters of the server admit a peer: a peer no rule matches is refused.
// Port 0 asks for the address of the peer on any port.
func (b *Base) Admits(peer net.Addr) bool {
	b.mu.RLock()
	filters := b.filters
	b.mu.RUnlock()
	allow, matched := b.rt.Match(filters, peer)
	return matched && allow
}

func (b *Base) Start() error     { return nil }
func (b *Base) Close(bool) error { return nil }
func (b *Base) Sessions() int    { return 0 }
func (b *Base) Serve(api.Conn) (api.Conn, api.Verdict, error) {
	return nil, api.Consumed, ErrNotServing
}
