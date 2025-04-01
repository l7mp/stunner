// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/pion/logging"
	"github.com/pion/transport/v3"
	"github.com/pion/transport/v3/stdnet"

	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
	"github.com/l7mp/stunner/pkg/logger"
)

const DefaultLogLevel = "all:WARN"

var DefaultInstanceId = fmt.Sprintf("default/stunnerd-%s", uuid.New().String())

// Stunner is an instance of the STUNner deamon.
type Stunner struct {
	name, version                                              string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	suppressRollback, dryRun                                   bool
	resolver                                                   resolver.DnsResolver
	udpThreadNum                                               int
	telemetry                                                  *telemetry.Telemetry
	logger                                                     logger.LoggerFactory
	log                                                        logging.LeveledLogger
	quotaHandler                                               QuotaHandler
	offloadHandler                                             OffloadHandler
	node                                                       string
	net                                                        transport.Net
	ready, shutdown                                            bool
}

// NewStunner creates a new STUNner deamon for the specified Options. Call Reconcile to reconcile
// the daemon for a new configuration. Object lifecycle is as follows: the daemon is "alive"
// (answers liveness probes if healthchecking is enabled) once the main object is successfully
// initialized, and "ready" after the first successful reconciliation (answers readiness probes if
// healthchecking is enabled). Calling program should catch SIGTERM signals and call Shutdown(),
// which will keep on serving connections but will fail readiness probes.
func NewStunner(options Options) *Stunner {
	logger := logger.NewLoggerFactory(DefaultLogLevel)
	if options.LogLevel != "" {
		logger.SetLevel(options.LogLevel)
	}
	log := logger.NewLogger("stunner")

	r := options.Resolver
	if r == nil {
		r = resolver.NewDnsResolver("dns-resolver", logger)
	}

	var vnet transport.Net
	if options.Net == nil {
		net, err := stdnet.NewNet() // defaults to native operation
		if err != nil {
			log.Error("Could not create vnet")
			return nil
		}
		vnet = net
	} else {
		vnet = options.Net
		log.Warn("Virtual net (vnet) is enabled")
	}

	udpThreadNum := 0
	if options.UDPListenerThreadNum > 0 {
		udpThreadNum = options.UDPListenerThreadNum
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
		logger:           logger,
		log:              log,
		suppressRollback: options.SuppressRollback,
		dryRun:           options.DryRun,
		resolver:         r,
		udpThreadNum:     udpThreadNum,
		node:             options.NodeName,
		net:              vnet,
	}

	s.offloadHandler = s.NewOffloadHandler()
	statsHandler := func(name string, marker stnrv1.StatType) stnrv1.OffloadDirStat {
		return s.offloadHandler.Stats(name, marker)
	}

	s.adminManager = manager.NewManager("admin-manager",
		object.NewAdminFactory(options.DryRun, s.NewReadinessHandler(), s.NewStatusHandler(), logger), logger)
	s.authManager = manager.NewManager("auth-manager",
		object.NewAuthFactory(logger), logger)
	s.listenerManager = manager.NewManager("listener-manager",
		object.NewListenerFactory(vnet, s.NewRealmHandler(), statsHandler, logger), logger)
	s.clusterManager = manager.NewManager("cluster-manager",
		object.NewClusterFactory(r, statsHandler, logger), logger)
	s.quotaHandler = s.NewQuotaHandler()

	telemetryCallbacks := telemetry.Callbacks{
		GetAllocationCount: func() int64 { return s.GetActiveConnections() },
	}
	t, err := telemetry.New(telemetryCallbacks, s.dryRun, logger.NewLogger("metrics"))
	if err != nil {
		log.Errorf("Could not initialize metric provider: %s", err.Error())
		return nil
	}
	s.telemetry = t

	if !s.dryRun {
		s.resolver.Start()
	}

	s.ready = true

	return s
}

// GetId returns the id of the current stunnerd instance.
func (s *Stunner) GetId() string {
	return s.name
}

// GetVersion returns the STUNner API version.
func (s *Stunner) GetVersion() string {
	return s.version
}

// IsReady returns true if the STUNner instance is ready to serve allocation requests.
func (s *Stunner) IsReady() bool {
	return s.ready
}

// Shutdown causes STUNner to fail the readiness check. Manwhile, it will keep on serving
// connections. This function should be called after the main program catches a SIGTERM.
func (s *Stunner) Shutdown() {
	s.shutdown = true
	s.ready = false
}

// GetAdmin returns the admin object. Panics if no admin object is available.
func (s *Stunner) GetAdmin() *object.Admin {
	a, found := s.adminManager.Get(stnrv1.DefaultAdminName)
	if !found {
		panic("internal error: no Admin found")
	}
	return a.(*object.Admin)
}

// GetLicenseConfigManager returns the manager handling license status. Panics if no manager is
// available.
func (s *Stunner) GetLicenseConfigManager() licensecfg.ConfigManager {
	return s.GetAdmin().LicenseManager
}

// GetAuth returns the authenitation object. Panics if no auth object is available.
func (s *Stunner) GetAuth() *object.Auth {
	a, found := s.authManager.Get(stnrv1.DefaultAuthName)
	if !found {
		panic("internal error: no Auth found")
	}
	return a.(*object.Auth)
}

// GetListener returns a STUNner listener or nil of no listener with the given name was found.
func (s *Stunner) GetListener(name string) *object.Listener {
	l, found := s.listenerManager.Get(name)
	if !found {
		return nil
	}
	return l.(*object.Listener)
}

// GetCluster returns a STUNner cluster or nil if no cluster with the given name was found.
func (s *Stunner) GetCluster(name string) *object.Cluster {
	l, found := s.clusterManager.Get(name)
	if !found {
		return nil
	}
	return l.(*object.Cluster)
}

// GetRealm returns the current STUN/TURN authentication realm.
func (s *Stunner) GetRealm() string {
	auth := s.GetAuth()
	if auth == nil {
		return ""
	}
	return auth.Realm
}

// GetLogger returns the logger factory of the running daemon. Useful for creating a sub-logger.
func (s *Stunner) GetLogger() logging.LoggerFactory {
	return s.logger
}

// SetLogLevel sets the loglevel.
func (s *Stunner) SetLogLevel(levelSpec string) {
	s.logger.SetLevel(levelSpec)
}

// GetAllocations returns the number of active allocations summed over all listeners.  It can be
// used to drain the server before closing.
func (s *Stunner) AllocationCount() int {
	n := 0
	listeners := s.listenerManager.Keys()
	for _, name := range listeners {
		l := s.GetListener(name)
		if l.Server != nil {
			n += l.Server.AllocationCount()
		}
	}
	return n
}

// Status returns the status for the running STUNner instance.
func (s *Stunner) Status() stnrv1.Status {
	status := stnrv1.StunnerStatus{ApiVersion: s.version}
	if admin := s.GetAdmin(); admin != nil {
		status.Admin = admin.Status().(*stnrv1.AdminStatus)
	}
	if auth := s.GetAuth(); auth != nil {
		status.Auth = auth.Status().(*stnrv1.AuthStatus)
	}

	ls := s.listenerManager.Keys()
	status.Listeners = make([]*stnrv1.ListenerStatus, len(ls))
	for i, lName := range ls {
		if l := s.GetListener(lName); l != nil {
			status.Listeners[i] = l.Status().(*stnrv1.ListenerStatus)
		}
	}

	cs := s.clusterManager.Keys()
	status.Clusters = make([]*stnrv1.ClusterStatus, len(cs))
	for i, cName := range cs {
		if c := s.GetCluster(cName); c != nil {
			status.Clusters[i] = c.Status().(*stnrv1.ClusterStatus)
		}
	}

	status.AllocationCount = s.AllocationCount()
	stat := "READY"
	if !s.ready {
		stat = "NOT-READY"
	}
	if s.shutdown {
		stat = "TERMINATING"
	}
	status.Status = stat

	return &status
}

// Close stops the STUNner daemon, cleans up any internal state, and closes all connections
// including the health-check and the metrics server listeners.
func (s *Stunner) Close() {
	s.log.Info("closing STUNner")

	// ignore restart-required errors
	if len(s.adminManager.Keys()) > 0 {
		_ = s.GetAdmin().Close()
	}

	if len(s.authManager.Keys()) > 0 {
		_ = s.GetAuth().Close()
	}

	listeners := s.listenerManager.Keys()
	for _, name := range listeners {
		l := s.GetListener(name)
		if err := l.Close(); err != nil && err != object.ErrRestartRequired {
			s.log.Errorf("Error closing listener %q at adddress %s: %s",
				l.Proto.String(), l.Addr, err.Error())
		}
	}

	clusters := s.clusterManager.Keys()
	for _, name := range clusters {
		c := s.GetCluster(name)
		if err := c.Close(); err != nil && err != object.ErrRestartRequired {
			s.log.Errorf("Error closing cluster %q: %s", c.ObjectName(),
				err.Error())
		}
	}

	if err := s.offloadHandler.Close(); err != nil {
		s.log.Errorf("Could not shutdown offload handler cleanly: %s", err.Error())
	}

	if err := s.telemetry.Close(); err != nil { // blocks until finished
		s.log.Errorf("Could not shutdown metric provider cleanly: %s", err.Error())
	}

	s.resolver.Close()
}

// GetActiveConnections returns the number of active downstream (listener-side) TURN allocations.
func (s *Stunner) GetActiveConnections() int64 {
	count := s.AllocationCount()
	return int64(count)
}
