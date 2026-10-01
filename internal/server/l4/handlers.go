package l4

import (
	"net"
	"sync"
	"time"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// FlowEvent carries the context of a flow lifecycle event. It is pure data (addresses,
// names): safe to serialize or ship across a process boundary.
type FlowEvent struct {
	// SrcAddr and DstAddr are the client's source address and the listener-side address
	// serving it.
	SrcAddr, DstAddr net.Addr
	// Protocol is the client-side transport, the listener protocol.
	Protocol stnrv2.Protocol
	// Peer is the endpoint the flow was forwarded to and PeerProtocol its transport
	// (ProtocolUDP or ProtocolTCP).
	Peer         net.Addr
	PeerProtocol stnrv2.Protocol
	// Cluster is the cluster the flow was forwarded through.
	Cluster string
	// Tunnel is the transport of the TURN tunnel the leg runs through, ProtocolUnknown for a
	// direct leg: with the protocols, what decides whether the flow is offloadable at all.
	Tunnel stnrv2.Protocol
	// Username and Realm are the flow's quota identity, as minted by the quota gate.
	Username, Realm string
	// RelayAddr is the local address the relayed traffic leaves from: the socket of a direct
	// leg, the transport socket of an upstream TURN leg.
	RelayAddr net.Addr
	// ServerAddr is the upstream TURN server of a tunnelled flow, nil for direct legs.
	ServerAddr net.Addr
}

// EventHandler is the set of callbacks the L4 server calls on flow lifecycle events, the L4
// analog of the TURN server's allocation event handlers. A flow collapses the TURN
// allocation/permission/channel lifecycle into a single event pair: flow setup is the
// allocation, the endpoint choice is the permission, and the fixed client-to-peer binding is the
// channel.
type EventHandler struct {
	// OnFlowCreated is called after a flow has been admitted, wired, and registered.
	OnFlowCreated func(ev FlowEvent)
	// OnFlowDeleted is called when a flow is being torn down, before its legs close.
	OnFlowDeleted func(ev FlowEvent)
	// OnFlowError is called when a pump exits on a write error.
	OnFlowError func(srcAddr net.Addr, protocol stnrv2.Protocol, message string)
}

// NewEventHandler returns the default flow event wiring: logging and quota accounting through the
// shared Quota machinery of the TURN server.
func NewEventHandler(rt *objruntime.Runtime, log logging.LeveledLogger, q *turnsrv.Quota) EventHandler {
	return EventHandler{
		OnFlowCreated: func(ev FlowEvent) {
			log.Debugf("flow created: client=%s-%s:%s, peer=%s:%s, cluster=%s",
				ev.SrcAddr.String(), ev.DstAddr.String(), ev.Protocol,
				ev.Peer.String(), ev.PeerProtocol, ev.Cluster)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol.String(), ev.Username,
				ev.Realm, turnsrv.AllocationCreated)
		},
		OnFlowDeleted: func(ev FlowEvent) {
			log.Debugf("flow deleted: client=%s-%s:%s, peer=%s:%s",
				ev.SrcAddr.String(), ev.DstAddr.String(), ev.Protocol,
				ev.Peer.String(), ev.PeerProtocol)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol.String(), ev.Username,
				ev.Realm, turnsrv.AllocationDeleted)
		},
		OnFlowError: func(srcAddr net.Addr, protocol stnrv2.Protocol, message string) {
			log.Debugf("flow error: client=%s:%s, error=%s", srcAddr.String(), protocol,
				message)
		},
	}
}

// flowIdentity mints the quota identity of a flow: the L4 analog of the TURN username is the
// client's source IP (raw flows carry no credentials, so the quota principal is the client
// endpoint), under the auth realm.
func flowIdentity(rt *objruntime.Runtime, srcAddr net.Addr) (username, realm string) {
	realm = stnrv2.DefaultRealm
	if auth, _ := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv2.AuthConfig); auth != nil {
		realm = auth.Realm
	}
	switch a := srcAddr.(type) {
	case *net.UDPAddr:
		return a.IP.String(), realm
	case *net.TCPAddr:
		return a.IP.String(), realm
	default:
		// the stdin pair carries no IP endpoint; its name is the principal
		return srcAddr.String(), realm
	}
}

// ChannelPollBackoff and ChannelPollAttempts pace the channel learning of tunnelled flows: the
// TURN client assigns the channel at the first client write towards the peer and binds it
// asynchronously.
var (
	ChannelPollBackoff  = 100 * time.Millisecond
	ChannelPollAttempts = 5
)

// offloadPair is the connection pair a flow is registered with, in the order the engine takes
// them: the side whose inbound packets carry ChannelData first, the raw side second.
type offloadPair struct {
	client, peer offload.Connection
	// pkts is the engine's packet counter as of the last liveness check, so the reaper can
	// tell whether the kernel moved anything for this flow since it last looked.
	pkts uint64
}

// OffloadHandler registers the server's flows with the kernel offload engine, keeping the
// per-flow housekeeping (the channel learning of tunnelled flows and the registered connection
// pairs) here so the flow events stay pure data.
type OffloadHandler struct {
	rt  *objruntime.Runtime
	log logging.LeveledLogger

	mu    sync.Mutex
	pairs map[*Flow]offloadPair
}

// NewOffloadHandler creates the offload handler of an L4 server.
func NewOffloadHandler(rt *objruntime.Runtime, log logging.LeveledLogger) *OffloadHandler {
	return &OffloadHandler{rt: rt, log: log, pairs: make(map[*Flow]offloadPair)}
}

// Upsert registers a flow with the offload engine, for the one flow shape the engines
// accelerate: a plain UDP client dialing a UDP peer through a tunnel over TURN-UDP. Registering any other flow
// would be worse than useless: the datapath would parse a channel header out of raw client payload
// and prepend one to the peer's replies, corrupting both directions.
//
// The upstream channel is assigned at the first client write and bound a round trip later, so a
// goroutine retries until the leg reports live framing, giving up when the flow closes or after
// the last attempt, leaving the flow unoffloaded.
func (h *OffloadHandler) Upsert(f *Flow) {
	if f.Event.Protocol != stnrv2.ProtocolUDP || f.Event.PeerProtocol != stnrv2.ProtocolUDP ||
		f.Event.Tunnel != stnrv2.ProtocolTURNUDP {
		return
	}

	// raw client traffic from SrcAddr arriving at the listener socket DstAddr
	raw := offload.Connection{RemoteAddr: f.Event.SrcAddr, LocalAddr: f.Event.DstAddr,
		Protocol: offload.ProtocolUDP}

	go func() {
		backoff := ChannelPollBackoff
		for attempt := 0; attempt < ChannelPollAttempts; attempt++ {
			time.Sleep(backoff)
			backoff *= 2
			if f.Closed() {
				return
			}
			ch, ok := f.Leg.Channel(f.Event.Peer)
			if !ok {
				continue
			}
			wire := offload.Connection{RemoteAddr: f.Event.ServerAddr, LocalAddr: f.Event.RelayAddr,
				Protocol: offload.ProtocolUDP, ChannelID: uint32(ch)}
			h.register(f, offloadPair{client: wire, peer: raw})
			return
		}
	}()
}

// register books the flow's connection pair and installs it on the engine. The closed re-check
// under the lock closes the race with a concurrent remove: a flow torn down while its channel was
// being learned must not leave a stale offload behind.
func (h *OffloadHandler) register(f *Flow, p offloadPair) {
	h.mu.Lock()
	if f.Closed() {
		h.mu.Unlock()
		return
	}
	h.pairs[f] = p
	h.mu.Unlock()

	listener, cluster := f.Client.Tag().Name, f.Leg.Tag().Name
	if err := h.rt.OffloadEngine.Upsert(p.client, p.peer, listener, cluster); err != nil {
		h.log.Errorf("could not create offload %s(listener:%s)->%s(cluster:%s): %s",
			p.client.String(), listener, p.peer.String(), cluster, err.Error())
	}
}

// Active reports whether the kernel has forwarded anything for this flow since the last call, and
// whether the engine has an opinion at all. An offloaded flow's pumps never run, so without this
// the flow's idle timer reaps it mid-traffic; a flow the engine does not know returns (false,
// false), which means "no kernel-side opinion", not "idle".
func (h *OffloadHandler) Active(f *Flow) (moved, known bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pairs[f]
	if !ok {
		return false, false
	}
	pkts, known := h.rt.OffloadEngine.Packets(p.client, p.peer)
	if !known {
		return false, false
	}
	moved = pkts != p.pkts
	p.pkts = pkts
	h.pairs[f] = p
	return moved, true
}

// Remove uninstalls the flow's registered connection pair; a flow that never got offloaded (a
// tunnelled flow whose channel never bound) has no pair booked and needs nothing.
func (h *OffloadHandler) Remove(f *Flow) {
	h.mu.Lock()
	p, ok := h.pairs[f]
	delete(h.pairs, f)
	h.mu.Unlock()
	if !ok {
		return
	}

	if err := h.rt.OffloadEngine.Remove(p.client, p.peer); err != nil {
		h.log.Errorf("could not remove offload %s->%s: %s", p.client.String(),
			p.peer.String(), err.Error())
	}
}
