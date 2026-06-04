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

	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/resolver"
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
	name, version string

	// root is the root Object of the live dataplane tree; registry stores every Object.
	root        object.Object
	rootManager *manager.Manager
	registry    object.Registry

	suppressRollback, dryRun bool

	// Subsystems shared across object factories.
	resolver   resolver.DnsResolver
	quotaStore quota.Store

	udpThreadNum                int
	logRateLimit                rate.Limit
	logBurst                    int
	telemetry                   *telemetry.Telemetry
	logger                      logger.LoggerFactory
	log                         logging.LeveledLogger
	node                        string
	net                         transport.Net
	ready, shutdown, forceReady bool
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

	if err := s.buildObjectTree(); err != nil {
		log.Errorf("could not build object tree: %s", err.Error())
		return nil
	}

	if !s.dryRun {
		s.resolver.Start()
	}

	return s
}

// buildObjectTree constructs the persistent Object tree (root + singletons + sub-managers).
// The tree shape is fixed at startup: per-reconcile changes only touch the multi-instance
// Listener/Cluster children.
func (s *Stunner) buildObjectTree() error {
	reg := manager.NewRegistry()
	s.registry = reg

	rateLimitedLog := logger.NewRateLimitedLoggerFactory(s.logger, s.logRateLimit, s.logBurst)
	runtime := &object.Runtime{
		Logger:           s.logger,
		DryRun:           s.dryRun,
		Resolver:         s.resolver,
		Telemetry:        s.telemetry,
		QuotaStore:       s.quotaStore,
		UdpThreadNum:     s.udpThreadNum,
		Net:              s.net,
		ReadinessHandler: s.NewReadinessHandler(),
		StatusHandler:    s.NewStatusHandler(),
	}
	listenerRuntime := *runtime
	listenerRuntime.Logger = rateLimitedLog

	// Root Stunner Object.
	rootFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewStunner(conf, reg, runtime)
	}
	root, err := rootFac(nil, reg)
	if err != nil {
		return fmt.Errorf("root factory: %w", err)
	}
	if err := reg.Add(root); err != nil {
		return fmt.Errorf("register root: %w", err)
	}
	s.root = root

	// Admin singleton.
	adminFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewAdmin(conf, reg, runtime)
	}
	admin, err := adminFac(nil, reg)
	if err != nil {
		return fmt.Errorf("admin factory: %w", err)
	}
	if err := reg.Add(admin); err != nil {
		return fmt.Errorf("register admin: %w", err)
	}

	// Health / Metrics / Offload singletons under Admin.
	healthFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewHealth(conf, reg, runtime)
	}
	health, err := healthFac(nil, reg)
	if err != nil {
		return fmt.Errorf("health factory: %w", err)
	}
	if err := reg.Add(health); err != nil {
		return fmt.Errorf("register health: %w", err)
	}
	metricsFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewMetrics(conf, reg, runtime)
	}
	metrics, err := metricsFac(nil, reg)
	if err != nil {
		return fmt.Errorf("metrics factory: %w", err)
	}
	if err := reg.Add(metrics); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}
	offloadFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewOffload(conf, reg, runtime)
	}
	offload, err := offloadFac(nil, reg)
	if err != nil {
		return fmt.Errorf("offload factory: %w", err)
	}
	if err := reg.Add(offload); err != nil {
		return fmt.Errorf("register offload: %w", err)
	}

	// Auth singleton.
	authFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewAuth(conf, reg, runtime)
	}
	auth, err := authFac(nil, reg)
	if err != nil {
		return fmt.Errorf("auth factory: %w", err)
	}
	if err := reg.Add(auth); err != nil {
		return fmt.Errorf("register auth: %w", err)
	}

	// ListenerList + ClusterList singletons.
	listenerListFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewListenerList(conf, reg, runtime)
	}
	listenerList, err := listenerListFac(nil, reg)
	if err != nil {
		return fmt.Errorf("listener-list factory: %w", err)
	}
	if err := reg.Add(listenerList); err != nil {
		return fmt.Errorf("register listener-list: %w", err)
	}
	clusterListFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewClusterList(conf, reg, runtime)
	}
	clusterList, err := clusterListFac(nil, reg)
	if err != nil {
		return fmt.Errorf("cluster-list factory: %w", err)
	}
	if err := reg.Add(clusterList); err != nil {
		return fmt.Errorf("register cluster-list: %w", err)
	}

	// Per-kind managers. Multi-instance managers (listener, cluster) use list extractors that
	// produce one entry per slice element; singleton managers produce a single-element list.
	rootMgr := manager.NewManager("stunner-mgr", object.TypeStunner, rootFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config { return c }),
		reg, s.logger, manager.WithSingleton(stnrv1.DefaultStunnerName))
	adminMgr := manager.NewManager("admin-mgr", object.TypeAdmin, adminFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config { return &c.Admin }),
		reg, s.logger, manager.WithSingleton(stnrv1.DefaultAdminName))
	authMgr := manager.NewManager("auth-mgr", object.TypeAuth, authFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config { return &c.Auth }),
		reg, s.logger, manager.WithSingleton(stnrv1.DefaultAuthName))
	listenerListMgr := manager.NewManager("listener-list-mgr", object.TypeListenerList, listenerListFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config {
			return &object.ListenerListConfig{Listeners: append([]stnrv1.ListenerConfig(nil), c.Listeners...)}
		}),
		reg, s.logger, manager.WithSingleton(object.DefaultListenerListName))
	clusterListMgr := manager.NewManager("cluster-list-mgr", object.TypeClusterList, clusterListFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config {
			return &object.ClusterListConfig{Clusters: append([]stnrv1.ClusterConfig(nil), c.Clusters...)}
		}),
		reg, s.logger, manager.WithSingleton(object.DefaultClusterListName))

	// Sub-managers under Admin (singletons each).
	healthMgr := manager.NewManager("health-mgr", object.TypeHealth, healthFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config {
			endpoint := defaultHealthEndpoint()
			if c.Admin.HealthCheckEndpoint != nil {
				endpoint = *c.Admin.HealthCheckEndpoint
			}
			return &object.HealthConfig{Endpoint: endpoint}
		}),
		reg, s.logger, manager.WithSingleton(object.DefaultHealthName))
	metricsMgr := manager.NewManager("metrics-mgr", object.TypeMetrics, metricsFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config {
			return &object.MetricsConfig{Endpoint: c.Admin.MetricsEndpoint}
		}),
		reg, s.logger, manager.WithSingleton(object.DefaultMetricsName))
	offloadMgr := manager.NewManager("offload-mgr", object.TypeOffload, offloadFac,
		singletonExtractor(func(c *stnrv1.StunnerConfig) stnrv1.Config {
			return &object.OffloadConfig{
				Engine:     c.Admin.OffloadEngine,
				Interfaces: append([]string(nil), c.Admin.OffloadInterfaces...),
			}
		}),
		reg, s.logger, manager.WithSingleton(object.DefaultOffloadName))

	// Sub-managers under ListenerList / ClusterList: multi-instance.
	listenerFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewListener(conf, reg, &listenerRuntime)
	}
	listenerMgr := manager.NewManager("listener-mgr", object.TypeListener, listenerFac,
		func(c *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return extractListenerConfigsForFull(c), nil
		},
		reg, s.logger)

	clusterFac := func(conf stnrv1.Config, reg object.Registry) (object.Object, error) {
		return object.NewCluster(conf, reg, runtime)
	}
	clusterMgr := manager.NewManager("cluster-mgr", object.TypeCluster, clusterFac,
		func(c *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			out := make([]stnrv1.Config, len(c.Clusters))
			for i := range c.Clusters {
				cc := c.Clusters[i]
				out[i] = &cc
			}
			return out, nil
		},
		reg, s.logger)

	// Wire sub-managers onto their parent Nodes.
	if err := reg.AttachSubManager(root, adminMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(root, authMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(root, listenerListMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(root, clusterListMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(admin, healthMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(admin, metricsMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(admin, offloadMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(listenerList, listenerMgr); err != nil {
		return err
	}
	if err := reg.AttachSubManager(clusterList, clusterMgr); err != nil {
		return err
	}

	s.rootManager = rootMgr
	return nil
}

// singletonExtractor turns a single-config extractor into a ListExtractor (returning a
// one-element slice).
func singletonExtractor(f func(*stnrv1.StunnerConfig) stnrv1.Config) manager.ListExtractor {
	return func(c *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
		return []stnrv1.Config{f(c)}, nil
	}
}

// defaultHealthEndpoint mirrors the historical default for AdminConfig.HealthCheckEndpoint==nil.
func defaultHealthEndpoint() string {
	return fmt.Sprintf("http://:%d", stnrv1.DefaultHealthCheckPort)
}

// extractListenerConfigsForFull builds the per-listener configs used by the listener-manager's
// list extractor.
func extractListenerConfigsForFull(full *stnrv1.StunnerConfig) []stnrv1.Config {
	out := make([]stnrv1.Config, len(full.Listeners))
	for i := range full.Listeners {
		lc := full.Listeners[i]
		out[i] = &lc
	}
	return out
}

// GetId returns the id of the current stunnerd instance.
func (s *Stunner) GetId() string { return s.name }

// GetVersion returns the STUNner API version.
func (s *Stunner) GetVersion() string { return s.version }

// IsReady returns true if the STUNner instance is ready to serve allocation requests.
func (s *Stunner) IsReady() bool { return s.ready }

// Shutdown causes STUNner to fail the readiness check. Meanwhile, it will keep on serving
// connections. This function should be called after the main program catches a SIGTERM.
func (s *Stunner) Shutdown() {
	s.shutdown = true
	s.ready = false
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

// Status returns the root status. The root Object aggregates from its descendants.
func (s *Stunner) Status() stnrv1.Status {
	status := s.root.Status().(*stnrv1.StunnerStatus)
	status.AllocationCount = s.AllocationCount()
	stat := "READY"
	if !s.ready {
		stat = "NOT-READY"
	}
	if s.shutdown {
		stat = "TERMINATING"
	}
	status.Status = stat
	return status
}

// Close stops the STUNner daemon, cleans up any internal state, and closes all connections.
func (s *Stunner) Close() {
	s.log.Info("closing STUNner")
	if s.rootManager != nil {
		_ = s.rootManager.Shutdown()
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
	a, ok := s.registry.LookupOne(object.TypeAdmin)
	if !ok {
		return nil
	}
	return a.(*object.Admin)
}

// GetAuth returns the Auth Object, or nil if not registered.
func (s *Stunner) GetAuth() *object.Auth {
	a, ok := s.registry.LookupOne(object.TypeAuth)
	if !ok {
		return nil
	}
	return a.(*object.Auth)
}

// GetListeners returns every Listener in stable order.
func (s *Stunner) GetListeners() []*object.Listener {
	objs := s.registry.LookupAll(object.TypeListener)
	out := make([]*object.Listener, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*object.Listener))
	}
	return out
}

// GetListener returns a Listener by name, or nil if not found.
func (s *Stunner) GetListener(name string) *object.Listener {
	l, ok := s.registry.Lookup(object.TypeListener, name)
	if !ok {
		return nil
	}
	return l.(*object.Listener)
}

// GetClusters returns every Cluster in stable order.
func (s *Stunner) GetClusters() []*object.Cluster {
	objs := s.registry.LookupAll(object.TypeCluster)
	out := make([]*object.Cluster, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*object.Cluster))
	}
	return out
}

// GetCluster returns a Cluster by name, or nil if not found.
func (s *Stunner) GetCluster(name string) *object.Cluster {
	c, ok := s.registry.Lookup(object.TypeCluster, name)
	if !ok {
		return nil
	}
	return c.(*object.Cluster)
}

// NewReadinessHandler / NewStatusHandler are defined in handlers.go.
