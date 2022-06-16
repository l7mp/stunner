package stunner

import (
	"net"
	// "fmt"
	"strconv"
	"testing"
	"time"

	"github.com/pion/transport/test"
	"github.com/pion/turn/v2"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// *****************
// Reconciliation tests
// *****************
type StunnerReconcileTestConfig struct {
	name   string
	config v1alpha1.StunnerConfig
	tester func(t *testing.T, s *Stunner, err error)
}

var testReconcileDefault = []StunnerReconcileTestConfig{
	{
		name: "reconcile-test: default admin",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
			// make sure we get the right loglevel, we may override this for debugging the tests
			// assert.Equal(t, admin.LogLevel, v1alpha1.DefaultLogLevel, "stunner loglevel")

			assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

			assert.Equal(t, auth.Username, "user", "username ok")
			assert.Equal(t, auth.Password, "pass", "password ok")

			key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, key, turn.GenerateAuthKey("user",
				v1alpha1.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing

			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	{
		name: "reconcile-test: empty credentials errs: user",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "empty username or password")
		},
	},
	{
		name: "reconcile-test: empty credentials errs: passwd",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "empty username or password")
		},
	},
	{
		name: "reconcile-test: empty listener is fine",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// deleting a listener requires a restart
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")
		},
	},
	{
		name: "reconcile-test: empty listener name errs",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "missing name")
		},
	},
	{
		name: "reconcile-test: empty cluster is fine",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")
		},
	},
	{
		name: "reconcile-test: empty cluster name errs",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Endpoints: []string{"0.0.0.0/0"},
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
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				Name: "new-name",
			},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			// check everyting
			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, "new-name", "stunner name")
			// assert.Equal(t, admin.LogLevel, v1alpha1.DefaultLogLevel, "stunner loglevel")

			assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

			assert.Equal(t, auth.Username, "user", "username ok")
			assert.Equal(t, auth.Password, "pass", "password ok")

			key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, key, turn.GenerateAuthKey("user",
				v1alpha1.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	{
		name: "reconcile-test: reconcile loglevel",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: "anything",
			},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, "default-stunnerd", "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

			assert.Equal(t, auth.Username, "user", "username ok")
			assert.Equal(t, auth.Password, "pass", "password ok")

			key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, key, turn.GenerateAuthKey("user",
				v1alpha1.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	/// auth
	{
		name: "reconcile-test: reconcile plaintextauth name",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: "anything",
			},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "newuser",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

			assert.Equal(t, auth.Username, "newuser", "username ok")
			assert.Equal(t, auth.Password, "pass", "password ok")

			key, ok := auth.Handler("newuser", v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, key, turn.GenerateAuthKey("newuser",
				v1alpha1.DefaultRealm, "pass"), "auth handler ok")

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	{
		name: "reconcile-test: reconcile plaintext auth passwd",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: "anything",
			},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "newpass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

			assert.Equal(t, auth.Username, "user", "username ok")
			assert.Equal(t, auth.Password, "newpass", "password ok")

			key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")
			assert.Equal(t, key, turn.GenerateAuthKey("user",
				v1alpha1.DefaultRealm, "newpass"), "auth handler ok")

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	{
		name: "reconcile-test: reconcile longterm auth",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: "anything",
			},
			Auth: v1alpha1.AuthConfig{
				Type: "longterm",
				Credentials: map[string]string{
					"secret": "newsecret",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// no restart!
			assert.NoError(t, err, "no restart needed")

			auth := s.GetAuth()
			assert.Equal(t, auth.Type, v1alpha1.AuthTypeLongTerm, "auth type ok")
			assert.Equal(t, auth.Secret, "newsecret")

			logger := NewLoggerFactory(stunnerTestLoglevel)
			handler := turn.NewLongTermAuthHandler("newsecret", logger.NewLogger("test-auth"))
			duration, _ := time.ParseDuration("10h")
			d := time.Now().Add(duration).Unix()
			username := strconv.FormatInt(d, 10)

			key, ok := auth.Handler(username, v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok, "authHandler key ok")

			key2, ok2 := handler(username, v1alpha1.DefaultRealm,
				&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
			assert.True(t, ok2)
			assert.Equal(t, key, key2)

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener  uses the open cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	/// listener
	{
		name: "reconcile-test: reconcile existing listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:         "default-listener",
				Protocol:     "tcp",
				Addr:         "127.0.0.2",
				Port:         12345,
				MinRelayPort: 10,
				MaxRelayPort: 100,
				Routes:       []string{"none", "dummy"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTCP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
			assert.Equal(t, l.Port, 12345, "listener port ok")
			assert.Equal(t, l.MinPort, 10, "listener minport ok")
			assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
			assert.Len(t, l.Routes, 2, "listener route count ok")
			// sorted!!!
			assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
			assert.Equal(t, l.Routes[1], "none", "listener route name ok")

			assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
			admin := s.GetAdmin()
			assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
			// assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener uses the old cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
	{
		name: "reconcile-test: reconcile new listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:         "newlistener",
				Protocol:     "tcp",
				Addr:         "127.0.0.2",
				Port:         1,
				MinRelayPort: 10,
				MaxRelayPort: 100,
				Routes:       []string{"none", "dummy"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

			assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.Nil(t, l, "listener found")

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTCP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
			assert.Equal(t, l.Port, 1, "listener port ok")
			assert.Equal(t, l.MinPort, 10, "listener minport ok")
			assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
			assert.Len(t, l.Routes, 2, "listener route count ok")
			// sorted!
			assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
			assert.Equal(t, l.Routes[1], "none", "listener route name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener uses the old cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
	{
		name: "reconcile-test: reconcile additional listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}, {
				Name:         "newlistener",
				Protocol:     "tcp",
				Addr:         "127.0.0.2",
				Port:         1,
				MinRelayPort: 10,
				MaxRelayPort: 100,
				Routes:       []string{"none", "dummy"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

			assert.Len(t, s.listenerManager.Keys(), 2, "listenerManager keys")

			l := s.GetListener("default-listener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")
			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUDP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
			assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
			assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
			assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
			assert.Len(t, l.Routes, 1, "listener route count ok")
			assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			// listener uses the old cluster for routing
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")

			l = s.GetListener("newlistener")
			assert.NotNil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTCP, "listener proto ok")
			assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
			assert.Equal(t, l.Port, 1, "listener port ok")
			assert.Equal(t, l.MinPort, 10, "listener minport ok")
			assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
			assert.Len(t, l.Routes, 2, "listener route count ok")
			// sorted!
			assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
			assert.Equal(t, l.Routes[1], "none", "listener route name ok")

			p = s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")

		},
	},
	{
		name: "reconcile-test: reconcile deleted listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

			l := s.GetListener("default-listener")
			assert.Nil(t, l, "listener found")

			l = s.GetListener("newlistener")
			assert.Nil(t, l, "listener found")
			assert.IsType(t, l, &object.Listener{}, "listener type ok")

			assert.Len(t, s.listenerManager.Keys(), 0, "listenerManager keys")
		},
	},
	/// cluster
	{
		name: "reconcile-test: reconcile existing cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("1.1.1.1/32")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
			_, n, _ = net.ParseCIDR("2.2.2.2/8")
			assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

			l := s.GetListener("default-listener")
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")

			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
	{
		name: "reconcile-test: reconcile new cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.Nil(t, c, "cluster found")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("1.1.1.1/32")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
			_, n, _ = net.ParseCIDR("2.2.2.2/8")
			assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

			l := s.GetListener("default-listener")
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")

			// listener still uses the old cluster for routing
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
	{
		name: "reconcile-test: reconcile additional cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
			}, {
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.clusterManager.Keys(), 2, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			l := s.GetListener("default-listener")
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
			_, n, _ = net.ParseCIDR("1.1.1.1/32")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
			_, n, _ = net.ParseCIDR("2.2.2.2/8")
			assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

			// listener still uses the old open cluster for routing
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
		},
	},
	{
		name: "reconcile-test: reconcile additional cluster and reroute",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"newcluster"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "newcluster",
				Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
			}, {
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			// only routes have changed, we shouldn't need a restart
			assert.NoError(t, err, err)

			assert.Len(t, s.clusterManager.Keys(), 2, "clusterManager keys")

			c := s.GetCluster("allow-any")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
			_, n, _ := net.ParseCIDR("0.0.0.0/0")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

			l := s.GetListener("default-listener")
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")

			c = s.GetCluster("newcluster")
			assert.NotNil(t, c, "cluster found")
			assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
			assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
			assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
			_, n, _ = net.ParseCIDR("1.1.1.1/32")
			assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
			_, n, _ = net.ParseCIDR("2.2.2.2/8")
			assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
			assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
			assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
	{
		name: "reconcile-test: reconcile deleted cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:   "default-listener",
				Addr:   "127.0.0.1",
				Routes: []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{},
		},
		tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

			assert.Len(t, s.clusterManager.Keys(), 0, "clusterManager keys")

			l := s.GetListener("default-listener")
			p := s.NewPermissionHandler(l)
			assert.NotNil(t, p, "permission handler exists")

			// missing cluster, deny all IPs
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
			assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
				net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
		},
	},
}

// start with default config and then reconcile with the given config
func TestStunnerReconcile(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testReconcileDefault {
		t.Run(c.name, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.name)

			log.Debug("creating a stunnerd")
			conf, err := NewDefaultConfig("turn://user:pass@127.0.0.1:3478")
			assert.NoError(t, err, err)

			conf.Admin.LogLevel = stunnerTestLoglevel

			s, err := NewStunner(*conf)
			assert.NoError(t, err, err)

			runningConf := s.GetConfig()
			assert.NotNil(t, runningConf, "default stunner get config ok")

			// fmt.Printf("default conf: %#v\n", conf.Clusters[0])
			// fmt.Printf("running conf: %#v\n", runningConf.Clusters[0])
			// x := reflect.DeepEqual(conf.Clusters[0], runningConf.Clusters[0])
			// fmt.Printf("deepeq: %#v\n", x)
			// x = conf.Clusters[0].DeepEqual(&runningConf.Clusters[0])
			// fmt.Printf("deepeqqqqqqq: %#v\n", x)

			assert.True(t, conf.Admin.DeepEqual(&runningConf.Admin),
				"default stunner admin config ok")
			assert.True(t, conf.Auth.DeepEqual(&runningConf.Auth),
				"default stunner auth config ok")
			assert.True(t, conf.Listeners[0].DeepEqual(
				&runningConf.Listeners[0]), "default stunner listener config ok")
			assert.True(t, conf.Clusters[0].DeepEqual(
				&runningConf.Clusters[0]), "default stunner cluster config ok")

			assert.True(t, conf.DeepEqual(runningConf), "default stunner config ok")

			err = s.Reconcile(c.config)
			c.tester(t, s, err)

			s.Close()
		})
	}
}

// e2e reconcile test with a running server
type StunnerTestReconcileE2EConfig struct {
	testName                         string
	config                           v1alpha1.StunnerConfig
	echoServerAddr                   string
	bindSuccess, echoResult, restart bool
}

var testReconcileE2E = []StunnerTestReconcileE2EConfig{
	{
		testName: "initial E2E reconcile test: empty server",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{},
			Clusters:  []v1alpha1.ClusterConfig{},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        true,
		bindSuccess:    false,
		echoResult:     false,
	},
	{
		testName: "adding a listener at the wrong port",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        true,
		bindSuccess:    false,
		echoResult:     false,
	},
	{
		testName: "adding a cluster to a listener at the wrong port",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    false,
		echoResult:     false,
	},
	{
		testName: "adding a listener at the right port",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}, {
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        true,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "adding a cluster to the wrong IP",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}, {
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "removing working cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}, {
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "dummy-cluster",
				Endpoints: []string{},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     false,
	},
	{
		testName: "reintroducing good cluster to the wrong IP",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}, {
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3479,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "removing wrong listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}, {
				Name:      "dummy-cluster",
				Endpoints: []string{},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        true,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "correct the wrong cluster and remove the good one",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.10",
				},
			}, {
				Name: "dummy-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "removing wrong cluster and reverting the working one",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "removing dangling cluster ref",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "converting cluster to strict dns",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"echo-server.l7mp.io",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "rewiring to an open cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"open-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name: "open-cluster",
				Endpoints: []string{
					"0.0.0.0/0",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     true,
	},
	{
		testName: "closing open cluster",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:     "udp-ok",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"open-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        false,
		bindSuccess:    true,
		echoResult:     false,
	},
	{
		testName: "closing listener",
		config: v1alpha1.StunnerConfig{
			ApiVersion: "v1alpha1",
			Admin:      v1alpha1.AdminConfig{},
			Auth: v1alpha1.AuthConfig{
				Credentials: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{},
			Clusters:  []v1alpha1.ClusterConfig{},
		},
		echoServerAddr: "1.2.3.5:5678",
		restart:        true,
		bindSuccess:    false,
		echoResult:     false,
	},
}

func TestStunnerReconcileE2EWithVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	// patch in the vnet
	log.Debug("building virtual network")
	v, err := buildVNet(loggerFactory)
	assert.NoError(t, err, err)

	log.Debug("creating default stunner config")
	conf, err := NewDefaultConfig("turn://user:pass@1.2.3.4:3478?transport=udp")
	assert.NoError(t, err, err)

	conf.Admin.LogLevel = stunnerTestLoglevel

	// patch in the vnet
	conf.Net = v.podnet

	s, err := NewStunner(*conf)
	assert.NoError(t, err, err)

	log.Debug("setting up the mock DNS")
	mockDns := &resolver.MockResolver{
		Zone: map[string]([]string){
			"stunner.l7mp.io":     []string{"1.2.3.4"},
			"echo-server.l7mp.io": []string{"1.2.3.5"},
			"dummy.l7mp.io":       []string{"1.2.3.10"},
		}}
	s.resolver.SetResolver(mockDns)

	log.Debug("starting stunnerd")
	assert.NoError(t, s.Start())

	for _, c := range testReconcileE2E {
		c.config.Net = v.podnet
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			log.Debug("reconciling server")
			err := s.Reconcile(c.config)

			if err == v1alpha1.ErrRestartRequired {
				log.Debug("restarting server")

				assert.True(t, c.restart, "restart required ok")
				s.Close()
				err := s.Start()
				assert.NoError(t, err, "restart STUNner ok")
			} else {
				assert.NoError(t, err, "cannot reconcile")
				assert.False(t, c.restart, "restart not required ok")
			}

			// make sure new clusters use the mockDns
			s.resolver.SetResolver(mockDns)

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, s, "stunner.l7mp.io:3478",
				lconn, "user", "pass", net.IPv4(5, 6, 7, 8), c.echoServerAddr,
				c.bindSuccess, c.echoResult, loggerFactory}
			stunnerEchoTest(testConfig)

			time.Sleep(100 * time.Millisecond)
			lconn.Close()
		})
	}

	s.Close()
	assert.NoError(t, v.Close(), "cannot close VNet")
}
