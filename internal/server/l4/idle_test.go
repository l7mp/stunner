package l4

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
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// stubLeg is the minimal upstream leg a flow needs to tear itself down.
type stubLeg struct{ net.Conn }

func (stubLeg) Class() string { return "cl" }

// countingEngine answers Packets with whatever the case wants, which is the only thing the idle
// check asks of an engine.
type countingEngine struct {
	offload.Engine // the null engine, so the teardown path has something to call
	pkts           uint64
	known          bool
}

func newCountingEngine(pkts uint64, known bool) *countingEngine {
	return &countingEngine{Engine: offload.NewNullEngine(), pkts: pkts, known: known}
}

func (e *countingEngine) Packets(_, _ offload.Connection) (uint64, bool) { return e.pkts, e.known }

// TestCheckIdleConsultsTheEngine pins the decision an offloaded flow's idle check has to make,
// without waiting on a timer for it. An offloaded flow's pumps never run, so its own timestamp is
// stale by definition: the engine's packet counter is the only thing that can tell a live flow
// from a dead one, and getting this wrong reaps flows mid-traffic.
func TestCheckIdleConsultsTheEngine(t *testing.T) {
	for _, c := range []struct {
		name       string
		registered bool
		lastSample uint64
		engine     *countingEngine
		survives   bool
	}{
		{
			name:       "kernel moved packets since the last check",
			registered: true,
			engine:     newCountingEngine(17, true),
			survives:   true,
		},
		{
			name:       "kernel knows the flow but forwarded nothing",
			registered: true,
			lastSample: 17,
			engine:     newCountingEngine(17, true),
		},
		{
			// includes every flow that was never offloaded: the userspace verdict stands
			name:       "engine has no opinion on the flow",
			registered: true,
			engine:     newCountingEngine(0, false),
		},
		{
			name:   "flow was never registered with the engine",
			engine: newCountingEngine(17, true),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			log := logging.NewDefaultLoggerFactory().NewLogger("test")
			rt := &objruntime.Runtime{Config: objruntime.Config{OffloadEngine: c.engine}}
			s := &Server{
				idle:    time.Minute,
				log:     log,
				flows:   map[*Flow]struct{}{},
				offload: NewOffloadHandler("li", stnrv1.ListenerProtocolUDP, rt, log),
				events:  EventHandler{OnFlowDeleted: func(FlowEvent) {}},
			}

			client, leg := net.Pipe()
			f := &Flow{s: s, client: client, RelayStream: stubLeg{leg},
				timer: time.NewTimer(time.Hour)}
			s.flows[f] = struct{}{}
			// the flow has been quiet in user space for longer than the idle timeout, which
			// is the normal state of an offloaded flow
			f.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())

			if c.registered {
				s.offload.pairs[f] = offloadPair{pkts: c.lastSample}
			}

			f.checkIdle()

			assert.Equal(t, !c.survives, f.closed.Load(), "flow torn down")
			assert.Equal(t, c.survives, s.AllocationCount() == 1, "flow still counted")
		})
	}
}

// TestCheckIdleRearmsOnUserspaceActivity covers the other half: a flow that is NOT yet idle in user
// space is left alone without the engine being asked at all.
func TestCheckIdleRearmsOnUserspaceActivity(t *testing.T) {
	log := logging.NewDefaultLoggerFactory().NewLogger("test")
	// an engine that would insist the flow is dead, to prove it is not consulted
	eng := newCountingEngine(0, true)
	rt := &objruntime.Runtime{Config: objruntime.Config{OffloadEngine: eng}}
	s := &Server{
		idle:    time.Minute,
		log:     log,
		flows:   map[*Flow]struct{}{},
		offload: NewOffloadHandler("li", stnrv1.ListenerProtocolUDP, rt, log),
		events:  EventHandler{OnFlowDeleted: func(FlowEvent) {}},
	}

	client, leg := net.Pipe()
	f := &Flow{s: s, client: client, RelayStream: stubLeg{leg}, timer: time.NewTimer(time.Hour)}
	s.flows[f] = struct{}{}
	s.offload.pairs[f] = offloadPair{}
	f.touch()

	f.checkIdle()

	assert.False(t, f.closed.Load(), "a flow busy in user space survives")
	assert.Equal(t, 1, s.AllocationCount(), "flow still counted")
}

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

// TestOffloadPairShape pins what the flow engine hands the offload engine, which the matrix does
// not look at: the pair is flipped, because the engine decapsulates ChannelData arriving on its
// client side and the channel is on the upstream wire, not on our client. Handing these over the
// wrong way round would have the datapath strip a channel header from raw client payload.
func TestOffloadPairShape(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	listenerAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 3478}
	peerAddr := &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 2222}
	serverAddr := &net.UDPAddr{IP: net.ParseIP("7.7.7.7"), Port: 3478}
	wireAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 50000}

	eng := newRecordingEngine()
	rt := &objruntime.Runtime{Config: objruntime.Config{OffloadEngine: eng}}
	log := logging.NewDefaultLoggerFactory().NewLogger("test")
	h := NewOffloadHandler("li", stnrv1.ListenerProtocolUDP, rt, log)

	f := &Flow{
		Peer:        peerAddr,
		RelayPacket: stubRelay{server: serverAddr, local: wireAddr, chanID: 0x4000, framed: true},
		Event: FlowEvent{
			SrcAddr: clientAddr, DstAddr: listenerAddr, Peer: peerAddr,
			RelayAddr: wireAddr, ServerAddr: serverAddr,
			Cluster: "cl", ClusterProtocol: stnrv1.ClusterProtocolTURNUDP,
		},
	}
	h.Upsert(f)

	require.Eventually(t, func() bool { return len(eng.registrations()) == 1 },
		2*time.Second, 20*time.Millisecond, "flow registered")
	reg := eng.registrations()[0]

	assert.Equal(t, serverAddr, reg.client.RemoteAddr, "wire faces the upstream server")
	assert.Equal(t, wireAddr, reg.client.LocalAddr, "wire leaves the transport socket")
	assert.Equal(t, uint32(0x4000), reg.client.ChannelID, "channel on the wire side")
	assert.Equal(t, clientAddr, reg.peer.RemoteAddr, "raw side faces our client")
	assert.Equal(t, listenerAddr, reg.peer.LocalAddr, "raw side arrives at the listener")
	assert.Zero(t, reg.peer.ChannelID, "no channel on the raw side")
	assert.Equal(t, "li", reg.listener, "listener attribution")
	assert.Equal(t, "cl", reg.cluster, "cluster attribution")
}

// recordingEngine records what the flow engine asks of the offload engine.
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
