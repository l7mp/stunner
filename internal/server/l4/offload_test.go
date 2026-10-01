package l4

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

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

// TestOffloadActive pins the verdict an offloaded flow's idle check gets from the offload handler.
// An offloaded flow's pumps never run, so the engine's packet counter is the only thing that can
// tell a live flow from a dead one, and getting this wrong reaps flows mid-traffic.
func TestOffloadActive(t *testing.T) {
	for _, c := range []struct {
		name         string
		registered   bool
		lastSample   uint64
		engine       *countingEngine
		moved, known bool
	}{
		{
			name:       "kernel moved packets since the last check",
			registered: true,
			engine:     newCountingEngine(17, true),
			moved:      true,
			known:      true,
		},
		{
			name:       "kernel knows the flow but forwarded nothing",
			registered: true,
			lastSample: 17,
			engine:     newCountingEngine(17, true),
			known:      true,
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
			h := NewOffloadHandler(rt, log)
			f := &Flow{}
			if c.registered {
				h.pairs[f] = offloadPair{pkts: c.lastSample}
			}
			moved, known := h.Active(f)
			assert.Equal(t, c.moved, moved, "moved")
			assert.Equal(t, c.known, known, "known")
		})
	}
}

// stubWire is the wire of an upstream TURN leg with a fixed address and a channel that is live
// from the start, which is what the offload registration waits for.
type stubWire struct {
	server net.Addr
	local  net.Addr
	chanID uint16
	framed bool
}

func (s stubWire) TransportAddrs() (net.Addr, net.Addr) { return s.local, s.server }
func (s stubWire) Channel(net.Addr) (uint16, bool)      { return s.chanID, s.framed }

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
	h := NewOffloadHandler(rt, log)

	client, leg := net.Pipe()
	f := &Flow{
		Entry: server.Entry{
			Client: netconn.NewConn(client, api.Tag{Name: "li", Proto: stnrv2.ProtocolUDP}, nil),
			Leg: netconn.NewConn(leg, api.Tag{Name: "upstream", Proto: stnrv2.ProtocolUDP},
				stubWire{server: serverAddr, local: wireAddr, chanID: 0x4000, framed: true}),
		},
		Event: FlowEvent{
			SrcAddr: clientAddr, DstAddr: listenerAddr, Protocol: stnrv2.ProtocolUDP, Peer: peerAddr,
			PeerProtocol: stnrv2.ProtocolUDP, RelayAddr: wireAddr, ServerAddr: serverAddr,
			Cluster: "upstream", Tunnel: stnrv2.ProtocolTURNUDP,
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
	assert.Equal(t, "upstream", reg.cluster, "cluster attribution")
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
