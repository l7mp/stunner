// Package runtime is the kernel of the STUNner object system: it defines the node contracts
// (Runnable, Reconcilable, Object), the Registry that stores every live node keyed by (type,
// name) with parent edges, and the Runtime, the single cross-object access point carrying
// process-wide dependencies, registry-backed config/status lookups, relay routing, and the
// readiness/shutdown flags.
package runtime

import (
	"sync/atomic"

	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/offload"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
	"github.com/l7mp/stunner/pkg/logger"
)

// Config carries the process-wide dependencies shared by all objects. Set once at startup. The
// service interfaces (QuotaHandler, Router) are declared in api.go; their implementations live in
// internal/quota and internal/router. OffloadEngine is a process-wide singleton owned by the
// Runtime (its eBPF lifetime is the server's lifetime); internal/offload is a one-way dependency.
type Config struct {
	Logger        logger.LoggerFactory
	DryRun        bool
	Resolver      resolver.DnsResolver
	Telemetry     *telemetry.Telemetry
	QuotaHandler  QuotaHandler
	License       licensecfg.ConfigManager
	OffloadEngine offload.Engine
	UdpThreadNum  int
	Net           transport.Net
}

// Runtime is the single cross-object access point: process-wide dependencies, the object
// Registry, relay routing, and the process readiness/shutdown flags.
type Runtime struct {
	Config

	// Registry stores every live node in the dataplane.
	Registry *Registry
	// Router resolves cluster relays for the TURN packet path with LRU caching.
	Router Router

	ready      atomic.Bool
	shutdown   atomic.Bool
	forceReady atomic.Bool
}

// New creates a Runtime with an empty Registry. The caller wires rt.Router via
// router.NewRouter(rt) (kept out of the kernel to avoid importing the router implementation).
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
	return &Runtime{Config: deps, Registry: NewRegistry()}
}

// IsReady returns true if STUNner is ready to serve requests.
func (rt *Runtime) IsReady() bool {
	return rt.ready.Load()
}

// ReadyForProbes returns true if readiness probes should report ready.
func (rt *Runtime) ReadyForProbes() bool {
	return rt.forceReady.Load() || rt.ready.Load()
}

// IsShutdown returns true if STUNner is in shutdown mode.
func (rt *Runtime) IsShutdown() bool {
	return rt.shutdown.Load()
}

// SetReady updates the runtime readiness flag.
func (rt *Runtime) SetReady(v bool) {
	rt.ready.Store(v)
}

// SetShutdown updates shutdown mode and clears readiness on shutdown.
func (rt *Runtime) SetShutdown(v bool) {
	rt.shutdown.Store(v)
	if v {
		rt.ready.Store(false)
	}
}

// SetForceReady configures force-ready readiness probe behavior.
func (rt *Runtime) SetForceReady(v bool) {
	rt.forceReady.Store(v)
}
