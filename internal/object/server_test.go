package object_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestServerObjectSemantics(t *testing.T) {
	otherRealm := &stnrv2.StunnerConfig{Auth: stnrv2.AuthConfig{
		Type:        stnrv2.AuthTypeStatic.String(),
		Realm:       "example.org",
		Credentials: map[string]string{"username": "user", "password": "pass"},
	}}

	for _, tc := range []struct {
		typ, otherType stnrv2.ServerType
		// realmChange is the action a realm change alone takes: a TURN server bakes the
		// realm into its instance, an L4 server has no realm
		realmChange runtime.Action
	}{
		{typ: stnrv2.ServerTypeTURN, otherType: stnrv2.ServerTypeL4, realmChange: runtime.ActionRestart},
		{typ: stnrv2.ServerTypeL4, otherType: stnrv2.ServerTypeTURN, realmChange: runtime.ActionNone},
	} {
		server := func(typ stnrv2.ServerType, clusters ...string) *stnrv2.ServerConfig {
			c := &stnrv2.ServerConfig{Name: "server-a", Type: typ.String(), Clusters: clusters}
			require.NoError(t, c.Validate())
			return c
		}

		runObjectSemanticsCase(t, objectSemanticsCase{
			name: tc.typ.String(),
			setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
				// the realm is taken at Start, so the server must run
				env := newLiveEnv(t, nil)
				base := server(tc.typ, "cluster-a")
				obj, err := object.NewServer(base, env.rt)
				require.NoError(t, err)
				env.start(t, obj, nil)
				return obj, base, &stnrv2.StunnerConfig{Auth: *staticAuthConfig()}
			},
			expectations: []inspectExpectation{
				{name: "unchanged-none", conf: server(tc.typ, "cluster-a"), want: runtime.ActionNone},
				{name: "clusters-change-reconcile", conf: server(tc.typ, "cluster-a", "cluster-b"), want: runtime.ActionReconcile},
				{name: "type-change-restart", conf: server(tc.otherType, "cluster-a"), want: runtime.ActionRestart},
				{name: "realm-change", conf: server(tc.typ, "cluster-a"), full: otherRealm, want: tc.realmChange},
			},
		})
	}
}

// TestServerTypeChange pins that a type change rebuilds the server as the new type: the object
// keeps its name and config but runs a server of the new kind.
func TestServerTypeChange(t *testing.T) {
	env := newLiveEnv(t, nil)
	obj, err := object.NewServer(&stnrv2.ServerConfig{Name: "server-a",
		Type: stnrv2.ServerTypeTURN.String()}, env.rt)
	require.NoError(t, err)
	env.start(t, obj, nil)

	// a type change is a restart: close, reconcile, start
	require.NoError(t, obj.Close(false))
	l4 := &stnrv2.ServerConfig{Name: "server-a", Type: stnrv2.ServerTypeL4.String()}
	require.NoError(t, obj.Reconcile(l4))
	require.NoError(t, obj.Start())
	assert.True(t, obj.GetConfig().DeepEqual(l4), "config round trip")
	status := obj.Status().(*stnrv2.ServerStatus)
	assert.Equal(t, stnrv2.ServerTypeL4.String(), status.Type)
	assert.Zero(t, status.Sessions)

	// renaming is not a reconcile
	require.Error(t, obj.Reconcile(&stnrv2.ServerConfig{Name: "server-b"}))
}

// TestServerServesByName pins that a server object is found by name through the runtime and
// takes conns only while it runs.
func TestServerServesByName(t *testing.T) {
	env := newLiveEnv(t, nil)
	obj, err := object.NewServer(&stnrv2.ServerConfig{Name: "server-a",
		Type: stnrv2.ServerTypeL4.String()}, env.rt)
	require.NoError(t, err)
	require.NoError(t, env.rt.Registry.Add(obj, nil))

	s, ok := env.rt.Server("server-a")
	require.True(t, ok, "found by name")
	client, _ := net.Pipe()
	conn := netconn.NewConn(client, api.Tag{Name: "listener-a", Proto: stnrv2.ProtocolUDP}, nil)
	_, _, err = s.Serve(conn)
	assert.ErrorIs(t, err, server.ErrNotServing, "a stopped server refuses")

	require.NoError(t, obj.Start())
	t.Cleanup(func() { _ = obj.Close(true) })
	_, verdict, err := s.Serve(conn)
	assert.NoError(t, err, "a running server takes the conn")
	assert.Equal(t, api.Consumed, verdict, "an l4 server consumes the conn")

	_, ok = env.rt.Server("no-such-server")
	assert.False(t, ok)
}
