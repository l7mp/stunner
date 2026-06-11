// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC.
package stunner

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/pion/logging"
	"github.com/pion/transport/v4"
	"github.com/pion/transport/v4/stdnet"
	"golang.org/x/time/rate"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/reconciler"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// DefaultLogLevel indicates the default log level.
const DefaultLogLevel = "all:WARN"

// LogRateLimit is the default number of log events per second reported at ERROR, WARN and INFO
// loglevel (logging at DEBUG and TRACE levels is not rate-limited).
var LogRateLimit rate.Limit = 50.0

// LogBurst is the default burst size for rate-limited logging at ERROR, WARN and INFO loglevel
// (logging at DEBUG and TRACE levels is not rate-limited).
var LogBurst = 3

// DefaultInstanceId is the default instance id for stunnerd processes.
var DefaultInstanceId = fmt.Sprintf("default/stunnerd-%s", uuid.New().String())

// ErrRestartRequired is returned by Reconcile to indicate that some server components have been
// restarted but otherwise reconciliation occured smoothly. It is safe to ignore this error.
var ErrRestartRequired = object.ErrRestartRequired

// LogOptions configures logging behavior for STUNner.
type LogOptions = logger.Options

// Stunner is an instance of the STUNner daemon.
type Stunner struct {
	// Object-wide config.
	name, version string
	node          string
	udpThreadNum  int

	// Flags.
	forceReady, suppressRollback, dryRun bool

	// Subsystems shared across object factories.
	reconciler *reconciler.Reconciler
	resolver   resolver.DnsResolver
	quotaStore quota.Store
	telemetry  *telemetry.Telemetry
	rt         *runtime.Runtime
	net        transport.Net

	// Logging.
	logRateLimit rate.Limit
	logBurst     int
	log          logging.LeveledLogger
	logger       logger.LoggerFactory
}

// NewStunner creates a new STUNner deamon for the specified Options. Call Reconcile to reconcile
// the daemon for a new configuration. The daemon is "alive" once the main object is initialized,
// and "ready" after the first successful reconciliation.
func NewStunner(options Options) *Stunner {
	var logFactory logger.LoggerFactory
	if options.LogOptions.Format == "json" {
		logFactory = logger.NewJSONLoggerFactory(DefaultLogLevel)
	} else {
		logFactory = logger.NewLoggerFactory(DefaultLogLevel)
	}
	if options.LogOptions.Level != "" {
		logFactory.SetLevel(options.LogOptions.Level)
	}
	log := logFactory.NewLogger("stunner")

	r := options.Resolver
	if r == nil {
		r = resolver.NewDnsResolver("dns-resolver", logFactory)
	}

	var vnet transport.Net
	if options.Net == nil {
		net, err := stdnet.NewNet()
		if err != nil {
			log.Error("could not create vnet")
			return nil
		}
		vnet = net
	} else {
		vnet = options.Net
		log.Warn("virtual net (vnet) is enabled")
	}

	udpThreadNum := 0
	if options.UDPListenerThreadNum > 0 {
		udpThreadNum = options.UDPListenerThreadNum
	}

	logRateLimit := LogRateLimit
	if options.LogOptions.RateLimit > 0 {
		logRateLimit = options.LogOptions.RateLimit
	}

	logBurst := LogBurst
	if options.LogOptions.Burst > 0 {
		logBurst = options.LogOptions.Burst
	}

	id := options.Name
	if id == "" {
		if h, err := os.Hostname(); err != nil {
			id = DefaultInstanceId
		} else {
			id = fmt.Sprintf("default/stunnerd-%s", h)
		}
	}

	s := &Stunner{
		name:             id,
		version:          stnrv1.ApiVersion,
		logger:           logFactory,
		log:              log,
		suppressRollback: options.SuppressRollback,
		dryRun:           options.DryRun,
		resolver:         r,
		quotaStore:       quota.NewStore(),
		udpThreadNum:     udpThreadNum,
		node:             options.NodeName,
		forceReady:       options.ForceReadyDuringTermination,
		net:              vnet,
		logRateLimit:     logRateLimit,
		logBurst:         logBurst,
	}

	// Telemetry needs the live allocation count via callback. We capture s by reference so the
	// callback reads the running value after Listeners exist.
	telemetryCallbacks := telemetry.Callbacks{
		GetAllocationCount: func() int64 { return s.GetActiveConnections() },
	}
	t, err := telemetry.New(telemetryCallbacks, s.dryRun, logFactory.NewLogger("metrics"))
	if err != nil {
		log.Errorf("could not initialize metric provider: %s", err.Error())
		return nil
	}
	s.telemetry = t

	// Build the Runtime: a subsystem registry used throughout the code.
	rt := runtime.New(runtime.Config{
		Logger:       s.logger,
		DryRun:       s.dryRun,
		Resolver:     s.resolver,
		Telemetry:    s.telemetry,
		QuotaStore:   s.quotaStore,
		UdpThreadNum: s.udpThreadNum,
		Net:          s.net,
	})
	rt.SetForceReady(s.forceReady)
	s.rt = rt

	// Catalog describes the object hierarchy.
	catalog := object.NewCatalog()
	rootSpec, ok := catalog.Kind(runtime.TypeStunner)
	if !ok {
		log.Errorf("object catalog missing type: %s", runtime.TypeStunner)
		return nil
	}

	// Register the root Stunner object.
	root, err := rootSpec.New(nil, nil, rt)
	if err != nil {
		log.Errorf("root factory: %s", err.Error())
		return nil
	}
	if err := rt.Registry.Add(root, nil); err != nil {
		log.Errorf("register root: %s", err.Error())
		return nil
	}

	// Create a reconciler.
	s.reconciler = reconciler.New(catalog, rt, s.logger)

	if !s.dryRun {
		s.resolver.Start()
	}

	return s
}

// GetId returns the id of the current stunnerd instance.
func (s *Stunner) GetId() string { return s.name }

// GetVersion returns the STUNner API version.
func (s *Stunner) GetVersion() string { return s.version }

// IsReady returns true if the STUNner instance is ready to serve allocation requests.
func (s *Stunner) IsReady() bool { return s.rt.IsReady() }

// Shutdown causes STUNner to fail the readiness check. Meanwhile, it will keep on serving
// connections. This function should be called after the main program catches a SIGTERM.
func (s *Stunner) Shutdown() {
	s.rt.SetShutdown(true)
}

// GetLogger returns the logger factory of the running daemon.
func (s *Stunner) GetLogger() logging.LoggerFactory { return s.logger }

// SetLogLevel sets the loglevel.
func (s *Stunner) SetLogLevel(levelSpec string) { s.logger.SetLevel(levelSpec) }

// AllocationCount returns the number of active allocations summed over all listeners.
func (s *Stunner) AllocationCount() int {
	n := 0
	for _, l := range s.GetListeners() {
		n += l.AllocationCount()
	}
	return n
}

// GetStatus returns the root status. The root Object aggregates from its descendants.
func (s *Stunner) GetStatus() stnrv1.Status {
	status := s.rt.GetStatus(runtime.TypeStunner, "").(*stnrv1.StunnerStatus)
	status.AllocationCount = s.AllocationCount()
	stat := "READY"
	if !s.rt.IsReady() {
		stat = "NOT-READY"
	}
	if s.rt.IsShutdown() {
		stat = "TERMINATING"
	}
	status.Status = stat
	return status
}

// Close stops the STUNner daemon, cleans up any internal state, and closes all connections.
func (s *Stunner) Close() {
	s.log.Info("closing STUNner")
	if s.reconciler != nil {
		_ = s.reconciler.Shutdown()
	}
	if s.telemetry != nil {
		if err := s.telemetry.Close(); err != nil {
			s.log.Errorf("could not shutdown metric provider cleanly: %s", err.Error())
		}
	}
	if s.resolver != nil {
		s.resolver.Close()
	}
}

// GetActiveConnections returns the number of active downstream (listener-side) TURN allocations.
func (s *Stunner) GetActiveConnections() int64 { return int64(s.AllocationCount()) }

// GetAdmin returns the Admin Object, or nil if not registered.
func (s *Stunner) GetAdmin() *object.Admin {
	a, ok := s.rt.Registry.Get(runtime.TypeAdmin, stnrv1.DefaultAdminName)
	if !ok {
		return nil
	}
	return a.(*object.Admin)
}

// GetAuth returns the Auth Object, or nil if not registered.
func (s *Stunner) GetAuth() *object.Auth {
	a, ok := s.rt.Registry.Get(runtime.TypeAuth, stnrv1.DefaultAuthName)
	if !ok {
		return nil
	}
	return a.(*object.Auth)
}

// GetListeners returns every Listener in stable order.
func (s *Stunner) GetListeners() []*object.Listener {
	objs := s.rt.Registry.List(runtime.TypeListener)
	out := make([]*object.Listener, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*object.Listener))
	}
	return out
}

// GetListener returns a Listener by name, or nil if not found.
func (s *Stunner) GetListener(name string) *object.Listener {
	l, ok := s.rt.Registry.Get(runtime.TypeListener, name)
	if !ok {
		return nil
	}
	return l.(*object.Listener)
}

// GetClusters returns every Cluster in stable order.
func (s *Stunner) GetClusters() []*object.Cluster {
	objs := s.rt.Registry.List(runtime.TypeCluster)
	out := make([]*object.Cluster, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*object.Cluster))
	}
	return out
}

// GetCluster returns a Cluster by name, or nil if not found.
func (s *Stunner) GetCluster(name string) *object.Cluster {
	c, ok := s.rt.Registry.Get(runtime.TypeCluster, name)
	if !ok {
		return nil
	}
	return c.(*object.Cluster)
}
