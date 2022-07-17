// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	"fmt"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/logger"
	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/monitoring"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

const DefaultLogLevel = "all:WARN"

// Options defines various options to define the way STUNner will run
type Options struct {
	// DryRun will suppress sideeffects: it will not initialize listener sockets and it will
	// not bring up the TURN server. This is mostly for testing, default is false
	DryRun bool
	// SuppressRollback controls whether to rollback the last working configuration after a
	// failed reconciliation request. Default is false, which means to always rollback
	SuppressRollback bool
	// LogLevel specifies the required loglevel for STUNner and each of its sub-objects, e.g.,
	// "all:TRACE" will force maximal loglevel throughout the daemon will
	// "all:ERROR,auth:TRACE,turn:DEBUG" will suppress all logs except in the authentication
	// subsystem and the TURN protocol logic
	LogLevel string
	// Resolver swaps the internal DNS resolver with a custom implementation (used mostly for
	// testing)
	Resolver resolver.DnsResolver
	// MonitoringBackend serves Prometheus metrics data.
	MonitoringBackend *monitoring.Backend
	// VNet will switch STUNner into testing mode, using a vnet.Net instance to run STUNner
	// over an emulated data-plane
	Net *vnet.Net
}

// Stunner is an instance of the STUNner deamon
type Stunner struct {
	version                                                    string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	resolver                                                   resolver.DnsResolver
	logger                                                     *logger.LoggerFactory
	log                                                        logging.LeveledLogger
	server                                                     *turn.Server
	monitoringBackend                                          *monitoring.Backend
	net                                                        *vnet.Net
	options                                                    Options
}

// NewStunner creates a new empty STUNner deamon. Call Reconcile to reconcile the daemon for the given configuration
func NewStunner() *Stunner {
	loggerFactory := logger.NewLoggerFactory(DefaultLogLevel)
	r := resolver.NewDnsResolver("dns-resolver", loggerFactory)
	vnet := vnet.NewNet(nil)

	s := Stunner{
		version: v1alpha1.ApiVersion,
		logger:  loggerFactory,
		log:     loggerFactory.NewLogger("stunner"),
		adminManager: manager.NewManager("admin-manager",
			object.NewAdminFactory(loggerFactory), loggerFactory),
		authManager: manager.NewManager("auth-manager",
			object.NewAuthFactory(loggerFactory), loggerFactory),
		listenerManager: manager.NewManager("listener-manager",
			object.NewListenerFactory(vnet, loggerFactory), loggerFactory),
		clusterManager: manager.NewManager("cluster-manager",
			object.NewClusterFactory(r, loggerFactory), loggerFactory),
		resolver:          r,
		monitoringBackend: nil,
		net:               vnet,
		options:           Options{},
	}

	// register metrics
	monitoring.RegisterMetrics(s.log, func() float64 { return float64(s.GetServer().AllocationCount()) })

	return &s
}

// WithOptions will take into effect the options passed in
func (s *Stunner) WithOptions(options Options) *Stunner {
	s.options = options

	if options.LogLevel != "" {
		s.logger.SetLevel(options.LogLevel)
	}

	if options.Net != nil {
		s.log.Warn("vnet is enabled")
		s.net = options.Net
		s.listenerManager = manager.NewManager("listener-manager",
			object.NewListenerFactory(options.Net, s.logger), s.logger)
	}

	if options.Resolver != nil {
		s.resolver = options.Resolver
		s.clusterManager = manager.NewManager("cluster-manager",
			object.NewClusterFactory(options.Resolver, s.logger), s.logger)
	}

	if options.MonitoringBackend != nil {
		s.monitoringBackend = options.MonitoringBackend
		// add monitoring server to AdminFactory and manage it
	}

	return s
}

// GetVersion returns the STUNner API version
func (s *Stunner) GetVersion() string {
	return s.version
}

// GetServer returns the TURN server instance running the STUNner daemon
func (s *Stunner) GetServer() *turn.Server {
	return s.server
}

// GetAdmin returns the adminisittive information for STUNner
func (s *Stunner) GetAdmin() *object.Admin {
	a, found := s.adminManager.Get(v1alpha1.DefaultAdminName)
	if !found {
		panic("internal error: no Admin found")
	}
	return a.(*object.Admin)
}

// GetAdmin returns the STUNner authenitator
func (s *Stunner) GetAuth() *object.Auth {
	a, found := s.authManager.Get(v1alpha1.DefaultAuthName)
	if !found {
		panic("internal error: no Auth found")
	}
	return a.(*object.Auth)
}

// GetListener returns a STUNner listener or nil of no listener with the given name found
func (s *Stunner) GetListener(name string) *object.Listener {
	l, found := s.listenerManager.Get(name)
	if !found {
		return nil
	}
	return l.(*object.Listener)
}

// GetCluster returns a STUNner cluster or nil of no cluster with the given name found
func (s *Stunner) GetCluster(name string) *object.Cluster {
	l, found := s.clusterManager.Get(name)
	if !found {
		return nil
	}
	return l.(*object.Cluster)
}

// GetLogger returns the logger factory of the running daemon, useful for creating a sub-logger
func (s *Stunner) GetLogger() logging.LoggerFactory {
	return s.logger
}

// String returns a short description of the running STUNner instance
func (s *Stunner) String() string {
	listeners := s.listenerManager.Keys()
	ls := make([]string, len(listeners))
	for i, l := range listeners {
		ls[i] = s.GetListener(l).String()
	}
	str := "NONE"
	if len(ls) > 0 {
		str = strings.Join(ls, ", ")
	}

	auth := s.GetAuth()
	return fmt.Sprintf("authentication type: %s, realm: %s, listeners: %s",
		auth.Type.String(), auth.Realm, str)
}

// Close stops the STUNner daemon, cleans up any associated state and closes all connections it is
// managing
func (s *Stunner) Close() {
	s.log.Info("closing STUNner")

	// stop the server
	s.Stop()

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
		if err := l.Close(); err != nil && err != v1alpha1.ErrRestartRequired {
			s.log.Errorf("Error closing listener %q at adddress %s: %s",
				l.Proto.String(), l.Addr, err.Error())
		}
	}

	clusters := s.clusterManager.Keys()
	for _, name := range clusters {
		c := s.GetCluster(name)
		if err := c.Close(); err != nil && err != v1alpha1.ErrRestartRequired {
			s.log.Errorf("Error closing cluster %q: %s", c.ObjectName(),
				err.Error())
		}
	}

	s.resolver.Close()
}
