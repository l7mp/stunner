// Package runtime is the kernel of the STUNner object system: it defines the node contracts
// (Runnable, Reconcilable, Object), the Registry that stores every live node keyed by (type,
// name) with parent edges, and the Runtime, the single cross-object access point carrying
// process-wide dependencies, registry-backed config/status lookups, and the
// readiness/shutdown flags.
package runtime

import (
	"sync/atomic"

	"github.com/l7mp/stunner/v2/pkg/buildinfo"

	"github.com/pion/transport/v5"

	"github.com/l7mp/stunner/v2/internal/offload"
	"github.com/l7mp/stunner/v2/internal/proto/fifo"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	licensecfg "github.com/l7mp/stunner/v2/pkg/config/license"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// Config carries the process-wide dependencies shared by all objects. Set once at startup. The
// service interface QuotaHandler is declared in api.go; its implementation lives in
// internal/quota. OffloadEngine is a process-wide singleton owned by the
// Runtime (its eBPF lifetime is the server's lifetime); internal/offload is a one-way dependency.
type Config struct {
	Logger        logger.LoggerFactory
	DryRun        bool
	Resolver      resolver.DnsResolver
	Telemetry     *telemetry.Telemetry
	QuotaHandler  QuotaHandler
	License       licensecfg.ConfigManager
	OffloadEngine offload.Engine
	Net           transport.Net
	// Fifos are the in-process fifos the FIFO listeners bind and the FIFO clusters dial.
	Fifos *fifo.Fifos
	// BuildInfo is the version of the binary.
	BuildInfo buildinfo.BuildInfo
}

// Runtime is the single cross-object access point: process-wide dependencies, the object
// Registry, and the process readiness/shutdown flags.
type Runtime struct {
	Config

	// Registry stores every live node in the dataplane.
	Registry *Registry

	ready    atomic.Bool
	shutdown atomic.Bool
}

// New creates a Runtime with an empty Registry.
func New(deps Config) *Runtime {
	if deps.License == nil {
		deps.License = licensecfg.New(deps.Logger.NewLogger("license"))
	}
	if deps.OffloadEngine == nil {
		deps.OffloadEngine = offload.New(offload.Deps{
			Telemetry: deps.Telemetry,
			License:   deps.License,
			Log:       deps.Logger.NewLogger("offload"),
		})
	}
	if deps.Fifos == nil {
		deps.Fifos = fifo.New()
	}
	return &Runtime{Config: deps, Registry: NewRegistry()}
}

// IsReady returns true from the first config until the daemon closes, through the graceful
// shutdown; it backs the readiness probe.
func (rt *Runtime) IsReady() bool {
	return rt.ready.Load()
}

// IsShutdown returns true if STUNner is in shutdown mode.
func (rt *Runtime) IsShutdown() bool {
	return rt.shutdown.Load()
}

// SetReady updates the runtime readiness flag.
func (rt *Runtime) SetReady(v bool) {
	rt.ready.Store(v)
}

// SetShutdown updates shutdown mode. The graceful shutdown keeps the readiness on purpose: kube-proxy
// deletes the UDP conntrack entries of an endpoint that stops serving, which would cut the live UDP
// sessions, while a terminating pod gets no new traffic anyway. See the readiness probe in
// internal/object/health.go for the full story before changing this.
func (rt *Runtime) SetShutdown(v bool) {
	rt.shutdown.Store(v)
}
