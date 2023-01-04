// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	"fmt"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/internal/logger"
	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

const DefaultLogLevel = "all:WARN"

// Options defines various options for the STUNner server.
type Options struct {
	// DryRun suppresses sideeffects: STUNner will not initialize listener sockets and bring up
	// the TURN server, and it will not fire up the health-check and the metrics
	// servers. Intended for testing, default is false.
	DryRun bool
	// SuppressRollback controls whether to rollback to the last working configuration after a
	// failed reconciliation request. Default is false, which means to always do a rollback.
	SuppressRollback bool
	// LogLevel specifies the required loglevel for STUNner and each of its sub-objects, e.g.,
	// "all:TRACE" will force maximal loglevel throughout, "all:ERROR,auth:TRACE,turn:DEBUG"
	// will suppress all logs except in the authentication subsystem and the TURN protocol
	// logic.
	LogLevel string
	// Resolver swaps the internal DNS resolver with a custom implementation. Intended for
	// testing.
	Resolver resolver.DnsResolver
	// VNet will switch on testing mode, using a vnet.Net instance to run STUNner over an
	// emulated data-plane.
	Net *vnet.Net
}

// Stunner is an instance of the STUNner deamon.
type Stunner struct {
	version                                                    string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	suppressRollback, dryRun                                   bool
	resolver                                                   resolver.DnsResolver
	logger                                                     *logger.LoggerFactory
	log                                                        logging.LeveledLogger
	net                                                        *vnet.Net
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

	vnet := vnet.NewNet(nil)
	if options.Net != nil {
		log.Warn("vnet is enabled")
		vnet = options.Net
	}

	s := &Stunner{
		version:          v1alpha1.ApiVersion,
		logger:           logger,
		log:              log,
		suppressRollback: options.SuppressRollback,
		dryRun:           options.DryRun,
		resolver:         r,
		net:              vnet,
	}

	s.adminManager = manager.NewManager("admin-manager",
		object.NewAdminFactory(options.DryRun, s.NewReadinessHandler(), logger), logger)
	s.authManager = manager.NewManager("auth-manager",
		object.NewAuthFactory(logger), logger)
	s.listenerManager = manager.NewManager("listener-manager",
		object.NewListenerFactory(vnet, s.NewRealmHandler(), logger), logger)
	s.clusterManager = manager.NewManager("cluster-manager",
		object.NewClusterFactory(r, logger), logger)

	if !s.dryRun {
		s.resolver.Start()
		telemetry.Init()
		// telemetry.RegisterMetrics(s.log, func() float64 { return s.GetAciveConnections() })
	}

	return s
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

// GetAdmin returns the admin object underlying STUNner.
func (s *Stunner) GetAdmin() *object.Admin {
	a, found := s.adminManager.Get(v1alpha1.DefaultAdminName)
	if !found {
		panic("internal error: no Admin found")
	}
	return a.(*object.Admin)
}

// GetAuth returns the authenitation object underlying STUNner.
func (s *Stunner) GetAuth() *object.Auth {
	a, found := s.authManager.Get(v1alpha1.DefaultAuthName)
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

// Status returns a short status description of the running STUNner instance.
func (s *Stunner) Status() string {
	listeners := s.listenerManager.Keys()
	ls := make([]string, len(listeners))
	for i, l := range listeners {
		ls[i] = s.GetListener(l).String()
	}
	str := "NONE"
	if len(ls) > 0 {
		str = strings.Join(ls, ", ")
	}

	status := "READY"
	if !s.ready {
		status = "NOT-READY"
	}
	if s.shutdown {
		status = "TERMINATING"
	}

	auth := s.GetAuth()
	return fmt.Sprintf("status: %s, realm: %s, authentication: %s, listeners: %s",
		status, auth.Realm, auth.Type.String(), str)
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

	// telemetry.UnregisterMetrics(s.log)
	if !s.dryRun {
		telemetry.Close()
	}

	s.resolver.Close()
}

// GetAciveConnections returns the number of active downstream (listener-side) TURN allocations.
func (s *Stunner) GetAciveConnections() float64 { return 0.0 }
