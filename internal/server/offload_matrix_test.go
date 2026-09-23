package server_test

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// matrixCluster is the routed cluster, as the Router hands it to a matcher.
type matrixCluster struct{ proto stnrv1.ClusterProtocol }

func (matrixCluster) Name() string                       { return "cl" }
func (c matrixCluster) Protocol() stnrv1.ClusterProtocol { return c.proto }
func (matrixCluster) TURNServer() *stnrv1.TURNServer     { return nil }
func (matrixCluster) Admits(net.IP, int) bool            { return true }

// matrixRouter routes every listener to one cluster, applying the caller's matcher as the real
// router does, and attributes a peer to it only when the protocol asked for matches.
type matrixRouter struct{ c matrixCluster }

func (r matrixRouter) Route(_ string, match func(objruntime.Cluster) bool) (objruntime.Cluster, bool) {
	if !match(r.c) {
		return nil, false
	}
	return r.c, true
}

func (r matrixRouter) RoutePeer(_ string, proto stnrv1.ClusterProtocol, _ net.IP, _ int) (string, bool) {
	if proto != r.c.proto {
		return "", false
	}
	return r.c.Name(), true
}

func (matrixRouter) InvalidateCache() {}

// TestOffloadMatrix checks the whole offload matrix: every listener protocol against every cluster
// protocol, the rule and both of the paths that have to obey it.
//
// The engines accelerate exactly one leg shape, plaintext ChannelData on one side and raw
// datagrams on the other, which leaves two mirror-image protocol pairs: a turn-udp listener
// relaying to a udp cluster, and a udp listener tunnelling to a turn-udp cluster. Nothing else
// offloads, in either direction.
func TestOffloadMatrix(t *testing.T) {
	// the flow engine learns the channel asynchronously; shorten the backoff so a rejected
	// registration can be distinguished from a merely slow one without waiting out the real
	// pacing
	defer func(d time.Duration) { l4.ChannelPollBackoff = d }(l4.ChannelPollBackoff)
	l4.ChannelPollBackoff = time.Millisecond

	clientAddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	listenerAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 3478}
	relayAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 50000}
	serverAddr := &net.UDPAddr{IP: net.ParseIP("7.7.7.7"), Port: 3478}
	peerAddr := &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 2222}

	listeners := []stnrv1.ListenerProtocol{
		stnrv1.ListenerProtocolUDP, stnrv1.ListenerProtocolTCP, stnrv1.ProtocolSTDIN,
		stnrv1.ListenerProtocolTURNUDP, stnrv1.ListenerProtocolTURNTCP,
		stnrv1.ListenerProtocolTURNTLS, stnrv1.ListenerProtocolTURNDTLS,
	}
	clusters := []stnrv1.ClusterProtocol{
		stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP,
		stnrv1.ClusterProtocolTURNUDP, stnrv1.ClusterProtocolTURNTCP,
		stnrv1.ClusterProtocolTURNTLS, stnrv1.ClusterProtocolTURNDTLS,
	}

	for _, lp := range listeners {
		for _, cp := range clusters {
			// the two accelerated shapes, and only those
			want := (lp == stnrv1.ListenerProtocolTURNUDP && cp == stnrv1.ClusterProtocolUDP) ||
				(lp == stnrv1.ListenerProtocolUDP && cp == stnrv1.ClusterProtocolTURNUDP)

			t.Run(lp.String()+"-listener/"+cp.String()+"-cluster", func(t *testing.T) {
				assert.Equal(t, want, offload.Offloadable(lp, cp), "offload rule")

				eng := newRecordingEngine()
				rt := &objruntime.Runtime{
					Config: objruntime.Config{OffloadEngine: eng},
					Router: matrixRouter{c: matrixCluster{proto: cp}},
				}
				log := logging.NewDefaultLoggerFactory().NewLogger("test")

				if lp.IsTURN() {
					// the TURN server engine: a channel binding on a listener serving
					// this protocol, routed to a cluster of that one
					h := turn.NewEventHandler("li", lp, rt, log,
						turn.NewQuotaHandler(rt))
					h.OnChannelCreated(clientAddr, listenerAddr, "UDP", "user", "realm",
						relayAddr, peerAddr, 0x4000)
					h.OnChannelDeleted(clientAddr, listenerAddr, "UDP", "user", "realm",
						relayAddr, peerAddr, 0x4000)

					if want {
						require.Len(t, eng.registrations(), 1, "offload registered")
						assert.Equal(t, 1, eng.removals(), "offload removed")
						return
					}
					assert.Empty(t, eng.registrations(), "no offload registered")
					assert.Zero(t, eng.removals(), "no offload removal attempted")
					return
				}

				// the L4 flow engine: a flow on a plain listener of this protocol, whose
				// upstream leg already carries a live channel, so only the rule can stop
				// the registration
				h := l4.NewOffloadHandler("li", lp, rt, log)
				f := &l4.Flow{
					Peer: peerAddr,
					RelayPacket: stubRelay{server: serverAddr, local: relayAddr,
						chanID: 0x4000, framed: true},
					Event: l4.FlowEvent{
						SrcAddr: clientAddr, DstAddr: listenerAddr, Peer: peerAddr,
						RelayAddr: relayAddr, ServerAddr: serverAddr,
						Cluster: "cl", ClusterProtocol: cp,
					},
				}
				h.Upsert(f)

				if want {
					require.Eventually(t, func() bool { return len(eng.registrations()) == 1 },
						2*time.Second, 20*time.Millisecond, "offload registered")
					return
				}
				// the probe would have run several times over by now, so an empty engine
				// means the rule rejected the flow rather than the probe being slow
				assert.Never(t, func() bool { return len(eng.registrations()) > 0 },
					50*time.Millisecond, 5*time.Millisecond, "no offload registered")
			})
		}
	}
}

// TestOffloadChannelPeerShape covers the two channel cases the protocol matrix cannot express,
// because neither is about protocols: a peer that is not a datagram endpoint, and a peer that no
// cluster routes any more.
func TestOffloadChannelPeerShape(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	listenerAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 3478}
	relayAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 50000}

	for _, c := range []struct {
		name  string
		peer  net.Addr
		route bool
	}{
		// an RFC 6062 relayed TCP connection never carries a channel
		{name: "stream peer", peer: &net.TCPAddr{IP: net.ParseIP("9.9.9.9"), Port: 2222}, route: true},
		// routing changed under a live channel: with no cluster behind the peer there is no
		// leg left to accelerate
		{name: "unrouted peer", peer: &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 2222}},
	} {
		t.Run(c.name, func(t *testing.T) {
			eng := newRecordingEngine()
			var router objruntime.Router = unroutedRouter{}
			if c.route {
				router = matrixRouter{c: matrixCluster{proto: stnrv1.ClusterProtocolUDP}}
			}
			rt := &objruntime.Runtime{
				Config: objruntime.Config{OffloadEngine: eng},
				Router: router,
			}
			log := logging.NewDefaultLoggerFactory().NewLogger("test")
			h := turn.NewEventHandler("li", stnrv1.ListenerProtocolTURNUDP, rt, log,
				turn.NewQuotaHandler(rt))

			h.OnChannelCreated(clientAddr, listenerAddr, "UDP", "user", "realm", relayAddr, c.peer, 0x4000)
			h.OnChannelDeleted(clientAddr, listenerAddr, "UDP", "user", "realm", relayAddr, c.peer, 0x4000)

			assert.Empty(t, eng.registrations(), "no offload registered")
			assert.Zero(t, eng.removals(), "no offload removal attempted")
		})
	}
}

// unroutedRouter routes nothing, which is what the router looks like once the cluster behind a
// live channel is gone.
type unroutedRouter struct{}

func (unroutedRouter) Route(string, func(objruntime.Cluster) bool) (objruntime.Cluster, bool) {
	return nil, false
}
func (unroutedRouter) RoutePeer(string, stnrv1.ClusterProtocol, net.IP, int) (string, bool) {
	return "", false
}
func (unroutedRouter) InvalidateCache() {}

// stubRelay is a datagram upstream leg with a fixed wire address and a channel that is live from
// the start, which is what the offload registration waits for.
type stubRelay struct {
	net.PacketConn
	server net.Addr
	local  net.Addr
	chanID uint16
	framed bool
}

func (s stubRelay) TransportAddrs() (net.Addr, net.Addr) { return s.local, s.server }
func (s stubRelay) Channel(net.Addr) (uint16, bool)      { return s.chanID, s.framed }
func (s stubRelay) Class(net.Addr) (string, bool)        { return "cl", true }

// recordingEngine records what the servers ask of the offload engine.
type recordingEngine struct {
	offload.Engine
	mu      sync.Mutex
	upserts []offloadRegistration
	removes int
}

type offloadRegistration struct {
	client, peer      offload.Connection
	listener, cluster string
}

func newRecordingEngine() *recordingEngine {
	return &recordingEngine{Engine: offload.NewNullEngine()}
}

func (e *recordingEngine) Upsert(client, peer offload.Connection, listener, cluster string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.upserts = append(e.upserts, offloadRegistration{client, peer, listener, cluster})
	return nil
}

func (e *recordingEngine) Remove(_, _ offload.Connection) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removes++
	return nil
}

func (e *recordingEngine) removals() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.removes
}

func (e *recordingEngine) registrations() []offloadRegistration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]offloadRegistration{}, e.upserts...)
}
