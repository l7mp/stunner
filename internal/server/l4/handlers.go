package l4

import (
	"net"

	"github.com/pion/logging"

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
	// Peer is the endpoint the flow was forwarded to and PeerProtocol the protocol of the leg
	// towards it, its tag's.
	Peer         net.Addr
	PeerProtocol stnrv2.Protocol
	// Cluster is the cluster the flow was forwarded through.
	Cluster string
	// Username and Realm are the flow's quota identity, as minted by the quota gate.
	Username, Realm string
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
// endpoint), under the realm of the authentication policy the server names, if any.
func (s *Server) flowIdentity(srcAddr net.Addr) (username, realm string) {
	s.mu.RLock()
	name := s.auth
	s.mu.RUnlock()
	realm = stnrv2.DefaultRealm
	if auth := s.rt.AuthConfig(name); auth != nil {
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
