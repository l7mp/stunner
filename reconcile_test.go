package stunner

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	// "strconv"
	"testing"
	"time"

	"github.com/pion/transport/v5/test"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	objectturn "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	a12n "github.com/l7mp/stunner/v2/pkg/authentication"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

var _ = fmt.Sprintf("%d", 1)

const (
	dummyCert64 = "ZHVtbXktY2VydA==" // "dummy-cert"
	dummyKey64  = "ZHVtbXkta2V5"     // "dummy-key"
)

// *****************
// Reconciliation tests
// *****************
type StunnerReconcileTestConfig struct {
	name   string
	config stnrv2.StunnerConfig
	tester func(t *testing.T, s *Stunner, err error)
}

var testReconcileDefault = []StunnerReconcileTestConfig{
	{
		name: "reconcile-test: default admin",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), stnrv2.DefaultStunnerName, "stunner name")
			// make sure we get the right loglevel, we may override this for debugging the tests
			// assert.Equal(t, admin.LogLevel, stnrv2.DefaultLogLevel, "stunner loglevel")

			assert.NotNil(t, s.GetAuth(), "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "user", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "pass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "user",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "user", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("user",
				stnrv2.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			// the server uses the open cluster
		},
	},
	{
		name: "reconcile-test: empty credentials are accepted: user",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "missing credentials are no config error: the TURN server refuses every client")
		},
	},
	{
		name: "reconcile-test: empty credentials are accepted: passwd",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "missing credentials are no config error: the TURN server refuses every client")
		},
	},
	{
		name: "reconcile-test: auth-type=none is OK",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "none",
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "empty username or password is OK with auth type none")
			assert.Equal(t, stnrv2.AuthTypeNone, mustAuthType(t, s.GetAuth()))
			authConfig, ok := s.GetAuth().GetConfig().(*stnrv2.AuthConfig)
			assert.True(t, ok)
			assert.Empty(t, authConfig.Credentials)
		},
	},
	{
		name: "reconcile-test: empty listener is fine",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// deleting a listener does not require a restart
			assert.NoError(t, err, "restarted")
		},
	},
	{
		name: "reconcile-test: empty listener name errs",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{""},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "missing name")
		},
	},
	{
		name: "reconcile-test: empty cluster is fine",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")
		},
	},
	{
		name: "reconcile-test: empty cluster name errs",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "missing name", "missing username")
		},
	},
	////////////// reconcile tests
	/// admin
	{
		name: "reconcile-test: reconcile name",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				Name:     "new-name",
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			// check everyting
			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), "new-name", "stunner name")
			// assert.Equal(t, admin.LogLevel, stnrv2.DefaultLogLevel, "stunner loglevel")

			assert.NotNil(t, s.GetAuth(), "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "user", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "pass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "user",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "user", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("user",
				stnrv2.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile loglevel",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: "anything",
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), "default-stunnerd", "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.NotNil(t, s.GetAuth(), "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "user", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "pass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "user",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "user", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("user",
				stnrv2.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile metrics_endpoint",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel:        "anything",
				MetricsEndpoint: "http://0.0.0.0:8080/metrics",
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// metrics endpoint changed: the metrics server restart is reported
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Contains(t, e.Objects, "metrics: default-metrics", "restarted object")

			// check everyting
			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), "default-stunnerd", "stunner name")
			// assert.Equal(t, admin.LogLevel, stnrv2.DefaultLogLevel, "stunner loglevel")
			adminConf := admin.GetConfig().(*stnrv2.AdminConfig)
			assert.Equal(t, adminConf.MetricsEndpoint, "http://0.0.0.0:8080/metrics",
				"stunner metrics endpoint")

			assert.NotNil(t, s.GetAuth(), "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "user", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "pass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "user",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "user", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("user",
				stnrv2.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	/// auth
	{
		name: "reconcile-test: reconcile staticauth name",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "newuser",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "newuser", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "pass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "newuser",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "newuser", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("newuser",
				stnrv2.DefaultRealm, "pass"), "auth handler ok")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), stnrv2.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile static auth passwd",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "newpass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeStatic, mustAuthType(t, auth), "auth type ok")

			assert.Equal(t, authCreds(t, auth)["username"], "user", "username ok")
			assert.Equal(t, authCreds(t, auth)["password"], "newpass", "password ok")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: "user",
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, "user", userID, "authHandler userID ok")
			assert.Equal(t, key, a12n.GenerateAuthKey("user",
				stnrv2.DefaultRealm, "newpass"), "auth handler ok")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), stnrv2.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile ephemeral auth",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "ephemeral",
				Credentials: map[string]string{
					"secret": "newsecret",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, stnrv2.AuthTypeEphemeral, mustAuthType(t, auth), "auth type ok")
			assert.Equal(t, authCreds(t, auth)["secret"], "newsecret")

			duration, _ := time.ParseDuration("10h")
			username := a12n.GenerateTimeWindowedUsername(time.Now(), duration, "dummy_user")
			passwd, err := a12n.GetLongTermCredential(username, "newsecret")
			assert.NoError(t, err, "GetLongTermCredential")

			handler := newAuthHandler(s)
			userID, key, ok := callAuthHandler(t, handler, &turn.RequestAttributes{
				Username: username,
				Realm:    stnrv2.DefaultRealm,
				SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
			})
			assert.True(t, ok, "authHandler key ok")

			key2 := a12n.GenerateAuthKey(username, stnrv2.DefaultRealm, passwd)
			assert.Equal(t, key, key2, "authHandler key matches")

			// userID must be the parsed user-id portion ("dummy_user")
			assert.Equal(t, "dummy_user", userID, "authHandler userID ok for ephemeral auth")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), stnrv2.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	/// listener
	{
		name: "reconcile-test: reconcile existing listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Protocol: "TCP",
				Servers:  []string{"default-server"},
				Addr:     "127.0.0.2",
				Port:     12345,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// requires a restart!
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Len(t, e.Objects, 1, "restarted object")
			assert.Contains(t, e.Objects, "listener: default-listener")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTCP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.2", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, 12345, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "server cluster count ok")
			// sorted!!!
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "none", "server cluster name ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[1], "dummy", "server cluster name ok")

			assert.NotNil(t, s.GetAdmin(), "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, mustAdminName(t, admin), stnrv2.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: rename listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "renamed-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"renamed-listener"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "renamed-listener",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")

			assert.Nil(t, s.GetListener("default-listener"), "old listener deleted")

			l := s.GetListener("renamed-listener")
			assert.NotNil(t, l, "renamed listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, "allow-any", serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "server cluster name ok")
		},
	},
	{
		name: "reconcile-test: reconcile new listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "newlistener",
				Protocol: "TCP",
				Servers:  []string{"newlistener"},
				Addr:     "127.0.0.2",
				Port:     1,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "newlistener",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// does not require a restart!
			assert.NoError(t, err, "restarted")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.Nil(t, l, "listener found")

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTCP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.2", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, 1, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "server cluster count ok")
			// sorted!
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "none", "server cluster name ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[1], "dummy", "server cluster name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: empty TLS credentials pass validation",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "newlistener",
				Protocol: "TLS",
				Servers:  []string{"newlistener"},
				Addr:     "127.0.0.2",
				Port:     1,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "newlistener",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// validation is syntactic: the listener fails when it starts, not here
			assert.NoError(t, err, "reconcile")
			assert.NotNil(t, s.GetListener("newlistener"), "listener created")
		},
	},
	{
		name: "reconcile-test: reconcile additional listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}, {
				Name:     "newlistener",
				Protocol: "TCP",
				Servers:  []string{"newlistener"},
				Addr:     "127.0.0.2",
				Port:     1,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}, {
				Name:     "newlistener",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// does not require a restart!
			assert.NoError(t, err, "restart")

			assert.Len(t, s.GetListeners(), 2, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			// the server uses the old cluster

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTCP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.2", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, 1, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "server cluster count ok")
			// sorted!
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "none", "server cluster name ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[1], "dummy", "server cluster name ok")
		},
	},
	{
		name: "reconcile-test: reconcile existing listener with TLS cert and add a new one",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "DTLS",
				Servers:  []string{"default-server"},
				Cert:     dummyCert64,
				Key:      dummyKey64,
			}, {
				Name:     "newlistener",
				Protocol: "TCP",
				Servers:  []string{"newlistener"},
				Addr:     "127.0.0.2",
				Port:     1,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}, {
				Name:     "newlistener",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// default-listener restarts
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Len(t, e.Objects, 1, "restarted object")
			assert.Contains(t, e.Objects, "listener: default-listener")

			assert.Len(t, s.GetListeners(), 2, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolDTLS.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Cert, dummyCert64, "listener cert ok")
			assert.Equal(t, listenerConf(t, l).Key, dummyKey64, "listener key ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			// the server uses the old cluster

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTCP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.2", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, 1, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "server cluster count ok")
			// sorted!
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "none", "server cluster name ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[1], "dummy", "server cluster name ok")
		},
	},
	{
		name: "reconcile-test: reconcile existing listener with TLS cert and add a new one",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "TLS",
				Servers:  []string{"default-server"},
				Cert:     dummyCert64,
				Key:      dummyKey64,
			}, {
				Name:     "newlistener",
				Protocol: "TCP",
				Servers:  []string{"newlistener"},
				Addr:     "127.0.0.2",
				Port:     1,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}, {
				Name:     "newlistener",
				Type:     "turn",
				Clusters: []string{"none", "dummy"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// default-listener restarts
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Len(t, e.Objects, 1, "restarted object")
			assert.Contains(t, e.Objects, "listener: default-listener")

			assert.Len(t, s.GetListeners(), 2, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTLS.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Cert, dummyCert64, "listener cert ok")
			assert.Equal(t, listenerConf(t, l).Key, dummyKey64, "listener key ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			// the server uses the old cluster

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolTCP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.2", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, 1, "listener port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "server cluster count ok")
			// sorted!
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "none", "server cluster name ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[1], "dummy", "server cluster name ok")
		},
	},
	{
		name: "reconcile-test: reconcile existing listener with new public IP and port",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:       "default-listener",
				Addr:       "127.0.0.1",
				Protocol:   "UDP",
				Servers:    []string{"default-server"},
				PublicAddr: "127.0.0.2",
				PublicPort: 33478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// does not require a restart!
			assert.NoError(t, err, "restart")

			assert.Len(t, s.GetListeners(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, listenerConf(t, l).Protocol, stnrv2.ProtocolUDP.String(), "listener proto ok")
			assert.Equal(t, listenerConf(t, l).Addr, "127.0.0.1", "listener address ok")
			assert.Equal(t, listenerConf(t, l).Port, stnrv2.DefaultPort, "listener port ok")
			assert.Equal(t, listenerConf(t, l).PublicAddr, "127.0.0.2", "listener public address ok")
			assert.Equal(t, listenerConf(t, l).PublicPort, 33478, "listener public port ok")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "allow-any", "server cluster name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile deleted listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// does not require a restart!
			assert.NoError(t, err, "restarted")

			l := s.GetListener("default-listener")
			assert.Nil(t, l, "listener found")

			l = s.GetListener("newlistener")
			assert.Nil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Len(t, s.GetListeners(), 0, "listenerManager keys")
		},
	},
	/// cluster
	{
		name: "reconcile-test: reconcile existing cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: rename cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"renamed-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "renamed-cluster",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Nil(t, s.GetCluster("allow-any"), "old cluster deleted")

			c := s.GetCluster("renamed-cluster")
			assert.NotNil(t, c, "renamed cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 1, "server cluster count ok")
			assert.Equal(t, "renamed-cluster", serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters[0], "server cluster name ok")
		},
	},
	{
		name: "reconcile-test: reconcile new cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.Nil(t, c, "cluster found")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile cluster with port range",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1:1-2", "2.2.2.2/8:3-4"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 1, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.Nil(t, c, "cluster found")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1:1-2", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8:3-4", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile additional cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
				Protocol:  "UDP",
			}, {
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 2, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile additional cluster and reroute",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"newcluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
				Protocol:  "UDP",
			}, {
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// only clusters have changed, we shouldn't need a restart
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 2, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile port-range",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"newcluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1:1-2", "2.2.2.2/8:3-4"},
				Protocol:  "UDP",
			}, {
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// only clusters have changed, we shouldn't need a restart
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 2, "cluster keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "0.0.0.0/0", "cluster endpoint ok")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			assert.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, clusterEndpoints(t, c)[0], "1.1.1.1:1-2", "cluster endpoint ok")
			assert.Equal(t, clusterEndpoints(t, c)[1], "2.2.2.2/8:3-4", "cluster endpoint ok")
		},
	},
	{
		name: "reconcile-test: reconcile deleted cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.rt.Registry.List(runtime.TypeCluster), 0, "cluster keys")
		},
	},
	{
		name: "reconcile-test: reconcile user quota",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				UserQuota: 12,
				LogLevel:  stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			a := s.GetAdmin()
			assert.NotNil(t, a, "admin")

			c := a.GetConfig()
			assert.NotNil(t, c, "admin-getconfig")
			ca, ok := c.(*stnrv2.AdminConfig)
			assert.True(t, ok, "adminconfig cast")
			assert.Equal(t, 12, ca.UserQuota, "quota")
		},
	},
	{
		name: "reconcile-test: reconcile offload mode",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				OffloadEngine: "XDP",
				LogLevel:      stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "none",
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.Error(t, err, "restarted") // changing the offload mode requires a restart
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Len(t, e.Objects, 1, "restarted object")
			assert.Contains(t, e.Objects, "offload: default-offload")

			a := s.GetAdmin()
			assert.NotNil(t, a, "admin")

			c := a.GetConfig()
			assert.NotNil(t, c, "admin-getconfig")
			ca, ok := c.(*stnrv2.AdminConfig)
			assert.True(t, ok, "adminconfig cast")
			assert.Equal(t, "XDP", ca.OffloadEngine, "offload")
		},
	},
	{
		name: "reconcile-test: reconcile offload interfaces (sorted)",
		config: stnrv2.StunnerConfig{
			// badly sorted
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				OffloadEngine:     "TC",
				OffloadInterfaces: []string{"c", "a", "b"}, // badly sorted
				LogLevel:          stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "none",
			},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.Error(t, err, "restarted") // changing the offload interfaces requires a restart
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Len(t, e.Objects, 1, "restarted object")
			assert.Contains(t, e.Objects, "offload: default-offload")

			a := s.GetAdmin()
			assert.NotNil(t, a, "admin")

			c := a.GetConfig()
			assert.NotNil(t, c, "admin-getconfig")
			ca, ok := c.(*stnrv2.AdminConfig)
			assert.True(t, ok, "adminconfig cast")
			assert.Equal(t, "TC", ca.OffloadEngine, "offload")
			assert.Equal(t, []string{"a", "b", "c"}, ca.OffloadInterfaces, "offload intfs")
		},
	},
	{
		name: "reconcile-test: TCP and UDP clusters on a TURN server behind a TCP listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Type:        "static",
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "tcp",
				Protocol: "TCP",
				Servers:  []string{"tcp"},
				Addr:     "127.0.0.1",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "tcp",
				Type:     "turn",
				Clusters: []string{"tcp-cluster", "udp-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "tcp-cluster",
				Type:      stnrv2.ClusterTypeStatic.String(),
				Endpoints: []string{"1.1.1.1", "2.2.2.0/24"},
				Protocol:  stnrv2.ProtocolTCP.String(),
			}, {
				Name:      "udp-cluster",
				Type:      stnrv2.ClusterTypeStatic.String(),
				Endpoints: []string{"3.3.3.3"},
				Protocol:  stnrv2.ProtocolUDP.String(),
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "reconcile")

			// The clusters expose their protocols through their public configs.
			tcpCluster := s.GetCluster("tcp-cluster")
			require.NotNil(t, tcpCluster, "tcp cluster present")
			tcpConf, ok := tcpCluster.GetConfig().(*stnrv2.ClusterConfig)
			require.True(t, ok, "cluster config cast")
			assert.Equal(t, stnrv2.ProtocolTCP.String(), tcpConf.Protocol, "tcp cluster protocol")
			udpCluster := s.GetCluster("udp-cluster")
			require.NotNil(t, udpCluster, "udp cluster present")
			udpConf, ok := udpCluster.GetConfig().(*stnrv2.ClusterConfig)
			require.True(t, ok, "cluster config cast")
			assert.Equal(t, stnrv2.ProtocolUDP.String(), udpConf.Protocol, "udp cluster protocol")
		},
	},
	// IPv6: listener address.
	{
		name: "reconcile-test: IPv6 listener address",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "2001:db8::1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// changing the listener address requires a restart
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Contains(t, e.Objects, "listener: default-listener", "restarted object")
			l := s.GetListener("default-listener")
			require.NotNil(t, l, "listener found")
			assert.Equal(t, "2001:db8::1", listenerConf(t, l).Addr, "listener IPv6 address ok")
		},
	},
	// IPv6: listener public address on an IPv4 listener (mixed families across addr/public-addr).
	{
		name: "reconcile-test: IPv6 listener public address",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:       "default-listener",
				Addr:       "127.0.0.1",
				Protocol:   "UDP",
				Servers:    []string{"default-server"},
				PublicAddr: "2001:db8::2",
				PublicPort: 33478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "reconcile")
			l := s.GetListener("default-listener")
			require.NotNil(t, l, "listener found")
			assert.Equal(t, "127.0.0.1", listenerConf(t, l).Addr, "listener address ok")
			assert.Equal(t, "2001:db8::2", listenerConf(t, l).PublicAddr, "listener IPv6 public address ok")
			assert.Equal(t, 33478, listenerConf(t, l).PublicPort, "listener public port ok")
		},
	},
	// IPv6: fully IPv6 listener with an IPv4 public address (the reverse mix).
	{
		name: "reconcile-test: IPv6 listener address with IPv4 public address",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:       "default-listener",
				Addr:       "2001:db8::1",
				Protocol:   "UDP",
				Servers:    []string{"default-server"},
				PublicAddr: "1.2.3.4",
				PublicPort: 33478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// changing the listener address requires a restart
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Contains(t, e.Objects, "listener: default-listener", "restarted object")
			l := s.GetListener("default-listener")
			require.NotNil(t, l, "listener found")
			assert.Equal(t, "2001:db8::1", listenerConf(t, l).Addr, "listener IPv6 address ok")
			assert.Equal(t, "1.2.3.4", listenerConf(t, l).PublicAddr, "listener IPv4 public address ok")
		},
	},
	// IPv6: fully-specified address in cluster endpoints (round-trips without a prefix length).
	{
		name: "reconcile-test: IPv6 cluster endpoint (fully-specified address)",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-some"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-some",
				Endpoints: []string{"2001:db8::1"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "reconcile")
			c := s.GetCluster("allow-some")
			require.NotNil(t, c, "cluster found")
			assert.Equal(t, stnrv2.ClusterTypeStatic, mustClusterType(t, c), "cluster type ok")
			require.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, "2001:db8::1", clusterEndpoints(t, c)[0], "cluster IPv6 endpoint ok")
		},
	},
	// IPv6: prefix in cluster endpoints.
	{
		name: "reconcile-test: IPv6 cluster endpoint (prefix)",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-some"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-some",
				Endpoints: []string{"2001:db8::/32"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "reconcile")
			c := s.GetCluster("allow-some")
			require.NotNil(t, c, "cluster found")
			require.Len(t, clusterEndpoints(t, c), 1, "cluster endpoint count ok")
			assert.Equal(t, "2001:db8::/32", clusterEndpoints(t, c)[0], "cluster IPv6 prefix ok")
		},
	},
	// IPv6: mixed IPv4/IPv6 endpoints in a single cluster; routing must match each family.
	{
		name: "reconcile-test: mixed IPv4/IPv6 cluster endpoints",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{"username": "user", "password": "pass"},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Addr:     "127.0.0.1",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-some"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-some",
				Endpoints: []string{"1.1.1.1", "2001:db8::/32"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "reconcile")
			c := s.GetCluster("allow-some")
			require.NotNil(t, c, "cluster found")
			require.Len(t, clusterEndpoints(t, c), 2, "cluster endpoint count ok")
			assert.Equal(t, "1.1.1.1", clusterEndpoints(t, c)[0], "cluster IPv4 endpoint ok")
			assert.Equal(t, "2001:db8::/32", clusterEndpoints(t, c)[1], "cluster IPv6 prefix ok")
		},
	},
	{
		name: "reconcile-test: TLS listener PQC mode",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Protocol: "TLS",
				Servers:  []string{"default-server"},
				Addr:     "127.0.0.1",
				Port:     3478,
				Key:      "ZHVtbXkK",
				Cert:     "ZHVtbXkK",
				PQCMode:  "Preferred",
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// the protocol changed: restarted, and the mode is parsed and normalized
			assert.Error(t, err, "restarted")
			e, ok := err.(stnrv2.ErrRestarted)
			assert.True(t, ok, "restarted status")
			assert.Contains(t, e.Objects, "listener: default-listener", "restarted object")

			l := s.GetListener("default-listener")
			require.NotNil(t, l, "listener found")
			assert.Equal(t, "preferred", listenerConf(t, l).PQCMode, "pqc mode normalized")
			assert.Contains(t, l.Status().String(), "pqc=preferred", "pqc mode in status")

			tlsConf := func(mode string, clusters ...string) *stnrv2.StunnerConfig {
				return &stnrv2.StunnerConfig{
					ApiVersion: stnrv2.ApiVersion,
					Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
					Auth: stnrv2.AuthConfig{Credentials: map[string]string{
						"username": "user", "password": "pass"}},
					Listeners: []stnrv2.ListenerConfig{{
						Name:     "default-listener",
						Protocol: "TLS",
						Servers:  []string{"default-server"},
						Addr:     "127.0.0.1",
						Port:     3478,
						Key:      "ZHVtbXkK",
						Cert:     "ZHVtbXkK",
						PQCMode:  mode,
					}},
					Servers: []stnrv2.ServerConfig{{
						Name:     "default-server",
						Type:     "turn",
						Clusters: clusters,
					}},
					Clusters: []stnrv2.ClusterConfig{{
						Name:      "allow-any",
						Endpoints: []string{"0.0.0.0/0"},
						Protocol:  "UDP",
					}},
				}
			}

			// a mode change restarts the listener, and only the listener
			err = s.Reconcile(tlsConf("enforced", "allow-any"))
			e, ok = err.(stnrv2.ErrRestarted)
			require.True(t, ok, "mode change: restarted status")
			assert.Equal(t, []string{"listener: default-listener"}, e.Objects, "mode change: restarted object")
			assert.Equal(t, "enforced", listenerConf(t, l).PQCMode, "mode change applied")

			// the same mode with a cluster change reconciles in place
			assert.NoError(t, s.Reconcile(tlsConf("enforced", "allow-any", "dummy")), "cluster change: no restart")
			assert.Equal(t, "enforced", listenerConf(t, l).PQCMode, "cluster change: mode kept")
			assert.Len(t, serverConf(t, s.GetServer(listenerConf(t, l).FirstServer())).Clusters, 2, "cluster change applied")

			// back to the default mode: a restart, and the default is the empty string
			err = s.Reconcile(tlsConf("Default", "allow-any", "dummy"))
			e, ok = err.(stnrv2.ErrRestarted)
			require.True(t, ok, "default mode: restarted status")
			assert.Equal(t, []string{"listener: default-listener"}, e.Objects, "default mode: restarted object")
			assert.Empty(t, listenerConf(t, l).PQCMode, "default mode normalized to empty")
			assert.NotContains(t, l.Status().String(), "pqc=", "default mode absent from status")

			// an omitted mode and a spelled-out default are the running mode: no restart
			assert.NoError(t, s.Reconcile(tlsConf("", "allow-any", "dummy")), "omitted mode: no restart")
			assert.NoError(t, s.Reconcile(tlsConf("default", "allow-any", "dummy")), "spelled-out default: no restart")

			// the mode must parse
			assert.ErrorContains(t, s.Reconcile(tlsConf("quantum", "allow-any", "dummy")), "unknown PQC mode", "unknown mode")
			assert.Empty(t, listenerConf(t, l).PQCMode, "a rejected config leaves the listener alone")

			// a listener of another protocol takes a mode and ignores it
			udp := tlsConf("preferred", "allow-any", "dummy")
			udp.Listeners[0].Protocol, udp.Listeners[0].Cert, udp.Listeners[0].Key = "UDP", "", ""
			_, ok = s.Reconcile(udp).(stnrv2.ErrRestarted)
			assert.True(t, ok, "a mode on a UDP listener: restarted for the protocol change")
			assert.Equal(t, stnrv2.ProtocolUDP.String(), listenerConf(t, l).Protocol, "protocol change applied")
		},
	},
}

// start with default config and then reconcile with the given config
func TestStunnerReconcile(t *testing.T) {

	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testReconcileDefault {
		t.Run(c.name, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.name)

			log.Debug("creating a stunnerd")
			conf, err := NewDefaultConfig("turn://user:pass@127.0.0.1:3478")
			assert.NoError(t, err, err)
			conf.Admin.LogLevel = stunnerTestLoglevel
			// the test configs bind their listeners to the loopback and advertise no relay
			// address
			conf.Listeners[0].Addr = "127.0.0.1"
			conf.Clusters[0].Addrs = nil

			log.Debug("creating a stunnerd")
			s := NewStunner(Options{
				DryRun:           true,
				LogOptions:       LogOptions{Level: stunnerTestLoglevel},
				SuppressRollback: true,
			})

			log.Debug("starting stunnerd")
			assert.NoError(t, s.Reconcile(conf), "starting server")

			runningConf := s.GetConfig()
			require.NotNil(t, runningConf, "default stunner get config ok")

			require.True(t, conf.Admin.DeepEqual(&runningConf.Admin),
				"default stunner admin config ok")
			require.True(t, conf.Auth.DeepEqual(&runningConf.Auth),
				"default stunner auth config ok")
			require.NotEmpty(t, conf.Listeners, "default conf listener config")
			require.NotEmpty(t, runningConf.Listeners, "running conf listener config")
			require.True(t, conf.Listeners[0].DeepEqual(
				&runningConf.Listeners[0]), "default stunner listener config ok")

			require.NotEmpty(t, runningConf.Servers, "running conf server config")
			require.True(t, conf.Servers[0].DeepEqual(
				&runningConf.Servers[0]), "default stunner server config ok")
			require.NotEmpty(t, runningConf.Clusters, "running conf cluster config")
			require.True(t, conf.Clusters[0].DeepEqual(
				&runningConf.Clusters[0]), "default stunner cluster config ok")

			require.True(t, conf.DeepEqual(runningConf), "default stunner config ok")

			err = s.Reconcile(&c.config)
			c.tester(t, s, err)

			s.Close()
		})
	}
}

// TestConcurrentReadsDuringReconcile asserts that lockless reads of the atomic snapshots
// (Auth.conf, Listener.conf, Server.conf) and the cluster endpoints stay race-free and
// panic-free while reconciliation concurrently swaps configs and bounces servers and listeners.
func TestConcurrentReadsDuringReconcile(t *testing.T) {
	s := NewStunner(Options{
		DryRun:           true,
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: true,
	})
	require.NotNil(t, s)
	defer s.Close()

	a := makeRaceConfig("realm-a")
	b := makeRaceConfig("realm-b")

	reconcileAllowRestart(t, s, &a)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				cfg := s.GetConfig()
				auth := newAuthHandler(s)
				if auth != nil {
					_, _, _ = auth(&turn.RequestAttributes{
						Username: "user",
						Realm:    cfg.Auth.Realm,
						SrcAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 20000},
					})
				}

				// the cluster endpoints the TURN permission handlers read, while
				// reconciliation swaps them
				if r := s.GetCluster("allow-any"); r != nil {
					_, _, _ = r.Route(netip.MustParseAddrPort("10.0.0.1:1"))
				}
			}
		}()
	}

	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			reconcileAllowRestart(t, s, &a)
		} else {
			reconcileAllowRestart(t, s, &b)
		}
	}

	close(stop)
	wg.Wait()
}

/********************************************
 *
 * E2E reconcile test with a running server
 *
 *********************************************/

type StunnerTestReconcileE2EConfig struct {
	testName                                          string
	config                                            stnrv2.StunnerConfig
	echoServerAddr                                    string
	errContains                                       string
	bindSuccess, allocateSuccess, echoResult, restart bool
	// restarted, when set, lists exactly the objects the reconcile restarts.
	restarted []string
}

func testStunnerReconcileWithVNet(t *testing.T, testcases []StunnerTestReconcileE2EConfig, rollback bool) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	// patch in the vnet
	log.Debug("building virtual network")
	v, err := buildVNet(loggerFactory)
	assert.NoError(t, err, err)

	log.Debug("creating default stunner config")
	conf, err := NewDefaultConfig("turn://user:pass@1.2.3.4:3478?transport=udp")
	assert.NoError(t, err, err)
	// the test configs bind their listeners to the pod address
	conf.Listeners[0].Addr = "1.2.3.4"

	conf.Admin.LogLevel = stunnerTestLoglevel
	conf.Admin.MetricsEndpoint = ""

	log.Debug("setting up the mock DNS")
	mockDns := resolver.NewMockResolver(map[string]([]string){
		"stunner.l7mp.io":     []string{"1.2.3.4"},
		"echo-server.l7mp.io": []string{"1.2.3.5"},
		"dummy.l7mp.io":       []string{"1.2.3.10"},
	}, loggerFactory)

	// should never err
	mockDns.Start()

	log.Debug("creating a stunnerd")
	s := NewStunner(Options{
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: rollback,
		Resolver:         mockDns,
		Net:              v.podnet,
	})

	log.Debug("starting stunnerd")
	assert.NoError(t, s.Reconcile(conf), "starting server")

	for _, c := range testcases {
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			log.Debug("reconciling server")
			err := s.Reconcile(&c.config)
			if c.errContains != "" {
				assert.ErrorContains(t, err, c.errContains, "starting server")
			} else if c.restart {
				assert.ErrorContains(t, err, "restart", "starting server")
				if c.restarted != nil {
					var restarted stnrv2.ErrRestarted
					require.True(t, errors.As(err, &restarted), "restarted status")
					assert.ElementsMatch(t, c.restarted, restarted.Objects, "restarted objects")
				}
			} else {
				assert.NoError(t, err, "no restart")
			}

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			require.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, s, "stunner.l7mp.io:3478",
				lconn, "user", "pass", net.IPv4(5, 6, 7, 8), c.echoServerAddr,
				c.allocateSuccess, c.bindSuccess, c.echoResult, loggerFactory, "", nil}
			stunnerEchoTest(testConfig)

			time.Sleep(100 * time.Millisecond)
			lconn.Close() //nolint:errcheck
		})
	}

	s.Close()
	assert.NoError(t, v.Close(), "cannot close VNet")
}

var testReconcileE2E = []StunnerTestReconcileE2EConfig{
	{
		testName: "empty server with no auth", // STUN-server mode
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "none",
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "default-listener",
				Protocol: "UDP",
				Servers:  []string{"default-server"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "default-server",
				Type:     "turn",
				Clusters: []string{},
			}},
			Clusters: []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: false,
		echoResult:      false,
	},
	{
		testName: "initial E2E reconcile test: empty server",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     false,
		allocateSuccess: false,
		echoResult:      false,
	},
	{
		testName: "adding a listener at the wrong port",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3480,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     false,
		allocateSuccess: true,
		echoResult:      false,
	},
	{
		testName: "adding a cluster to a listener at the wrong port",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3480,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     false,
		allocateSuccess: true,
		echoResult:      false,
	},
	{
		testName: "adding a listener at the right port",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3480,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "changing the port in the wrong listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         true,
		restarted:       []string{"listener: udp"},
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "changing static credentials to a wrong passwd",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "dummy",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: false,
		echoResult:      false,
	},
	{
		testName: "changing auth to ephemeral credentials errs",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "ephemeral",
				Credentials: map[string]string{
					"secret": "dummy",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: false,
		echoResult:      false,
	},
	{
		testName: "reverting good static credentials ok",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Realm: "stunner.l7mp.io",
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "realm reset induces a server restart",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Realm: "dummy",
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr: "1.2.3.5:6678",
		restart:        true,
		// the listeners restart with their servers, only the servers are reported
		restarted:       []string{"server: udp-ok", "server: udp"},
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "reverting the realm induces another server restart",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Realm: "stunner.l7mp.io",
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr: "1.2.3.5:6678",
		restart:        true,
		// the listeners restart with their servers, only the servers are reported
		restarted:       []string{"server: udp-ok", "server: udp"},
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "adding a cluster to the wrong IP",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "removing working cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "dummy-cluster",
				Endpoints: []string{},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      false,
	},
	{
		testName: "reintroducing good cluster to the wrong IP",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}, {
				Name:     "udp",
				Protocol: "UDP",
				Servers:  []string{"udp"},
				Addr:     "1.2.3.4",
				Port:     3479,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}, {
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "removing wrong listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "correct the wrong cluster and remove the good one",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.10"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "removing wrong cluster and reverting the working one",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "removing dangling cluster ref",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "adding port range to cluster ok",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5:6678"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "extensing port range still ok",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Endpoints: []string{"1.2.3.5:1-10000"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "converting cluster to strict dns",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"echo-server-cluster", "dummy-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "echo-server-cluster",
				Type:      "STRICT_DNS",
				Endpoints: []string{"echo-server.l7mp.io"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		testName: "rewiring to an open cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"open-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "open-cluster",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
				Addrs:     []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     true,
		allocateSuccess: true,
		echoResult:      true,
	},
	{
		// a cluster with no endpoints still makes relay sockets but admits no peer
		testName: "closing open cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"open-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:     "open-cluster",
				Protocol: "UDP",
				Addrs:    []string{"1.2.3.4"},
			}},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		allocateSuccess: true,
		bindSuccess:     true,
		echoResult:      false,
	},
	{
		// with no cluster the server has no transport to make a relay socket with
		testName: "closing cluster",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "UDP",
				Servers:  []string{"udp-ok"},
				Addr:     "1.2.3.4",
				Port:     3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp-ok",
				Type:     "turn",
				Clusters: []string{"open-cluster"},
			}},
			Clusters: []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		allocateSuccess: false,
		bindSuccess:     true,
		echoResult:      false,
	},
	{
		testName: "closing listener",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         false,
		bindSuccess:     false,
		allocateSuccess: true,
		echoResult:      false,
	},
	{
		testName: "changing the offload mode induces a restart",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				OffloadEngine: "XDP",
				LogLevel:      stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         true,
		restarted:       []string{"offload: default-offload"},
		bindSuccess:     false,
		allocateSuccess: false,
		echoResult:      false,
	},
	{
		testName: "changing offload interfaces induces a restart",
		config: stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				OffloadEngine:     "XDP",
				OffloadInterfaces: []string{"eth0"},
				LogLevel:          stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []stnrv2.ListenerConfig{},
			Servers:   []stnrv2.ServerConfig{},
			Clusters:  []stnrv2.ClusterConfig{},
		},
		echoServerAddr:  "1.2.3.5:6678",
		restart:         true,
		restarted:       []string{"offload: default-offload"},
		bindSuccess:     false,
		allocateSuccess: false,
		echoResult:      false,
	},
}

func TestStunnerReconcileWithVNetE2E(t *testing.T) {
	testStunnerReconcileWithVNet(t, testReconcileE2E, true)
}

/********************************************
 *
 * reconcile rollback tests: start from a base connfiguration and test through a series of rollback
 * tests
 *
 *********************************************/
var testReconcileRollback = map[string][]StunnerTestReconcileE2EConfig{
	"reconcile protocol": {
		{
			testName: "base config",
			config: stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin: stnrv2.AdminConfig{
					LogLevel: stunnerTestLoglevel,
				},
				Auth: stnrv2.AuthConfig{
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				},
				Listeners: []stnrv2.ListenerConfig{{
					Name:     "default-listener",
					Protocol: "UDP",
					Servers:  []string{"default-server"},
					Addr:     "1.2.3.4",
					Port:     3478,
				}},
				Servers: []stnrv2.ServerConfig{{
					Name:     "default-server",
					Type:     "turn",
					Clusters: []string{"echo-server-cluster"},
				}},
				Clusters: []stnrv2.ClusterConfig{{
					Name:      "echo-server-cluster",
					Endpoints: []string{"1.2.3.5"},
					Protocol:  "UDP",
					Addrs:     []string{"1.2.3.4"},
				}},
			},
			echoServerAddr:  "1.2.3.5:6678",
			restart:         false,
			bindSuccess:     true,
			allocateSuccess: true,
			echoResult:      true,
		},
		{
			// this will trigger an error at a later stage of reconciliation that the
			// validation phase cannot catch and cause a rollback
			testName: "reconcile listener with an invalid TLS cert/key",
			config: stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin: stnrv2.AdminConfig{
					LogLevel: stunnerTestLoglevel,
				},
				Auth: stnrv2.AuthConfig{
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				},
				Listeners: []stnrv2.ListenerConfig{{
					Name:     "default-listener",
					Protocol: "TLS",
					Servers:  []string{"default-server"},
					Addr:     "1.2.3.4",
					Port:     3478,
					Key:      "ZHVtbXkK",
					Cert:     "ZHVtbXkK",
				}},
				Servers: []stnrv2.ServerConfig{{
					Name:     "default-server",
					Type:     "turn",
					Clusters: []string{"echo-server-cluster"},
				}},
				Clusters: []stnrv2.ClusterConfig{{
					Name:      "echo-server-cluster",
					Endpoints: []string{"1.2.3.5"},
					Protocol:  "UDP",
					Addrs:     []string{"1.2.3.4"},
				}},
			},
			echoServerAddr:  "1.2.3.5:6678",
			errContains:     "cannot load cert/key pair",
			restart:         true,
			bindSuccess:     true,
			allocateSuccess: true,
			echoResult:      true,
		},
	},
}

func TestStunnerReconcileWithVNetRollback(t *testing.T) {
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("rollback-test")

	for name, testcase := range testReconcileRollback {
		log.Debugf("-------------- Running new test: %s -------------", name)
		testStunnerReconcileWithVNet(t, testcase, false)
	}
}

func newAuthHandler(s *Stunner) a12n.AuthHandler {
	return objectturn.NewAuthHandler(s.rt, s.log)
}

func callAuthHandler(t *testing.T, h a12n.AuthHandler, ra *turn.RequestAttributes) (string, []byte, bool) {
	t.Helper()
	if !assert.NotNil(t, h, "auth handler exists") {
		return "", nil, false
	}
	return h(ra)
}

func mustAuthType(t *testing.T, auth *object.Auth) stnrv2.AuthType {
	t.Helper()
	conf, ok := auth.GetConfig().(*stnrv2.AuthConfig)
	require.True(t, ok)
	typ, err := stnrv2.NewAuthType(conf.Type)
	require.NoError(t, err)
	return typ
}

func mustClusterType(t *testing.T, r *object.Cluster) stnrv2.ClusterType {
	t.Helper()
	require.NotNil(t, r, "cluster found")
	conf, ok := r.GetConfig().(*stnrv2.ClusterConfig)
	require.True(t, ok)
	typ, err := stnrv2.NewClusterType(conf.Type)
	require.NoError(t, err)
	return typ
}

// clusterEndpoints returns a cluster's reconciled endpoints via its public config.
func clusterEndpoints(t *testing.T, r *object.Cluster) []string {
	t.Helper()
	require.NotNil(t, r, "cluster found")
	conf, ok := r.GetConfig().(*stnrv2.ClusterConfig)
	require.True(t, ok)
	return conf.Endpoints
}

// listenerConf returns a listener's reconciled config via its public snapshot.
func listenerConf(t *testing.T, l *object.Listener) *stnrv2.ListenerConfig {
	t.Helper()
	require.NotNil(t, l, "listener found")
	conf, ok := l.GetConfig().(*stnrv2.ListenerConfig)
	require.True(t, ok)
	return conf
}

// serverConf returns a server's reconciled config via its public snapshot.
func serverConf(t *testing.T, srv *object.Server) *stnrv2.ServerConfig {
	t.Helper()
	require.NotNil(t, srv, "server found")
	conf, ok := srv.GetConfig().(*stnrv2.ServerConfig)
	require.True(t, ok)
	return conf
}

// authCreds returns an auth object's reconciled credentials via its public config.
func authCreds(t *testing.T, a *object.Auth) map[string]string {
	t.Helper()
	conf, ok := a.GetConfig().(*stnrv2.AuthConfig)
	require.True(t, ok)
	return conf.Credentials
}

func mustAdminName(t *testing.T, admin *object.Admin) string {
	t.Helper()
	conf, ok := admin.GetConfig().(*stnrv2.AdminConfig)
	require.True(t, ok)
	return conf.Name
}

func makeRaceConfig(realm string) stnrv2.StunnerConfig {
	return stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
		Auth: stnrv2.AuthConfig{
			Type:  stnrv2.AuthTypeStatic.String(),
			Realm: realm,
			Credentials: map[string]string{
				"username": "user",
				"password": "pass",
			},
		},
		Listeners: []stnrv2.ListenerConfig{{
			Name:     "default-listener",
			Protocol: "UDP",
			Servers:  []string{"default-server"},
			Addr:     "127.0.0.1",
			Port:     3478,
		}},
		Servers: []stnrv2.ServerConfig{{
			Name:     "default-server",
			Type:     "turn",
			Clusters: []string{"allow-any"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:      "allow-any",
			Type:      stnrv2.ClusterTypeStatic.String(),
			Endpoints: []string{"0.0.0.0/0"},
			Protocol:  stnrv2.ProtocolUDP.String(),
			Addrs:     []string{"127.0.0.1"},
		}},
	}
}

func reconcileAllowRestart(t *testing.T, s *Stunner, c *stnrv2.StunnerConfig) {
	t.Helper()
	err := s.Reconcile(c)
	if err == nil {
		return
	}

	var restarted stnrv2.ErrRestarted
	require.True(t, errors.As(err, &restarted), "unexpected reconcile error: %v", err)
}
