package stunner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/transport/v5/test"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/offload"
	sipsrv "github.com/l7mp/stunner/v2/internal/server/sip"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/mediactl"
	"github.com/l7mp/stunner/v2/pkg/mediactl/sip"
)

/********************************************
 *
 *  SIP gateway: a stateful SIP proxy and an RTP media proxy over vnet
 *
 *  Topology:
 *     trunk 203.0.113.10 ---+                 +--- "livekit-sip" phone 1.2.3.5:5060
 *                           |--- gw router ---|
 *     stranger 198.51.100.66+                 +--- stunnerd 1.2.3.4: SIP 5060, media 30000
 *
 *  Inbound only: the carrier starts every dialog, livekit-sip answers. The sip server programs
 *  the rtp server over ng: its rtp-ng cluster opens the fifo "rtp-ng", which the rtp server's
 *  listener labeled "role: control" makes. The carriers filter admits the trunk's network on
 *  the SIP and media listeners, the pods filter the pod network on the media cluster.
 *
 *********************************************/

const (
	sipStunnerIP = "1.2.3.4"
	sipPeerIP    = "1.2.3.5"
	sipTrunkIP   = "203.0.113.10"
	sipStranger  = "198.51.100.66"
)

type sipNet struct {
	router               *vnet.Router
	pod, trunk, stranger *vnet.Net
}

func newSIPNet(t *testing.T) *sipNet {
	t.Helper()
	lf := logger.NewLoggerFactory(stunnerTestLoglevel)
	r, err := vnet.NewRouter(&vnet.RouterConfig{Name: "gw", CIDR: "0.0.0.0/0", LoggerFactory: lf})
	require.NoError(t, err)
	n := &sipNet{router: r}
	for _, x := range []struct {
		net **vnet.Net
		ip  string
	}{{&n.pod, sipStunnerIP}, {&n.trunk, sipTrunkIP}, {&n.stranger, sipStranger}} {
		ips := []string{x.ip}
		if x.ip == sipStunnerIP {
			ips = append(ips, sipPeerIP)
		}
		*x.net, err = vnet.NewNet(&vnet.NetConfig{StaticIPs: ips})
		require.NoError(t, err)
		require.NoError(t, r.AddNet(*x.net))
	}
	require.NoError(t, r.Start())
	t.Cleanup(func() { _ = r.Stop() })
	return n
}

func sipGatewayConfig() *stnrv2.StunnerConfig {
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{Name: "sip-gateway", LogLevel: stunnerTestLoglevel},
		Auths:      []stnrv2.AuthConfig{{Type: "none"}},
		Listeners: []stnrv2.ListenerConfig{
			{Name: "sip", Protocol: "UDP", Addr: sipStunnerIP, Port: 5060, Servers: []string{"sip"},
				Filters: []string{"carriers"}},
			{Name: "media", Protocol: "UDP", Addr: sipStunnerIP, Port: 30000, Servers: []string{"rtp"},
				Filters: []string{"carriers"}},
			{Name: "rtp-ng", Protocol: "FIFO", Addr: "rtp-ng", Servers: []string{"rtp"},
				Labels: map[string]string{stnrv2.RoleLabel: stnrv2.RoleControl}},
		},
		Servers: []stnrv2.ServerConfig{
			{ServerSpec: stnrv2.ServerSpec{Name: "sip", Type: "SIP", Clusters: []string{"livekit-sip"}},
				SIP: &stnrv2.SIPServerConfig{PublicAddr: sipStunnerIP, PublicPort: 5060, MediaProxy: "rtp-ng",
					LBPolicy: "ROUND_ROBIN"}},
			{ServerSpec: stnrv2.ServerSpec{Name: "rtp", Type: "RTP", Clusters: []string{"livekit-media"}},
				RTP: &stnrv2.RTPServerConfig{PublicAddr: sipStunnerIP, PublicPort: 30000}},
		},
		Clusters: []stnrv2.ClusterConfig{
			{Name: "livekit-sip", Protocol: "UDP", Endpoints: []string{sipPeerIP + ":5060"}},
			{Name: "livekit-media", Protocol: "UDP", Addrs: []string{sipStunnerIP},
				Filters: []string{"pods"}},
			{Name: "rtp-ng", Protocol: "FIFO", Addrs: []string{"rtp-ng"}},
		},
		Filters: []stnrv2.FilterConfig{
			{Name: "carriers", Rules: []stnrv2.FilterRule{
				{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "203.0.113.0/24"}},
				{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "0.0.0.0/0"}},
			}},
			{Name: "pods", Rules: []stnrv2.FilterRule{
				{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.0/24"}},
				{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "0.0.0.0/0"}},
			}},
		},
	}
}

// sipFixture is stunnerd as the SIP gateway, a trunk phone and an in-cluster phone.
type sipFixture struct {
	net     *sipNet
	stunner *Stunner
	trunk   *sip.Phone
	peer    *sip.Phone
}

func newSIPFixture(t *testing.T, peerCfg sip.Config) *sipFixture {
	t.Helper()
	n := newSIPNet(t)
	s := NewStunner(Options{Name: "sip-gateway", Net: n.pod,
		LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	t.Cleanup(func() { closeStunner(s) })
	require.NoError(t, s.Reconcile(sipGatewayConfig()))

	lf := logger.NewLoggerFactory(stunnerTestLoglevel)
	trunk, err := sip.New(sip.Config{Net: n.trunk, LoggerFactory: lf})
	require.NoError(t, err)
	t.Cleanup(func() { _ = trunk.Close() })

	peerCfg.Net, peerCfg.Addr, peerCfg.LoggerFactory = n.pod, sipPeerIP+":5060", lf
	peer, err := sip.New(peerCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	return &sipFixture{net: n, stunner: s, trunk: trunk, peer: peer}
}

// sipMedia is the media of a dialed call: 20 ms frames of G.711.
var sipMedia = mediactl.Config{Length: 160, Rate: 50}

// answered waits for the call the peer answered, and echoes its media.
func answered(t *testing.T, ctx context.Context, p *sip.Phone) *sip.Call {
	t.Helper()
	select {
	case c := <-p.Incoming():
		go mediactl.Echo(c, true)
		return c
	case <-ctx.Done():
		t.Fatal("the call was never answered")
	}
	return nil
}

// assertEcho sends the media of a dialed call, until the call ends, and checks that it comes back
// through the gateway, RTCP included.
func assertEcho(t *testing.T, call *sip.Call, packets int) {
	t.Helper()
	go mediactl.Stream(context.Background(), call, sipMedia)
	require.Eventually(t, func() bool {
		s := call.Stats()
		return s.Received >= packets && s.RTCPReceived > 0
	}, 10*time.Second, 50*time.Millisecond, "the media comes back with RTCP: %s", call.Stats())
	assert.Zero(t, call.Stats().Lost, "no loss: %s", call.Stats())
}

// mediaDeleted checks that the rtp server holds no call.
func mediaDeleted(t *testing.T, s *Stunner) {
	t.Helper()
	assert.Eventually(t, func() bool { return s.GetServer("rtp").(api.Server).Sessions() == 0 },
		5*time.Second, 50*time.Millisecond, "the media of the call is deleted")
}

func TestSIPGateway(t *testing.T) {
	lim := test.TimeOut(120 * time.Second)
	defer lim.Stop()
	uri := "sip:+15551234@" + sipStunnerIP + ":5060"

	t.Run("inbound-call", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		call, err := f.trunk.Dial(ctx, uri)
		require.NoError(t, err)
		in := answered(t, ctx, f.peer)

		// the trunk talks to the public media address, the peer to stunnerd's pod address
		assert.Equal(t, sipStunnerIP+":30000", call.RemoteMedia().String())
		assert.Equal(t, sipStunnerIP, in.RemoteMedia().Addr().String())
		assert.NotEqual(t, 30000, int(in.RemoteMedia().Port()))
		assertEcho(t, call, 25)
		assert.Equal(t, 1, f.stunner.GetServer("rtp").(api.Server).Sessions())
		assert.Equal(t, 1, f.stunner.GetServer("sip").(api.Server).Sessions(), "the established call")

		require.NoError(t, call.Hangup(ctx))
		select {
		case <-in.Done():
			assert.Equal(t, "remote hangup", in.EndReason())
		case <-ctx.Done():
			t.Fatal("the BYE never reached the peer")
		}
		mediaDeleted(t, f.stunner)
		assert.Eventually(t, func() bool {
			return f.stunner.GetServer("sip").(api.Server).Sessions() == 0
		}, 5*time.Second, 50*time.Millisecond, "the call is gone")
	})

	t.Run("media-offload", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		rec := &recordingOffload{NullEngine: offload.NewNullEngine()}
		f.stunner.rt.OffloadEngine = rec
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		call, err := f.trunk.Dial(ctx, uri)
		require.NoError(t, err)
		in := answered(t, ctx, f.peer)
		assertEcho(t, call, 10)
		// the trunk's RTP source with the peer's RTP endpoint, both raw
		require.Eventually(t, func() bool { return len(rec.flows()) == 1 }, 5*time.Second,
			50*time.Millisecond, "the media is offloaded")
		pair := rec.flows()[0]
		port := func(a net.Addr) int { return a.(*net.UDPAddr).Port }
		assert.Equal(t, port(call.LocalAddr()), port(pair[0].RemoteAddr), "the trunk side")
		assert.Equal(t, port(in.LocalAddr()), port(pair[1].RemoteAddr), "the peer side")
		assert.Zero(t, pair[0].ChannelID+pair[1].ChannelID, "raw both ways")

		require.NoError(t, call.Hangup(ctx))
		<-in.Done()
		mediaDeleted(t, f.stunner)
		assert.Eventually(t, func() bool { return len(rec.flows()) == 0 }, 5*time.Second,
			50*time.Millisecond, "the call end removes the offload")
	})

	t.Run("bye-from-the-cluster", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		call, err := f.trunk.Dial(ctx, uri)
		require.NoError(t, err)
		in := answered(t, ctx, f.peer)
		assertEcho(t, call, 10)
		require.NoError(t, in.Hangup(ctx))
		select {
		case <-call.Done():
			assert.Equal(t, "remote hangup", call.EndReason())
		case <-ctx.Done():
			t.Fatal("the BYE never reached the trunk")
		}
		mediaDeleted(t, f.stunner)
	})

	t.Run("cancel", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{AnswerDelay: time.Hour})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dctx, dcancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, err := f.trunk.Dial(dctx, uri)
		dcancel()
		var se *sip.StatusError
		require.True(t, errors.As(err, &se), "the cancelled call gets a final response: %v", err)
		assert.Equal(t, 487, se.Code)
		mediaDeleted(t, f.stunner)
	})

	t.Run("inbound-only", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// the cluster cannot start a call: the carriers filter of the SIP listener drops it
		dctx, dcancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := f.peer.Dial(dctx, uri)
		dcancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded, "nothing outbound")
	})

	t.Run("listener-filter", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stranger, err := sip.New(sip.Config{Net: f.net.stranger})
		require.NoError(t, err)
		defer stranger.Close()

		// only the carriers reach the SIP listener: the stranger gets no answer
		conf := sipGatewayConfig()
		dctx, dcancel := context.WithTimeout(ctx, 2*time.Second)
		_, err = stranger.Dial(dctx, uri)
		dcancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded, "the filter drops the stranger")
		call, err := f.trunk.Dial(ctx, uri)
		require.NoError(t, err, "the trunk passes the filter")
		require.NoError(t, call.Hangup(ctx))

		// the rules change in place: an exception before the ALLOW decides
		conf.Filters[0].Rules = append([]stnrv2.FilterRule{{Action: "DENY",
			IPBlock: &stnrv2.IPBlock{CIDR: sipTrunkIP}}}, conf.Filters[0].Rules...)
		require.NoError(t, f.stunner.Reconcile(conf))
		dctx, dcancel = context.WithTimeout(ctx, 2*time.Second)
		_, err = f.trunk.Dial(dctx, uri)
		dcancel()
		assert.ErrorIs(t, err, context.DeadlineExceeded, "the exception drops the trunk")

		// a missing filter has no rules: the stranger gets through
		conf.Filters = nil
		require.NoError(t, f.stunner.Reconcile(conf))
		call, err = stranger.Dial(ctx, uri)
		require.NoError(t, err, "a missing filter admits")
		require.NoError(t, call.Hangup(ctx))
	})

	t.Run("call-quota", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// the quota caps the parallel calls of the media proxy
		conf := sipGatewayConfig()
		conf.Admin.UserQuota = 1
		require.NoError(t, f.stunner.Reconcile(conf))

		call, err := f.trunk.Dial(ctx, uri)
		require.NoError(t, err)
		in := answered(t, ctx, f.peer)
		var se *sip.StatusError
		_, err = f.trunk.Dial(ctx, uri)
		require.ErrorAs(t, err, &se, "a call over the quota is refused")
		assert.Equal(t, 503, se.Code)

		require.NoError(t, call.Hangup(ctx))
		<-in.Done()
		require.Eventually(t, func() bool {
			call, err := f.trunk.Dial(ctx, uri)
			if err != nil {
				return false
			}
			_ = call.Hangup(ctx)
			return true
		}, 5*time.Second, 100*time.Millisecond, "a call fits again after the hangup")
	})

	t.Run("digest-auth", func(t *testing.T) {
		f := newSIPFixture(t, sip.Config{})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// a carrier the filter admits must also answer the digest challenge of the server's auth
		// policy
		conf := sipGatewayConfig()
		conf.Auths = append(conf.Auths, stnrv2.AuthConfig{Name: "carriers", Type: "static",
			Realm: "sip.example.com", Credentials: map[string]string{"username": "carrier", "password": "secret"}})
		conf.Servers[0].SIP.Auth = "carriers"
		require.NoError(t, f.stunner.Reconcile(conf))

		var se *sip.StatusError
		_, err := f.trunk.Dial(ctx, uri)
		require.ErrorAs(t, err, &se, "a carrier without credentials is challenged")
		assert.Equal(t, 407, se.Code)

		lf := logger.NewLoggerFactory(stunnerTestLoglevel)
		carrier, err := sip.New(sip.Config{Net: f.net.trunk, Addr: sipTrunkIP + ":5070",
			LoggerFactory: lf, Username: "carrier", Password: "secret"})
		require.NoError(t, err)
		defer carrier.Close()
		call, err := carrier.Dial(ctx, uri)
		require.NoError(t, err)
		in := answered(t, ctx, f.peer)
		assertEcho(t, call, 10)
		require.NoError(t, call.Hangup(ctx))
		<-in.Done()

		impostor, err := sip.New(sip.Config{Net: f.net.trunk, Addr: sipTrunkIP + ":5080",
			LoggerFactory: lf, Username: "carrier", Password: "guess"})
		require.NoError(t, err)
		defer impostor.Close()
		_, err = impostor.Dial(ctx, uri)
		require.ErrorAs(t, err, &se, "a wrong password is refused")
		assert.Equal(t, 407, se.Code)

		// a server naming a missing auth policy refuses every call instead of admitting it
		conf.Servers[0].SIP.Auth = "no-such-policy"
		require.NoError(t, f.stunner.Reconcile(conf))
		_, err = carrier.Dial(ctx, uri)
		require.ErrorAs(t, err, &se, "a missing auth policy refuses")
		assert.Equal(t, 403, se.Code)
	})
}

// TestSIPGatewayLoad runs concurrent calls through the gateway, each against an echo.
func TestSIPGatewayLoad(t *testing.T) {
	lim := test.TimeOut(120 * time.Second)
	defer lim.Stop()

	const calls = 50
	f := newSIPFixture(t, sip.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	go func() { _ = mediactl.Serve(f.peer, true, nil) }()
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			call, err := f.trunk.Dial(ctx, fmt.Sprintf("sip:+1555%04d@%s:5060", i, sipStunnerIP))
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", i, err)
				return
			}
			go mediactl.Stream(ctx, call, sipMedia)
			deadline := time.Now().Add(20 * time.Second)
			for call.Stats().Received < 100 && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if s := call.Stats(); s.Received < 100 || s.Lost > 0 {
				errs <- fmt.Errorf("call %d: %s", i, s)
			}
			if err := call.Hangup(ctx); err != nil {
				errs <- fmt.Errorf("call %d hangup: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	mediaDeleted(t, f.stunner)
}

// TestSIPRegistration registers with a carrier over a flow stunnerd dials, and takes the
// carrier's call over that flow: the carrier is a registrar phone on the trunk network, stunnerd
// dials it from the "carrier" listener, whose label names the registration of the SIP server.
func TestSIPRegistration(t *testing.T) {
	lim := test.TimeOut(60 * time.Second)
	defer lim.Stop()
	defer func(k [2]time.Duration) { sipsrv.KeepaliveInterval = k }(sipsrv.KeepaliveInterval)
	sipsrv.KeepaliveInterval = [2]time.Duration{50 * time.Millisecond, 100 * time.Millisecond}

	n := newSIPNet(t)
	lf := logger.NewLoggerFactory(stunnerTestLoglevel)
	carrier, err := sip.New(sip.Config{Net: n.trunk, Addr: sipTrunkIP + ":5060",
		Registrar: true, LoggerFactory: lf})
	require.NoError(t, err)
	t.Cleanup(func() { _ = carrier.Close() })
	peer, err := sip.New(sip.Config{Net: n.pod, Addr: sipPeerIP + ":5060", LoggerFactory: lf})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })

	const aor = "sip:+3612345678@" + sipTrunkIP
	conf := sipGatewayConfig()
	conf.Listeners = append(conf.Listeners, stnrv2.ListenerConfig{Name: "carrier", Protocol: "UDP",
		Connect: &stnrv2.ListenerConnectConfig{Address: sipTrunkIP + ":5060"}, Servers: []string{"sip"},
		Labels: map[string]string{stnrv2.RegistrationLabel: "carrier"}})
	conf.Servers[0].SIP.Registrations = []stnrv2.SIPRegistrationConfig{
		{Name: "carrier", AOR: aor, Expires: "2s"}}
	s := NewStunner(Options{Name: "sip-gateway", Net: n.pod,
		LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	t.Cleanup(func() { closeStunner(s) })
	require.NoError(t, s.Reconcile(conf))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	registered := func() sip.Registration {
		t.Helper()
		select {
		case r := <-carrier.Registered():
			return r
		case <-ctx.Done():
			t.Fatal("no REGISTER reached the carrier")
		}
		return sip.Registration{}
	}

	// the REGISTER binds the AOR to the flow: the Contact is the flow's own address, marked outbound
	r := registered()
	assert.Equal(t, aor, r.AOR.String())
	assert.Equal(t, "+3612345678", r.Contact.User)
	assert.True(t, r.Contact.UriParams.Has("ob"), "an RFC 5626 outbound Contact: %s", r.Contact.String())
	assert.Equal(t, 2*time.Second, r.Expires)

	// the carrier calls the AOR over the registered flow, the call reaches the in-cluster peer
	call, err := carrier.Dial(ctx, aor)
	require.NoError(t, err)
	in := answered(t, ctx, peer)
	assertEcho(t, call, 25)
	require.NoError(t, call.Hangup(ctx))
	select {
	case <-in.Done():
	case <-ctx.Done():
		t.Fatal("the BYE never reached the peer")
	}
	mediaDeleted(t, s)

	// the registration is refreshed before it expires, over the same flow
	refreshed := registered()
	assert.Equal(t, r.Contact.String(), refreshed.Contact.String(), "the same flow")
}

// recordingOffload records the flows offloaded and removed.
type recordingOffload struct {
	*offload.NullEngine
	mu   sync.Mutex
	live map[string][2]offload.Connection
}

func (r *recordingOffload) Upsert(a, b offload.Connection, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live == nil {
		r.live = map[string][2]offload.Connection{}
	}
	r.live[a.String()+"/"+b.String()] = [2]offload.Connection{a, b}
	return nil
}

func (r *recordingOffload) Remove(a, b offload.Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, a.String()+"/"+b.String())
	return nil
}

func (r *recordingOffload) flows() [][2]offload.Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	ret := [][2]offload.Connection{}
	for _, f := range r.live {
		ret = append(ret, f)
	}
	return ret
}
