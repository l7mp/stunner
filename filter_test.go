package stunner

import (
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v5/test"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// filterTURNConfig is a TURN server whose cluster admits the peers 1.2.3.5 by address and filters
// their ports.
func filterTURNConfig(rules ...stnrv2.FilterRule) *stnrv2.StunnerConfig {
	noHealthCheck := ""
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel, HealthCheckEndpoint: &noHealthCheck},
		Auths: []stnrv2.AuthConfig{{Type: "static",
			Credentials: map[string]string{"username": "user1", "password": "passwd1"}}},
		Listeners: []stnrv2.ListenerConfig{{Name: "udp", Protocol: "UDP", Addr: "1.2.3.4", Port: 3478,
			Servers: []string{"turn"}}},
		Servers: []stnrv2.ServerConfig{{Name: "turn", Type: "turn", Clusters: []string{"peers"}}},
		Clusters: []stnrv2.ClusterConfig{{Name: "peers", Protocol: "UDP",
			Endpoints: []string{"1.2.3.5"}, Addrs: []string{"1.2.3.4"}, Filters: []string{"ports"}}},
		Filters: []stnrv2.FilterConfig{{Name: "ports", Rules: rules}},
	}
}

// TestClusterFilter checks the filters of a cluster on the traffic it carries: a TURN relay drops
// the datagrams to and from the peer ports they refuse, while the permission is the address's; an
// L4 flow to a refused peer is never dialed.
func TestClusterFilter(t *testing.T) {
	lim := test.TimeOut(60 * time.Second)
	defer lim.Stop()
	report := test.CheckRoutines(t)
	defer report()
	lf := logger.NewLoggerFactory(stunnerTestLoglevel)

	t.Run("turn-relay", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		allowed := startUDPEcho(t, tn.pod, "1.2.3.5:6678")
		refused := startUDPEcho(t, tn.pod, "1.2.3.5:6679")

		only6678 := stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.5"},
			Ports: []stnrv2.FilterPort{{Port: 6678}}}
		s := NewStunner(Options{Name: "filter-turn", Net: tn.pod,
			LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		rest := stnrv2.FilterRule{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.5"}}
		conf := filterTURNConfig(only6678, rest)
		require.NoError(t, s.Reconcile(allowPeers(conf)))

		allocate := func() net.PacketConn {
			lconn, err := tn.client.ListenPacket("udp4", "0.0.0.0:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = lconn.Close() }) // the TURN client does not close the conn it is given
			client, err := turn.NewClient(&turn.ClientConfig{STUNServerAddr: "1.2.3.4:3478",
				TURNServerAddr: "1.2.3.4:3478", Username: "user1", Password: "passwd1", Conn: lconn,
				Net: tn.client, LoggerFactory: lf})
			require.NoError(t, err)
			t.Cleanup(client.Close)
			require.NoError(t, client.Listen())
			relay, err := client.Allocate()
			require.NoError(t, err)
			t.Cleanup(func() { _ = relay.Close() })
			return relay
		}
		relay := allocate()

		echo := func(relay, peer net.PacketConn) bool {
			_, err := relay.WriteTo([]byte("hello"), peer.LocalAddr())
			require.NoError(t, err, "the permission is the address's: the write succeeds")
			buf := make([]byte, 100)
			require.NoError(t, relay.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
			n, from, err := relay.ReadFrom(buf)
			return err == nil && string(buf[:n]) == "hello" && from.String() == peer.LocalAddr().String()
		}
		assert.True(t, echo(relay, allowed), "the admitted port relays")
		assert.False(t, echo(relay, refused), "the refused port is dropped")

		// the return direction too: the refused port reaches the client through a permitted
		// address, and is dropped
		_, err := refused.WriteTo([]byte("hello"), relay.LocalAddr())
		require.NoError(t, err)
		buf := make([]byte, 100)
		require.NoError(t, relay.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
		_, _, err = relay.ReadFrom(buf)
		assert.Error(t, err, "a datagram from a refused port is dropped")

		// the rules change in place, on the live relay: a rule before the final DENY admits
		conf.Filters[0].Rules = []stnrv2.FilterRule{only6678, {Action: "ALLOW",
			IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.5"}, Ports: []stnrv2.FilterPort{{Port: 6679}}}, rest}
		require.NoError(t, s.Reconcile(allowPeers(conf)))
		assert.True(t, echo(relay, refused), "a new rule admits the port")

		// the first matching rule decides: an exception before the ALLOW; an admitted peer stays
		// admitted on its relay, a new relay refuses it
		conf.Filters[0].Rules = append([]stnrv2.FilterRule{{Action: "DENY",
			IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.0/24"}, Ports: []stnrv2.FilterPort{{Port: 6678}}}},
			conf.Filters[0].Rules...)
		require.NoError(t, s.Reconcile(allowPeers(conf)))
		assert.True(t, echo(relay, allowed), "an admitted peer stays admitted")
		fresh := allocate()
		assert.False(t, echo(fresh, allowed), "the earlier DENY decides")
		assert.True(t, echo(fresh, refused))

		// a missing filter has no rules: everything passes
		conf.Filters = nil
		require.NoError(t, s.Reconcile(allowPeers(conf)))
		assert.True(t, echo(fresh, allowed), "a missing filter admits")
	})

	t.Run("l4-dial", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		startUDPEcho(t, tn.pod, "1.2.3.5:25680")
		s := NewStunner(Options{Name: "filter-l4", Net: tn.pod,
			LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		conf := plainListenerConfig("UDP", tn.host, 23478, "1.2.3.5:25680")
		conf.Clusters[0].Filters = []string{"no-echo"}
		conf.Filters = []stnrv2.FilterConfig{{Name: "no-echo", Rules: []stnrv2.FilterRule{
			{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "1.2.3.5"},
				Ports: []stnrv2.FilterPort{{Port: 25680}}}}}}
		require.NoError(t, s.Reconcile(allowPeers(conf)))

		client, err := tn.client.Dial("udp", "1.2.3.4:23478")
		require.NoError(t, err)
		defer client.Close() //nolint:errcheck
		_, err = client.Write([]byte("hello"))
		require.NoError(t, err)
		buf := make([]byte, 100)
		require.NoError(t, client.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
		_, err = client.Read(buf)
		assert.Error(t, err, "the refused peer is never dialed")
		assert.Equal(t, 0, s.GetServer("plain").(api.Server).Sessions(), "no flow")
	})
}
