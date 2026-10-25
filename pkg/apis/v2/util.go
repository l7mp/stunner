package v2

import (
	"fmt"
	"strings"
)

// AuthType specifies the type of the STUN/TURN authentication mechanism used by STUNner.
type AuthType int

const (
	AuthTypeNone AuthType = iota
	AuthTypeStatic
	AuthTypeEphemeral
)

const (
	authTypeNoneStr      = "none"
	authTypeStaticStr    = "static"
	authTypeEphemeralStr = "ephemeral"
)

// NewAuthType parses the authentication mechanism specification.
func NewAuthType(raw string) (AuthType, error) {
	switch raw {
	case authTypeStaticStr:
		return AuthTypeStatic, nil
	case authTypeEphemeralStr:
		return AuthTypeEphemeral, nil
	case authTypeNoneStr:
		return AuthTypeNone, nil
	default:
		return AuthTypeNone, fmt.Errorf("unknown authentication type: \"%s\"", raw)
	}
}

// String returns a string representation for the authentication mechanism.
func (a AuthType) String() string {
	switch a {
	case AuthTypeNone:
		return authTypeNoneStr
	case AuthTypeStatic:
		return authTypeStaticStr
	case AuthTypeEphemeral:
		return authTypeEphemeralStr
	default:
		return "<unknown>"
	}
}

// PQCMode selects the post-quantum key exchange policy of a TLS listener.
type PQCMode int

const (
	// PQCModeDefault serves Go's default TLS settings.
	PQCModeDefault PQCMode = iota
	// PQCModePreferred prefers a post-quantum hybrid key exchange, falling back to classical
	// TLS 1.2 or 1.3 for clients that do not offer one.
	PQCModePreferred
	// PQCModeEnforced serves TLS 1.3 with a post-quantum hybrid key exchange only.
	PQCModeEnforced
)

const (
	pqcModeDefaultStr   = "default"
	pqcModePreferredStr = "preferred"
	pqcModeEnforcedStr  = "enforced"
)

// NewPQCMode parses a PQC mode; the empty string is the default mode.
func NewPQCMode(raw string) (PQCMode, error) {
	switch strings.ToLower(raw) {
	case "", pqcModeDefaultStr:
		return PQCModeDefault, nil
	case pqcModePreferredStr:
		return PQCModePreferred, nil
	case pqcModeEnforcedStr:
		return PQCModeEnforced, nil
	default:
		return PQCModeDefault, fmt.Errorf("unknown PQC mode: %q", raw)
	}
}

// String returns the name of the PQC mode.
func (m PQCMode) String() string {
	switch m {
	case PQCModeDefault:
		return pqcModeDefaultStr
	case PQCModePreferred:
		return pqcModePreferredStr
	case PQCModeEnforced:
		return pqcModeEnforcedStr
	default:
		return "<unknown>"
	}
}

// Protocol specifies a network protocol. A single enum lists every protocol STUNner understands;
// which subset is valid in a given context is enforced at the use site: NewListenerProtocol and
// NewClusterProtocol parse the listener and the cluster subsets.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolUDP
	ProtocolTCP
	ProtocolTLS
	ProtocolDTLS
	ProtocolTURNUDP
	ProtocolTURNTCP
	ProtocolTURNTLS
	ProtocolTURNDTLS
	ProtocolSTDIN
	ProtocolFIFO
	ProtocolSRT
)

const (
	protocolUDPStr      = "UDP"
	protocolTCPStr      = "TCP"
	protocolTLSStr      = "TLS"
	protocolDTLSStr     = "DTLS"
	protocolTURNUDPStr  = "TURN-UDP"
	protocolTURNTCPStr  = "TURN-TCP"
	protocolTURNTLSStr  = "TURN-TLS"
	protocolTURNDTLSStr = "TURN-DTLS"
	protocolSTDINStr    = "STDIN"
	protocolFIFOStr     = "FIFO"
	protocolSRTStr      = "SRT"
)

// NewProtocol parses a protocol name into a Protocol. It is permissive: it accepts any known
// protocol token and errors only on an unrecognized string.
func NewProtocol(raw string) (Protocol, error) {
	switch strings.ToUpper(raw) {
	case protocolUDPStr:
		return ProtocolUDP, nil
	case protocolTCPStr:
		return ProtocolTCP, nil
	case protocolTLSStr:
		return ProtocolTLS, nil
	case protocolDTLSStr:
		return ProtocolDTLS, nil
	case protocolTURNUDPStr:
		return ProtocolTURNUDP, nil
	case protocolTURNTCPStr:
		return ProtocolTURNTCP, nil
	case protocolTURNTLSStr:
		return ProtocolTURNTLS, nil
	case protocolTURNDTLSStr:
		return ProtocolTURNDTLS, nil
	case protocolSTDINStr:
		return ProtocolSTDIN, nil
	case protocolFIFOStr:
		return ProtocolFIFO, nil
	case protocolSRTStr:
		return ProtocolSRT, nil
	default:
		return ProtocolUnknown, fmt.Errorf("unknown protocol: \"%s\"", raw)
	}
}

// String returns the canonical string representation of a protocol.
func (p Protocol) String() string {
	switch p {
	case ProtocolUDP:
		return protocolUDPStr
	case ProtocolTCP:
		return protocolTCPStr
	case ProtocolTLS:
		return protocolTLSStr
	case ProtocolDTLS:
		return protocolDTLSStr
	case ProtocolTURNUDP:
		return protocolTURNUDPStr
	case ProtocolTURNTCP:
		return protocolTURNTCPStr
	case ProtocolTURNTLS:
		return protocolTURNTLSStr
	case ProtocolTURNDTLS:
		return protocolTURNDTLSStr
	case ProtocolSTDIN:
		return protocolSTDINStr
	case ProtocolFIFO:
		return protocolFIFOStr
	case ProtocolSRT:
		return protocolSRTStr
	default:
		return "<unknown>"
	}
}

// NewListenerProtocol parses a listener protocol: UDP, TCP, TLS, DTLS, STDIN or FIFO.
func NewListenerProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil {
		switch p {
		case ProtocolUDP, ProtocolTCP, ProtocolTLS, ProtocolDTLS, ProtocolSTDIN, ProtocolFIFO:
			return p, nil
		}
	}
	return ProtocolUnknown, fmt.Errorf("unknown listener protocol: %q", raw)
}

// NewClusterProtocol parses a cluster protocol: UDP, TCP or FIFO, the network the peers are
// reached over.
func NewClusterProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil &&
		(p == ProtocolUDP || p == ProtocolTCP || p == ProtocolFIFO) {
		return p, nil
	}
	return ProtocolUnknown, fmt.Errorf("unknown cluster protocol: %q", raw)
}

// ClusterType specifies how the endpoints of a cluster are resolved.
type ClusterType int

const (
	// ClusterTypeStatic endpoints are IP addresses with an optional port.
	ClusterTypeStatic ClusterType = iota + 1
	// ClusterTypeStrictDNS endpoints are domain names resolved in the background.
	ClusterTypeStrictDNS
	// ClusterTypeUnknown is an invalid cluster type.
	ClusterTypeUnknown
)

const (
	clusterTypeStaticStr    = "STATIC"
	clusterTypeStrictDNSStr = "STRICT_DNS"
)

// NewClusterType parses a cluster type.
func NewClusterType(raw string) (ClusterType, error) {
	switch strings.ToUpper(raw) {
	case clusterTypeStaticStr:
		return ClusterTypeStatic, nil
	case clusterTypeStrictDNSStr:
		return ClusterTypeStrictDNS, nil
	default:
		return ClusterTypeUnknown, fmt.Errorf("unknown cluster type: %q", raw)
	}
}

// String returns the name of the cluster type.
func (t ClusterType) String() string {
	switch t {
	case ClusterTypeStatic:
		return clusterTypeStaticStr
	case ClusterTypeStrictDNS:
		return clusterTypeStrictDNSStr
	default:
		return "<unknown>"
	}
}

// LBPolicy is how a server chooses the peer of a new session among the targets of its clusters.
type LBPolicy int

const (
	// LBPolicyUnknown is an invalid policy.
	LBPolicyUnknown LBPolicy = iota
	// LBPolicyRoundRobin chooses the targets in turn.
	LBPolicyRoundRobin
)

const (
	lbPolicyRoundRobinStr = "ROUND_ROBIN"
)

// NewLBPolicy parses a load-balancing policy.
func NewLBPolicy(raw string) (LBPolicy, error) {
	switch strings.ToUpper(raw) {
	case lbPolicyRoundRobinStr:
		return LBPolicyRoundRobin, nil
	default:
		return LBPolicyUnknown, fmt.Errorf("unknown load-balancing policy: %q", raw)
	}
}

// String returns the name of the load-balancing policy.
func (p LBPolicy) String() string {
	switch p {
	case LBPolicyRoundRobin:
		return lbPolicyRoundRobinStr
	default:
		return "<unknown>"
	}
}

// FilterAction is what a filter rule does with the traffic it matches.
type FilterAction int

const (
	// FilterActionUnknown is an invalid action.
	FilterActionUnknown FilterAction = iota
	// FilterActionAllow admits the traffic.
	FilterActionAllow
	// FilterActionDeny drops the traffic.
	FilterActionDeny
)

const (
	filterActionAllowStr = "ALLOW"
	filterActionDenyStr  = "DENY"
)

// NewFilterAction parses a filter action.
func NewFilterAction(raw string) (FilterAction, error) {
	switch strings.ToUpper(raw) {
	case filterActionAllowStr:
		return FilterActionAllow, nil
	case filterActionDenyStr:
		return FilterActionDeny, nil
	default:
		return FilterActionUnknown, fmt.Errorf("unknown filter action: %q", raw)
	}
}

// String returns the name of the filter action.
func (a FilterAction) String() string {
	switch a {
	case FilterActionAllow:
		return filterActionAllowStr
	case FilterActionDeny:
		return filterActionDenyStr
	default:
		return "<unknown>"
	}
}

// ServerType specifies the behavior of a server.
type ServerType int

const (
	// ServerTypeUnknown is an invalid server type.
	ServerTypeUnknown ServerType = iota
	// ServerTypeTURN serves TURN: its filters admit the peers a client may reach.
	ServerTypeTURN
	// ServerTypeL4 forwards every client flow to an endpoint load-balanced over its clusters.
	ServerTypeL4
	// ServerTypeSIP is a stateful SIP proxy that hands the SDP of every call to a media proxy.
	ServerTypeSIP
	// ServerTypeRTP is a media proxy relaying the RTP and RTCP of the calls a SIP proxy sets up.
	ServerTypeRTP
	// ServerTypeSRT routes every SRT flow by the stream it names to the endpoint holding the
	// stream, and relays it.
	ServerTypeSRT
)

const (
	serverTypeTURNStr = "TURN"
	serverTypeL4Str   = "L4"
	serverTypeSIPStr  = "SIP"
	serverTypeRTPStr  = "RTP"
	serverTypeSRTStr  = "SRT"
)

// NewServerType parses a server type.
func NewServerType(raw string) (ServerType, error) {
	switch strings.ToUpper(raw) {
	case serverTypeTURNStr:
		return ServerTypeTURN, nil
	case serverTypeL4Str:
		return ServerTypeL4, nil
	case serverTypeSIPStr:
		return ServerTypeSIP, nil
	case serverTypeRTPStr:
		return ServerTypeRTP, nil
	case serverTypeSRTStr:
		return ServerTypeSRT, nil
	default:
		return ServerTypeUnknown, fmt.Errorf("unknown server type: %q", raw)
	}
}

// String returns the name of the server type.
func (t ServerType) String() string {
	switch t {
	case ServerTypeTURN:
		return serverTypeTURNStr
	case ServerTypeL4:
		return serverTypeL4Str
	case ServerTypeSIP:
		return serverTypeSIPStr
	case ServerTypeRTP:
		return serverTypeRTPStr
	case ServerTypeSRT:
		return serverTypeSRTStr
	default:
		return "<unknown>"
	}
}

// IsTURN returns true if the protocol relays through an upstream TURN server (TURN-UDP, TURN-TCP,
// TURN-TLS or TURN-DTLS).
func (p Protocol) IsTURN() bool {
	switch p {
	case ProtocolTURNUDP, ProtocolTURNTCP, ProtocolTURNTLS, ProtocolTURNDTLS:
		return true
	default:
		return false
	}
}

// OffloadMode specifies the type of TURN offload mode.
type OffloadMode int

const (
	OffloadEngineNone OffloadMode = iota
	OffloadEngineXDP
	OffloadEngineTC
	OffloadEngineAuto
)

const (
	offloadEngineNoneStr = "None"
	offloadEngineXDPStr  = "XDP"
	offloadEngineTCStr   = "TC"
	offloadEngineAutoStr = "Auto"
)

// NewOffloadEngine parses the offload mode.
func NewOffloadEngine(raw string) (OffloadMode, error) {
	switch strings.ToLower(raw) {
	case strings.ToLower(offloadEngineNoneStr):
		return OffloadEngineNone, nil
	case strings.ToLower(offloadEngineXDPStr):
		return OffloadEngineXDP, nil
	case strings.ToLower(offloadEngineTCStr):
		return OffloadEngineTC, nil
	case strings.ToLower(offloadEngineAutoStr):
		return OffloadEngineAuto, nil
	default:
		return OffloadEngineNone,
			fmt.Errorf("unknown offload mode: %q", raw)
	}
}

// String returns a string representation of an offload mode.
func (p OffloadMode) String() string {
	switch p {
	case OffloadEngineNone:
		return offloadEngineNoneStr
	case OffloadEngineXDP:
		return offloadEngineXDPStr
	case OffloadEngineTC:
		return offloadEngineTCStr
	case OffloadEngineAuto:
		return offloadEngineAutoStr
	default:
		return "<unknown>"
	}
}

// Stats is the traffic of a listener or cluster: all it carried, and the part of it the offload
// engine carried in the kernel.
type Stats struct {
	Total   DirStat `json:"total"`
	Offload DirStat `json:"offload"`
}

// String stringifies the traffic statistics, the offloaded part only when there is any.
func (s Stats) String() string {
	ret := "total(rx/tx): " + s.Total.String()
	if s.Offload != (DirStat{}) {
		ret += ",offload(rx/tx): " + s.Offload.String()
	}
	return ret
}

// DirStat is the RX and TX traffic of a listener or cluster.
type DirStat struct {
	Rx StatInfo `json:"rx"`
	Tx StatInfo `json:"tx"`
}

// String stringifies the traffic in both directions.
func (d DirStat) String() string {
	return fmt.Sprintf("%d/%d pkts %d/%d bytes", d.Rx.Pkts, d.Tx.Pkts, d.Rx.Bytes, d.Tx.Bytes)
}

// StatInfo is the traffic of a listener or cluster in one direction, and for the offloaded
// traffic the time it was last seen.
type StatInfo struct {
	Pkts          uint64 `json:"pkts"`
	Bytes         uint64 `json:"bytes"`
	TimestampLast uint64 `json:"timestamp,omitempty"`
}
