package l4

import "github.com/l7mp/stunner/v2/internal/flow"

// Flow is one relayed client flow and the event describing it.
type Flow struct {
	flow.Entry
	Event FlowEvent
}
