// Package server holds the packet servers a Listener runs: the TURN server for the TURN-*
// protocols and the L4 flow engine for the plain protocols, each in its own subpackage. A
// Server is created running by its constructor and lives for exactly one Start/Close cycle of
// its listener, which owns it and reports its active session count.
package server

import (
	"fmt"

	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// Server is a running packet server.
type Server interface {
	// Close shuts the server down, tearing down its transport listeners and sessions.
	Close() error
	// AllocationCount returns the number of active sessions: TURN allocations on a TURN
	// server, live flows on the L4 engine.
	AllocationCount() int
}

// New starts the server matching the listener protocol. Servers read the listener config back
// through the runtime, so the listener must already be registered.
func New(listener string, proto stnrv1.ListenerProtocol, rt *runtime.Runtime) (Server, error) {
	switch proto {
	case stnrv1.ProtocolTURNUDP, stnrv1.ProtocolTURNTCP, stnrv1.ProtocolTURNTLS,
		stnrv1.ProtocolTURNDTLS:
		s, err := turn.NewServer(listener, proto, rt)
		if err != nil {
			return nil, err
		}
		return s, nil
	case stnrv1.ProtocolUDP, stnrv1.ProtocolTCP, stnrv1.ProtocolSTDIN:
		s, err := l4.NewServer(listener, proto, rt)
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported listener protocol %q", proto.String())
	}
}
