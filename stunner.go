// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/pion/logging"
	"github.com/pion/transport/v3"
	"github.com/pion/transport/v3/stdnet"

	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

const DefaultLogLevel = "all:WARN"

var DefaultInstanceId = fmt.Sprintf("stunnerd-%s", uuid.New().String())

// Stunner is an instance of the STUNner deamon.
type Stunner struct {
	id                                                         string
	version                                                    string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	suppressRollback, dryRun                                   bool
	resolver                                                   resolver.DnsResolver
	udpThreadNum                                               int
	logger                                                     *logger.LeveledLoggerFactory
	log                                                        logging.LeveledLogger
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
			log.Error("could not create stdnet.NewNet")
			return nil
		}
		vnet = net
	} else {
		vnet = options.Net
		log.Warn("vnet is enabled")
	}

	udpThreadNum := 0
	if options.UDPListenerThreadNum > 0 {
		udpThreadNum = options.UDPListenerThreadNum
	}

	id := options.Id
	if id == "" {
		if h, err := os.Hostname(); err != nil {
			id = DefaultInstanceId
		} else {
			id = h
		}
	}

	s := &Stunner{
		id:               id,
		version:          stnrv1.ApiVersion,
		logger:           logger,
		log:              log,
		suppressRollback: options.SuppressRollback,
		dryRun:           options.DryRun,
		resolver:         r,
		udpThreadNum:     udpThreadNum,
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
		// telemetry.RegisterMetrics(s.log, func() float64 { return s.GetActiveConnections() })
	}

	// TODO: remove this when STUNner gains self-managed dataplanes
	s.ready = true

	return s
}

// GetId returns the id of the current stunnerd instance.
func (s *Stunner) GetId() string {
	return s.id
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
	a, found := s.adminManager.Get(stnrv1.DefaultAdminName)
	if !found {
		panic("internal error: no Admin found")
	}
	return a.(*object.Admin)
}

// GetAuth returns the authenitation object underlying STUNner.
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
	return fmt.Sprintf("status: %s, realm: %s, authentication: %s, listeners: %s"+
		", active allocations: %d", status, auth.Realm, auth.Type.String(), str,
		s.AllocationCount())
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

// GetActiveConnections returns the number of active downstream (listener-side) TURN allocations.
func (s *Stunner) GetActiveConnections() float64 { return 0.0 }
