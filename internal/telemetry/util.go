package telemetry

// Direction species whether a conn stat applies in the sender or a receiving direction from the
// standpoint of STUNner.
type Direction int

const (
	Incoming Direction = iota + 1
	Outgoing
)

const (
	incomingStr = "rx"
	outgoingStr = "tx"
)

// String returns a string representation for a Direction.
func (d Direction) String() string {
	switch d {
	case Incoming:
		return incomingStr
	case Outgoing:
		return outgoingStr
	default:
		panic("unknown direction")
	}
}

// ConnType species whether a conn stat was collected at a listener or a cluster.
type ConnType int

const (
	ListenerType ConnType = iota + 1
	ClusterType
)

const (
	listenerStr = "listener"
	clusterStr  = "cluster"
)

// String returns a string representation for a ConnType.
func (c ConnType) String() string {
	switch c {
	case ListenerType:
		return listenerStr
	case ClusterType:
		return clusterStr
	default:
		panic("unknown conn-type")
	}
}
