package l4

import (
	"github.com/l7mp/stunner/v2/internal/server"
)

// Flow is one relayed client flow and the event describing it.
type Flow struct {
	server.Entry
	Event FlowEvent
}
