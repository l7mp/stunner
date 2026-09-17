package l4

import (
	"net"
	"sync"
	"time"

	"github.com/pion/logging"

	objectturn "github.com/l7mp/stunner/v2/internal/object/turn"
	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// FlowEvent carries the context of a flow lifecycle event. It is pure data (addresses,
// names): safe to serialize or ship across a process boundary.
type FlowEvent struct {
	// SrcAddr and DstAddr are the client's source address and the listener-side address
	// serving it.
	SrcAddr, DstAddr net.Addr
	// Protocol is the client-side transport ("udp", "tcp" or "stdin").
	Protocol string
	// Peer is the resolved peer endpoint and PeerProtocol its transport ("udp" or "tcp").
	Peer         net.Addr
	PeerProtocol string
	// Cluster is the routed cluster that admitted the peer, and ClusterProtocol its
	// protocol: the transport of the relay leg, and what decides whether the leg is
	// offloadable at all.
	Cluster         string
	ClusterProtocol stnrv1.ClusterProtocol
	// Username and Realm are the flow's quota identity, as minted by the quota gate.
	Username, Realm string
	// RelayAddr is the local address the relayed traffic leaves from: the relay socket of
	// a direct leg, the transport socket of an upstream TURN leg.
	RelayAddr net.Addr
	// ServerAddr is the upstream TURN server of a tunnel-mode flow, nil for direct legs.
	ServerAddr net.Addr
}

// EventHandler is the set of callbacks the flow engine calls on flow lifecycle events, the L4
// analog of the TURN server's allocation event handlers. A flow collapses the TURN
// allocation/permission/channel lifecycle into a single event pair: flow setup is the
// allocation, the admission verdict is the permission, and the fixed client-to-peer binding
// is the channel.
type EventHandler struct {
	// OnFlowCreated is called after a flow has been admitted, wired, and registered.
	OnFlowCreated func(ev FlowEvent)
	// OnFlowDeleted is called when a flow is being torn down, before its legs close.
	OnFlowDeleted func(ev FlowEvent)
	// OnFlowError is called when a pump exits on a write error.
	OnFlowError func(srcAddr net.Addr, protocol, message string)
}

// NewEventHandler returns the default flow event wiring for a listener context: logging,
// quota accounting through the shared Quota machinery of the TURN engine, and
// kernel-offload bookkeeping.
func NewEventHandler(listener string, rt *objruntime.Runtime, log logging.LeveledLogger, q *objectturn.Quota) EventHandler {
	return EventHandler{
		OnFlowCreated: func(ev FlowEvent) {
			log.Debugf("flow created: client=%s-%s:%s, peer=%s:%s, cluster=%s",
				ev.SrcAddr.String(), ev.DstAddr.String(), ev.Protocol,
				ev.Peer.String(), ev.PeerProtocol, ev.Cluster)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol, ev.Username,
				ev.Realm, objectturn.AllocationCreated)
		},
		OnFlowDeleted: func(ev FlowEvent) {
			log.Debugf("flow deleted: client=%s-%s:%s, peer=%s:%s",
				ev.SrcAddr.String(), ev.DstAddr.String(), ev.Protocol,
				ev.Peer.String(), ev.PeerProtocol)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol, ev.Username,
				ev.Realm, objectturn.AllocationDeleted)
		},
		OnFlowError: func(srcAddr net.Addr, protocol, message string) {
			log.Debugf("flow error: client=%s:%s, error=%s", srcAddr.String(), protocol,
				message)
		},
	}
}

// flowIdentity mints the quota identity of a flow: the L4 analog of the TURN username is the
// client's source IP (raw flows carry no credentials, so the quota principal is the client
// endpoint), under the auth realm.
func flowIdentity(rt *objruntime.Runtime, srcAddr net.Addr) (username, realm string) {
	realm = stnrv1.DefaultRealm
	if auth, _ := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv1.AuthConfig); auth != nil {
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

// channelPollBackoff and channelPollAttempts pace the channel learning of tunnel-mode
// flows: the pion client assigns the channel at the first client write towards the peer and
// binds it asynchronously.
const (
	channelPollBackoff  = 100 * time.Millisecond
	channelPollAttempts = 5
)

// offloadPair is the connection pair a flow is registered with, in the order the engine takes
// them: the side whose inbound packets carry ChannelData first, the raw side second. A pair with
// no channel on either side degenerates to a plain 5-tuple forwarder.
type offloadPair struct {
	client, peer offload.Connection
	// pkts is the engine's packet counter as of the last liveness check, so the reaper can
	// tell whether the kernel moved anything for this flow since it last looked.
	pkts uint64
}

// offloadHandler registers the listener's flows with the kernel offload engine, keeping the
// per-flow housekeeping (the tunnel-mode channel learning and the registered connection
// pairs) here so the flow events stay pure data.
type offloadHandler struct {
	rt       *objruntime.Runtime
	listener string
	proto    stnrv1.ListenerProtocol
	log      logging.LeveledLogger

	mu    sync.Mutex
	pairs map[*flow]offloadPair
}

func newOffloadHandler(listener string, proto stnrv1.ListenerProtocol, rt *objruntime.Runtime, log logging.LeveledLogger) *offloadHandler {
	return &offloadHandler{rt: rt, listener: listener, proto: proto, log: log,
		pairs: make(map[*flow]offloadPair)}
}

// upsert registers a flow with the offload engine, for the one leg shape the engines accelerate:
// offload.Offloadable decides, and on this side only a udp listener tunnelling to a turn-udp
// cluster qualifies. Registering any other flow would be worse than useless: the datapath would
// parse a channel header out of raw client payload and prepend one to the peer's replies,
// corrupting both directions.
//
// The upstream channel is assigned at the first client write and bound a round trip later, so a
// goroutine retries until the transport reports live framing, giving up when the flow closes or
// after the last attempt, leaving the flow unoffloaded.
func (h *offloadHandler) upsert(f *flow) {
	if !offload.Offloadable(h.proto, f.ev.ClusterProtocol) {
		return
	}

	// raw client traffic from SrcAddr arriving at the listener socket DstAddr
	raw := offload.Connection{RemoteAddr: f.ev.SrcAddr, LocalAddr: f.ev.DstAddr,
		Protocol: offload.ProtocolUDP}

	go func() {
		backoff := channelPollBackoff
		for attempt := 0; attempt < channelPollAttempts; attempt++ {
			time.Sleep(backoff)
			backoff *= 2
			if f.closed.Load() {
				return
			}
			ch, ok := f.relayPacket.Channel(f.ev.Peer)
			if !ok {
				continue
			}
			wire := offload.Connection{RemoteAddr: f.ev.ServerAddr, LocalAddr: f.ev.RelayAddr,
				Protocol: offload.ProtocolUDP, ChannelID: uint32(ch)}
			h.register(f, offloadPair{client: wire, peer: raw})
			return
		}
	}()
}

// register books the flow's connection pair and installs it on the engine. The closed
// re-check under the lock closes the race with a concurrent remove: a flow torn down while
// its channel was being learned must not leave a stale offload behind.
func (h *offloadHandler) register(f *flow, p offloadPair) {
	h.mu.Lock()
	if f.closed.Load() {
		h.mu.Unlock()
		return
	}
	h.pairs[f] = p
	h.mu.Unlock()

	if err := h.rt.OffloadEngine.Upsert(p.client, p.peer, h.listener, f.ev.Cluster); err != nil {
		h.log.Errorf("could not create offload %s(listener:%s)->%s(cluster:%s): %s",
			p.client.String(), h.listener, p.peer.String(), f.ev.Cluster, err.Error())
	}
}

// active reports whether the kernel has forwarded anything for this flow since the last call,
// and whether the engine has an opinion at all. An offloaded flow's pumps never run, so without
// this the flow's idle timer reaps it mid-traffic; a flow the engine does not know returns
// (false, false), which means "no kernel-side opinion", not "idle".
func (h *offloadHandler) active(f *flow) (moved, known bool) {
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

// remove uninstalls the flow's registered connection pair; a flow that never got offloaded
// (a tunnel-mode flow whose channel never bound) has no pair booked and needs nothing.
func (h *offloadHandler) remove(f *flow) {
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
