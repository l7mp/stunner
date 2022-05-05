package stunner

import (
	"fmt"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/pion/transport/vnet"
	"github.com/google/uuid"
	"github.com/google/go-cmp/cmp"
)

// Stunner is an instance of the STUNner deamon
type Stunner struct {
	version, name, logLevel, realm string
	logger    logging.LoggerFactory
	log       logging.LeveledLogger
	auth      *authenticator
	server    *turn.Server
	listeners []*listener
	net       *vnet.Net
}

// NewStunner creates the STUNner deamon using the specified configuration
func NewStunner(conf *StunnerConfig) (*Stunner, error) {
	s := Stunner{}

	// if conf.admin == nil { conf.admin = AdminConfig{} }
	if conf.Admin.LogLevel == "" { conf.Admin.LogLevel = DefaultLogLevel }
	s.logLevel = conf.Admin.LogLevel
	s.logger = NewLoggerFactory(conf.Admin.LogLevel)
	s.log = s.logger.NewLogger("stunner")
	
	if conf.ApiVersion != ApiVersion {
		return nil, fmt.Errorf("unsupported API version: %s", conf.ApiVersion)
	}
	s.version = conf.ApiVersion

	if conf.Net == nil {
		s.net = vnet.NewNet(nil)
	} else {
		s.net = conf.Net
		s.log.Warn("vnet is enabled")
	}
	
	if conf.Admin.Name  == "" { conf.Admin.Name = fmt.Sprintf("stunnerd:%s", uuid.NewString()) }
	if conf.Admin.Realm == "" { conf.Admin.Realm = DefaultRealm }
	s.name, s.realm = conf.Admin.Name, conf.Admin.Realm

	s.log.Debugf("NewStunner: Starting with config: %#v", conf)
	
	static := conf.Static
	
	s.log.Tracef("NewStunner: setting up authenticator")
	if static.Auth.Type == "" { static.Auth.Type = DefaultAuthType }
	auth, authErr := s.newAuthenticator(static.Auth)
	if authErr != nil {
		return nil, authErr
	}
	s.auth = auth

	for _, lconf := range static.Listeners {
		s.log.Tracef("setting up listener with config: %#v", lconf)
		l, err := s.newListener(lconf)
		if err != nil {
			return nil, err
		}
		s.listeners = append(s.listeners, l)
	}

	var pconn []turn.PacketConnConfig
	var conn  []turn.ListenerConfig

	for _, l := range s.listeners {
		switch l.proto {
		case ListenerProtocolUdp:  pconn = append(pconn, l.conn.(turn.PacketConnConfig))
		case ListenerProtocolTcp:  conn  = append(conn, l.conn.(turn.ListenerConfig))
		case ListenerProtocolTls:  conn  = append(conn, l.conn.(turn.ListenerConfig))
		case ListenerProtocolDtls: conn  = append(conn, l.conn.(turn.ListenerConfig))
		default: panic("internal error")
		}
	}	

	t, err := turn.NewServer(turn.ServerConfig{
		Realm: s.realm,
		AuthHandler: s.auth.handler,
		LoggerFactory:  s.logger,
		PacketConnConfigs: pconn,
		ListenerConfigs: conn,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot set up TURN server: %s", err)
	}
	s.server = t
	
	ls := make([]string, len(s.listeners))
	for i, l := range s.listeners { ls[i] = l.String() }
	s.log.Infof("Stunner: TURN server running, realm: %s, listeners: %s", s.realm,
		strings.Join(ls, ", "))

	return &s, nil
}

func (s *Stunner) GetConfig() StunnerConfig {
	s.log.Tracef("GetConfig")

	c := StunnerConfig{
		ApiVersion: s.version,
		Admin: AdminConfig{
			Name: s.name,
			LogLevel: s.logLevel,
			Realm: s.realm,
			// AccessLog: s.accessLog,
		},
		Static: StaticResourceConfig{
			Auth: s.auth.getConfig(),
			Listeners: make([]ListenerConfig, len(s.listeners)),
		},
	}
	
	for _, l := range s.listeners {
		c.Static.Listeners = append(c.Static.Listeners, l.getConfig())
	}
	
	return c
}

// Close stops the STUNner daemon. It cleans up any associated state and closes all connections it is managing
func  (s *Stunner) Close(){
	s.log.Debug("Closing Stunner")
	s.server.Close()
}

// GetServer returns the TURN server instance running the STUNner daemon
func  (s *Stunner) GetServer() *turn.Server {
	return s.server
}

// Reconcile handles the updates to the STUNner configuration. At the moment, all updates are destructive: the server is closed and restarted with the new configuration
func (s *Stunner) Reconcile(newConfig *StunnerConfig) error {
	s.log.Trace("Reconcile")

	oldConfig := s.GetConfig()
	s.log.Tracef("GetConfig: %#v", oldConfig)

	eq := cmp.Equal(oldConfig, newConfig, listenerListTransformer)
	if !eq {
		s.log.Debugf("Reconcile: config diff: %s",
			cmp.Diff(oldConfig, newConfig, listenerListTransformer))

		s.log.Infof("Reconcile: restarting Stunner with new config: %#v", newConfig)
		s.Close()

		news, err := NewStunner(newConfig)
		if err != nil {
			return err
		}
		s = news
	} else {
		s.log.Info("Reconcile: unchanged")
	}

	return nil
}

// NewDefaultStunnerConfig builds a default configuration from a STUNner URI. Example: the URI
// `turn://user:pass@127.0.0.1:3478` will be parsed into a STUNner configuration with a server
// running on the localhost at port 3478, with plain-text authentication using the
// username/password pair `user:pass`.
func NewDefaultStunnerConfig(uri, logLevel string) (*StunnerConfig, error) {
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

	return &StunnerConfig{
			ApiVersion: ApiVersion,
			Admin: AdminConfig{
				LogLevel: logLevel,
				Realm: DefaultRealm,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": u.Username,
						"password": u.Password,
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: u.Protocol,
					Addr: u.Address,
					Port: u.Port,
				}},
			},
	}, nil
}
