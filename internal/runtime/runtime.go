// Package runtime is the kernel of the STUNner object system: it defines the node contracts
// (Runnable, Reconcilable, Object), the Registry that stores every live node keyed by (type,
// name) with parent edges, and the Runtime, the single cross-object access point carrying
// process-wide dependencies, registry-backed config/status lookups, relay routing, and the
// readiness/shutdown flags.
package runtime

import (
	"sync/atomic"

	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

// Config carries the process-wide dependencies shared by all objects. Set once at startup.
type Config struct {
	Logger       logger.LoggerFactory
	DryRun       bool
	Resolver     resolver.DnsResolver
	Telemetry    *telemetry.Telemetry
	QuotaStore   quota.Store
	UdpThreadNum int
	Net          transport.Net
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

// New creates a Runtime with an empty Registry and a default Router.
func New(deps Config) *Runtime {
	rt := &Runtime{Config: deps, Registry: NewRegistry()}
	rt.Router = NewRouter(rt)
	return rt
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
