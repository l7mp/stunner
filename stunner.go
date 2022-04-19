package main

import (
	"fmt"
	"net/url"
	"strings"
	"strconv"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/google/uuid"
	"github.com/google/go-cmp/cmp"
)

type Stunner struct {
	version, name, logLevel, realm string
	log       logging.LeveledLogger
	auth      *authenticator
	server    *turn.Server
	listeners []*listener
}

func NewStunner(req StunnerRequest) (*Stunner, error) {
	s := Stunner{}

	// if req.admin == nil { req.admin = AdminRequest{} }
	if req.Admin.LogLevel == "" { req.Admin.LogLevel = defaultLogLevel }
	s.logLevel = req.Admin.LogLevel
	logger := NewLogger(req.Admin.LogLevel, "stunner")

	s.log = logger.NewLogger("stunner")
	s.log.Debugf("NewStunner: req: %#v", req)
	
	if req.ApiVersion != apiVersion {
		return nil, fmt.Errorf("unsupported API version: %s", req.ApiVersion)
	}
	s.version = req.ApiVersion
	
	if req.Admin.Name  == "" { req.Admin.Name = uuid.NewString() }
	if req.Admin.Realm == "" { req.Admin.Realm = defaultRealm }
	s.name, s.realm = req.Admin.Name, req.Admin.Realm
	
	static := req.Static
	s.log.Tracef("NewStunner: setting up authenticator")
	auth, authErr := newAuthenticator(static.Auth, s.realm, logger.NewLogger("stunner-auth"))
	if authErr != nil {
		return nil, authErr
	}
	s.auth = auth

	for _, lreq := range static.Listeners {
		s.log.Tracef("NewStunner: setting up listener from %#v", lreq)
		l, err := newListener(lreq, logger.NewLogger("stunner-listener"))
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
		LoggerFactory:  logger,
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

func (s *Stunner) GetConfig() StunnerRequest {
	s.log.Tracef("GetConfig")

	c := StunnerRequest{
		ApiVersion: s.version,
		Admin: AdminRequest{
			Name: s.name,
			LogLevel: s.logLevel,
			Realm: s.realm,
			// AccessLog: s.accessLog,
		},
		Static: StaticResourceRequest{
			Auth: s.auth.getConfig(),
			Listeners: make([]ListenerRequest, len(s.listeners)),
		},
	}
	
	for _, l := range s.listeners {
		c.Static.Listeners = append(c.Static.Listeners, l.getConfig())
	}
	
	return c
}

func  (s *Stunner) Close(){
	s.log.Debug("Closing Stunner")
	s.server.Close()
}

// at the moment, all updates are destructive: we close the server and restart with new config
// Reconcile is idempontent
func (s *Stunner) Reconcile(newConfig StunnerRequest) error {
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

/////
func DefaultStunnerConfig(uri, logLevel string) (*StunnerRequest, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("Invalid URI '%s': %s", uri, err)
	}

	if u.Scheme != "turn" && u.Scheme != "udp" {
		return nil, fmt.Errorf("Invalid protocol: %s", u.Scheme)
	}

	password, found := u.User.Password()
	if !found {
		return nil, fmt.Errorf("Invalid user:passwd pair: %s", u.User)
	}
	
	proto := "UDP"
	if m, err := url.ParseQuery(u.RawQuery); err != nil {
		if len(m["transport"]) > 0 {
			proto = m["protocol"][0]
		}
	}

	port, _ := strconv.Atoi(u.Port())
	return &StunnerRequest{
			ApiVersion: apiVersion,
			Admin: AdminRequest{
				LogLevel: logLevel,
				Realm: defaultRealm,
			},
			Static: StaticResourceRequest{
				Auth: AuthRequest{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": u.User.Username(),
						"password": password,
					},
				},
				Listeners: []ListenerRequest{{
					Protocol: proto,
					Addr: u.Hostname(),
					Port: port,
				}},
			},
	}, nil
}
