package sip

import (
	"net"

	"github.com/pion/logging"

	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// CallEvent is a call lifecycle event.
type CallEvent struct {
	// SrcAddr and DstAddr are the trunk's address and the listener-side address serving it.
	SrcAddr, DstAddr net.Addr
	// Protocol is the transport of the trunk's conn.
	Protocol stnrv2.Protocol
	// CallID is the SIP Call-ID of the call.
	CallID string
	// Username and Realm are the call's quota identity.
	Username, Realm string
}

// EventHandler is the set of callbacks of the SIP server on call lifecycle events.
type EventHandler struct {
	// OnCallCreated is called when a call is established.
	OnCallCreated func(ev CallEvent)
	// OnCallDeleted is called when an established call ends.
	OnCallDeleted func(ev CallEvent)
	// OnCallError is called when a call fails.
	OnCallError func(srcAddr net.Addr, protocol stnrv2.Protocol, message string)
}

// NewEventHandler returns the default call event wiring: logging and the quota accounting shared
// with the TURN server.
func NewEventHandler(log logging.LeveledLogger, q *turnsrv.Quota) EventHandler {
	return EventHandler{
		OnCallCreated: func(ev CallEvent) {
			log.Debugf("call created: call-id=%s, trunk=%s-%s:%s", ev.CallID, ev.SrcAddr.String(),
				ev.DstAddr.String(), ev.Protocol)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol.String(), ev.Username,
				ev.Realm, turnsrv.AllocationCreated)
		},
		OnCallDeleted: func(ev CallEvent) {
			log.Debugf("call deleted: call-id=%s, trunk=%s-%s:%s", ev.CallID, ev.SrcAddr.String(),
				ev.DstAddr.String(), ev.Protocol)
			q.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol.String(), ev.Username,
				ev.Realm, turnsrv.AllocationDeleted)
		},
		OnCallError: func(srcAddr net.Addr, protocol stnrv2.Protocol, message string) {
			log.Debugf("call error: trunk=%s:%s, error=%s", srcAddr.String(), protocol, message)
		},
	}
}
