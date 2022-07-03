// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	// "fmt"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/logger"
	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Stunner is an instance of the STUNner deamon
type Stunner struct {
	version                                                    string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	resolver                                                   resolver.DnsResolver
	logger                                                     *logger.LoggerFactory
	log                                                        logging.LeveledLogger
	server                                                     *turn.Server
	net                                                        *vnet.Net
}

// NewStunner creates a new STUNner deamon from the specified configuration
func NewStunner(req v1alpha1.StunnerConfig) (*Stunner, error) {
	return newStunner(req, nil)
}

// NewStunnerWithVNet creates a new STUNner deamon from the specified configuration, using a
// vnet.Net instance to test STUNner over an emulated data-plane
func NewStunnerWithVNet(req v1alpha1.StunnerConfig, net *vnet.Net) (*Stunner, error) {
	return newStunner(req, net)
}

func newStunner(req v1alpha1.StunnerConfig, net *vnet.Net) (*Stunner, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	loggerFactory := logger.NewLoggerFactory(req.Admin.LogLevel)
	s := Stunner{
		version:         req.ApiVersion,
		logger:          loggerFactory,
		log:             loggerFactory.NewLogger("stunner"),
		adminManager:    manager.NewManager("admin-manager", loggerFactory),
		authManager:     manager.NewManager("auth-manager", loggerFactory),
		listenerManager: manager.NewManager("listener-manager", loggerFactory),
		clusterManager:  manager.NewManager("cluster-manager", loggerFactory),
		resolver:        resolver.NewDnsResolver("dns-resolver", loggerFactory),
	}

	if net == nil {
		s.net = vnet.NewNet(nil)
	} else {
		s.net = net
		s.log.Warn("vnet is enabled")
	}

	s.log.Tracef("NewStunner: %#v", req)

	if err := s.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &s, nil
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
