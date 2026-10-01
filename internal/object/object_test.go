package object_test

import (
	"net"
	"strconv"
	"testing"

	"github.com/pion/transport/v5/stdnet"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/offload"
	"github.com/l7mp/stunner/v2/internal/quota"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

type inspectExpectation struct {
	name string
	conf stnrv2.Config
	full *stnrv2.StunnerConfig
	want runtime.Action
}

type objectSemanticsCase struct {
	name         string
	setup        func(*testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig)
	expectations []inspectExpectation
}

func runObjectSemanticsCase(t *testing.T, tc objectSemanticsCase) {
	t.Helper()

	obj, base, full := tc.setup(t)

	require.NoError(t, obj.Reconcile(base))
	got := obj.GetConfig()
	require.Truef(t, got.DeepEqual(base), "roundtrip mismatch: got=%s want=%s", got.String(), base.String())

	old := obj.GetConfig()
	for _, exp := range tc.expectations {
		t.Run(exp.name, func(t *testing.T) {
			fullConf := exp.full
			if fullConf == nil {
				fullConf = full
			}
			action, err := obj.Inspect(old, exp.conf, fullConf)
			require.NoError(t, err)
			require.Equal(t, exp.want, action)
		})
	}
}

type testEnv struct {
	rt *runtime.Runtime
}

func newTestEnv() *testEnv {
	log := logger.NewLoggerFactory(stnrv2.DefaultLogLevel)
	r := resolver.NewMockResolver(map[string][]string{}, log)
	rt := runtime.New(runtime.Config{Logger: log, DryRun: true, Resolver: r})
	return &testEnv{rt: rt}
}

// newLiveEnv returns a runtime objects can start in: real sockets, telemetry, the stub quota and
// the given offload engine (the null engine if nil), with the static auth and a default admin
// registered.
func newLiveEnv(t *testing.T, eng offload.Engine) *testEnv {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := runtime.New(runtime.Config{Logger: log, Telemetry: tm, Net: nw, OffloadEngine: eng,
		Resolver: resolver.NewMockResolver(map[string][]string{}, log)})
	rt.QuotaHandler = quota.New(rt)
	env := &testEnv{rt: rt}

	auth, err := object.NewAuth(staticAuthConfig(), rt)
	require.NoError(t, err)
	mustAdd(t, env, auth)
	admin, err := object.NewAdmin(&stnrv2.AdminConfig{}, rt)
	require.NoError(t, err)
	mustAdd(t, env, admin)
	return env
}

// start registers an object under its parent (nil for a top-level object) and starts it; the
// object is closed at the end of the test, before the objects started earlier.
func (env *testEnv) start(t *testing.T, o runtime.Object, parent runtime.Runnable) {
	t.Helper()
	require.NoError(t, env.rt.Registry.Add(o, parent))
	require.NoError(t, o.Start())
	t.Cleanup(func() { _ = o.Close(true) })
}

func freePort(t *testing.T, network string) int {
	t.Helper()
	var addr net.Addr
	if network == "tcp" {
		l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = l.Addr()
		require.NoError(t, l.Close())
	} else {
		c, err := net.ListenPacket("udp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = c.LocalAddr()
		require.NoError(t, c.Close())
	}
	_, port, err := net.SplitHostPort(addr.String())
	require.NoError(t, err)
	p, err := strconv.Atoi(port)
	require.NoError(t, err)
	return p
}

func mustAdd(t *testing.T, env *testEnv, o runtime.Runnable) {
	t.Helper()
	require.NoError(t, env.rt.Registry.Add(o, nil))
}

func staticAuthConfig() *stnrv2.AuthConfig {
	return &stnrv2.AuthConfig{
		Type:  stnrv2.AuthTypeStatic.String(),
		Realm: stnrv2.DefaultRealm,
		Credentials: map[string]string{
			"username": "user",
			"password": "pass",
		},
	}
}

func strPtr(s string) *string { return &s }
