// Package stunner contains the public API for l7mp/stunner, a Kubernetes ingress gateway for WebRTC
package stunner

import (
	"fmt"
	"net"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/manager"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Stunner is an instance of the STUNner deamon
type Stunner struct {
        version    string
	adminManager, authManager, listenerManager, clusterManager manager.Manager
	logger     logging.LoggerFactory
	log        logging.LeveledLogger
	server     *turn.Server
	net        *vnet.Net
        dryRun     bool
}

// NewStunner creates the STUNner deamon from the specified configuration
func NewStunner(req *v1alpha1.StunnerConfig) (*Stunner, error) {
        if err := req.Validate(); err != nil {
                return nil, err
        }

        logger := NewLoggerFactory(req.Admin.LogLevel)
        s := Stunner{
                version:         req.ApiVersion,
                logger:          logger,
                log:             logger.NewLogger("stunner"),
                adminManager:    manager.NewManager("admin-manager", logger),
                authManager:     manager.NewManager("auth-manager", logger),
                listenerManager: manager.NewManager("listener-manager", logger),
                clusterManager:  manager.NewManager("cluster-manager", logger),
                dryRun:          false,
        }

	if req.Net == nil {
		s.net = vnet.NewNet(nil)
	} else {
		s.net = req.Net
		s.log.Warn("vnet is enabled")
	}

        s.log.Tracef("NewStunner: %#v", req)

        if err := s.Reconcile(req); err != nil {
                return nil, err
        }

	s.log.Infof("STUNner starting with API version %q", s.version)

        if err := s.startServer(); err != nil {
                return nil, err
        }
        
        return &s, nil
}

// NewDefaultStunnerConfig builds a default configuration from a STUNner URI. Example: the URI
// `turn://user:pass@127.0.0.1:3478` will be parsed into a STUNner configuration with a server
// running on the localhost at port 3478, with plain-text authentication using the
// username/password pair `user:pass`.
func NewDefaultStunnerConfig(uri, logLevel string) (*v1alpha1.StunnerConfig, error) {
        u, err := ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("Invalid URI '%s': %s", uri, err)
	}

	if u.Protocol != "udp" {
		return nil, fmt.Errorf("Invalid protocol: %s", u.Protocol)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("Username/password must be set: '%s'", uri)
	}

	return &v1alpha1.StunnerConfig{
                ApiVersion: v1alpha1.ApiVersion,
                Admin: v1alpha1.AdminConfig{
                        LogLevel: logLevel,
                },
                Auth: v1alpha1.AuthConfig{
                        Type: "plaintext",
                        Realm: v1alpha1.DefaultRealm,
                        Credentials: map[string]string{
                                "username": u.Username,
                                "password": u.Password,
                        },
                },
                Listeners: []v1alpha1.ListenerConfig{{
                        Protocol: u.Protocol,
                        Addr: u.Address,
                        Port: u.Port,
                }},
	}, nil
}

// GetConfig returns the configuration of the running STUNner daemon
func (s *Stunner) GetConfig() *v1alpha1.StunnerConfig {
	s.log.Tracef("GetConfig")

        listeners := s.listenerManager.Keys()
        clusters  := s.clusterManager.Keys()

	c := v1alpha1.StunnerConfig{
		ApiVersion: s.version,
		Admin:      *s.GetAdmin().GetConfig().(*v1alpha1.AdminConfig),
                Auth:       *s.GetAuth().GetConfig().(*v1alpha1.AuthConfig),
                Listeners:  make([]v1alpha1.ListenerConfig, len(listeners)),
                Clusters:   make([]v1alpha1.ClusterConfig, len(clusters)),
	}

	for i, name := range listeners {
		c.Listeners[i] = *s.GetListener(name).GetConfig().(*v1alpha1.ListenerConfig)
        }

	for i, name := range clusters {
		c.Clusters[i] = *s.GetCluster(name).GetConfig().(*v1alpha1.ClusterConfig)
	}

        return &c
}

// Reconcile handles the updates to the STUNner configuration. Some updates are destructive: the server is closed and restarted with the new configuration, see the documentation of the corresponding STUNner object for when STUNner may restart after a reconciliation
func (s *Stunner) Reconcile(req *v1alpha1.StunnerConfig) error {
	s.log.Debugf("reconciling STUNner for config: %#v ", req)

        // validate config
        if err := req.Validate(); err != nil {
                return fmt.Errorf("invalid config: %#v ", req)
        }

        restart := false

        // admin
        newAdmin, err := s.adminManager.Reconcile([]v1alpha1.Config{&req.Admin})
        if err != nil {
                if err == object.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile admin config: %s", err.Error())
                }
        }

        for _, c := range newAdmin {
                o, err := object.NewAdmin(c, s.logger)
                if err != nil {
                        return err
                }
                s.adminManager.Upsert(o)
        }
        s.logger = NewLoggerFactory(s.GetAdmin().LogLevel)
        s.log    = s.logger.NewLogger("stunner")

        // auth
        newAuth, err := s.authManager.Reconcile([]v1alpha1.Config{&req.Auth})
        if err != nil {
                if err == object.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile auth config: %s", err.Error())
                }
        }

        for _, c := range newAuth {
                o, err := object.NewAuth(c, s.logger)
                if err != nil {
                        return err
                }
                s.authManager.Upsert(o)
        }

        // listener
        lconf := make([]v1alpha1.Config, len(req.Listeners))
        for i, c := range req.Listeners {
                lconf[i] = &c
        }
        newListener, err := s.listenerManager.Reconcile(lconf)
        if err != nil {
                if err == object.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile listener config: %s", err.Error())
                }
        }

        for _, c := range newListener {
                o, err := object.NewListener(c, s.net, s.logger)
                if err != nil {
                        return err
                }
                s.listenerManager.Upsert(o)
        }

        // cluster
        cconf := make([]v1alpha1.Config, len(req.Clusters))
        for i, c := range req.Clusters {
                cconf[i] = &c
        }
        newCluster, err := s.clusterManager.Reconcile(cconf)
        if err != nil {
                if err == object.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile cluster config: %s", err.Error())
                }
        }

        for _, c := range newCluster {
                o, err := object.NewCluster(c, s.logger)
                if err != nil {
                        return err
                }
                s.clusterManager.Upsert(o)
        }

        if restart {
                s.server.Close()
                if err := s.startServer(); err != nil {
                        return err
                }
        }

        return nil
}

// Close stops the STUNner daemon. It cleans up any associated state and closes all connections it is managing
func  (s *Stunner) Close(){
	s.log.Info("Closing Stunner")
        if s.server != nil {
                s.server.Close()
        }
}

// GetVersion returns the STUNner API version
func  (s *Stunner) GetVersion() string {
        return s.version
}

// GetServer returns the TURN server instance running the STUNner daemon
func  (s *Stunner) GetServer() *turn.Server {
        return s.server
}

// GetAdmin returns the adminisittive information for STUNner
func (s *Stunner) GetAdmin() *object.Admin {
        a, found := s.adminManager.Get(v1alpha1.DefaultAdminName)
        if !found { panic("internal error: no Admin found") }
        return a.(*object.Admin)
}

// GetAdmin returns the STUNner authenitator
func (s *Stunner) GetAuth() *object.Auth {
        a, found := s.authManager.Get(v1alpha1.DefaultAuthName)
        if !found { panic("internal error: no Auth found") }
        return a.(*object.Auth)
}

// GetListener returns a STUNner listener or nil of no listener with the given name found
func (s *Stunner) GetListener(name string) *object.Listener {
        l, found := s.listenerManager.Get(name)
        if !found { return nil }
        return l.(*object.Listener)
}

// GetCluster returns a STUNner cluster or nil of no cluster with the given name found
func (s *Stunner) GetCluster(name string) *object.Cluster {
        l, found := s.clusterManager.Get(name)
        if !found { return nil }
        return l.(*object.Cluster)
}


// Private API
func (s *Stunner) newPermissionHandler(l *object.Listener) turn.PermissionHandler {
	s.log.Trace("newPermissionHandler")

        return func (src net.Addr, peer net.IP) bool {
                peerIP := peer.String()
                s.log.Debugf("permission handler for listener %q: client %q, peer %q",
                        l.Name, src.String(), peerIP)
                clusters := s.clusterManager.Keys()

                for _, r := range l.Routes {
                        s.log.Tracef("considering route for cluster %q", r)
                        if contains(clusters, r) {
                                s.log.Tracef("considering endpoints for cluster %q", r)
                                e := s.GetCluster(r)
                                if contains(e.Endpoints, peerIP){
                                        s.log.Debugf("permission granted on listener %q for client %q",
                                                "to peer %s based via cluster %q", l.Name, src.String(),
                                                peerIP, r)
                                        return true
                                }
                        }
                }
                s.log.Debugf("permission denied on listener %q for client %q to peer %s: no route to endpoint",
                        l.Name, src.String(), peerIP)
                return false
        }
}

func contains(list []string, a string) bool {
        for _, b := range list {if b == a { return true } }
        return false
}

func (s *Stunner) startServer() error {
	s.log.Debug("(re)starting the TURN server for STUNner")

        if s.dryRun { return nil }
        
        auth :=  s.GetAuth()

	var pconn []turn.PacketConnConfig
	var conn  []turn.ListenerConfig

        listeners := s.listenerManager.Keys()
	for _, name := range listeners {
		l := s.GetListener(name)

		switch l.Proto {
		case v1alpha1.ListenerProtocolUdp:
                        c := l.Conn.(turn.PacketConnConfig)
                        c.PermissionHandler = s.newPermissionHandler(l)
                        pconn = append(pconn, c)
		case v1alpha1.ListenerProtocolTcp, v1alpha1.ListenerProtocolTls, v1alpha1.ListenerProtocolDtls: 
                        c := l.Conn.(turn.ListenerConfig)
                        c.PermissionHandler = s.newPermissionHandler(l)
                        conn = append(conn, c)
		default: panic("internal error")
		}
	}

	t, err := turn.NewServer(turn.ServerConfig{
		Realm: auth.Realm,
		AuthHandler: func(username, realm string, srcAddr net.Addr) (key []byte, ok bool) {
                        // dynamic: authHandler might have changed behind ur back
                        auth := s.GetAuth()
                        return auth.Handler(username, realm, srcAddr)
                },
		LoggerFactory:  s.logger,
		PacketConnConfigs: pconn,
		ListenerConfigs: conn,
	})
	if err != nil {
		return fmt.Errorf("cannot set up TURN server: %s", err)
	}
	s.server = t

	ls := make([]string, len(listeners))
	for i, l := range listeners { ls[i] = s.GetListener(l).String() }
	s.log.Infof("TURN server running, realm: %s, listeners: %s", auth.Realm,
		strings.Join(ls, ", "))

	return nil
}
