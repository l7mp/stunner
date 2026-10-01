package v2

import (
	"fmt"
	"strings"
)

// AuthType species the type of the STUN/TURN authentication mechanism used by STUNner.
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
	AuthTypePlainText    = AuthTypeStatic
	AuthTypeLongTerm     = AuthTypeEphemeral
	authTypePlainTextStr = "plaintext"
	authTypeLongTermStr  = "longterm"
)

// NewAuthType parses the authentication mechanism specification.
func NewAuthType(raw string) (AuthType, error) {
	switch raw {
	case authTypeStaticStr, authTypePlainTextStr:
		return AuthTypeStatic, nil
	case authTypeEphemeralStr, authTypeLongTermStr:
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

// PQCMode selects the post-quantum key exchange policy of a TLS listener. Honouring it is a
// premium feature: a build without it serves the default TLS settings whatever the mode says.
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
	default:
		return "<unknown>"
	}
}

// NewListenerProtocol parses a listener protocol: UDP, TCP, TLS, DTLS or STDIN.
func NewListenerProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil {
		switch p {
		case ProtocolUDP, ProtocolTCP, ProtocolTLS, ProtocolDTLS, ProtocolSTDIN:
			return p, nil
		}
	}
	return ProtocolUnknown, fmt.Errorf("unknown listener protocol: %q", raw)
}

// NewClusterProtocol parses a cluster protocol: UDP or TCP, the network the peers are reached over.
func NewClusterProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil && (p == ProtocolUDP || p == ProtocolTCP) {
		return p, nil
	}
	return ProtocolUnknown, fmt.Errorf("unknown cluster protocol: %q", raw)
}

// ClusterType specifies how the endpoints of a cluster are resolved.
type ClusterType int

const (
	// ClusterTypeStatic endpoints are IP addresses and prefixes.
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

// RoutingPolicy is how a cluster routes traffic: it filters the destinations its clients name,
// or it chooses one with a load-balancing policy, named as in Envoy.
type RoutingPolicy int

const (
	// RoutingPolicyUnknown is an invalid policy.
	RoutingPolicyUnknown RoutingPolicy = iota
	// RoutingPolicyFilter admits the destinations the endpoints contain.
	RoutingPolicyFilter
	// RoutingPolicyRoundRobin chooses the endpoints in turn.
	RoutingPolicyRoundRobin
)

const (
	routingPolicyFilterStr     = "FILTER"
	routingPolicyRoundRobinStr = "ROUND_ROBIN"
)

// NewRoutingPolicy parses a routing policy.
func NewRoutingPolicy(raw string) (RoutingPolicy, error) {
	switch strings.ToUpper(raw) {
	case routingPolicyFilterStr:
		return RoutingPolicyFilter, nil
	case routingPolicyRoundRobinStr:
		return RoutingPolicyRoundRobin, nil
	default:
		return RoutingPolicyUnknown, fmt.Errorf("unknown routing policy: %q", raw)
	}
}

// String returns the name of the routing policy.
func (p RoutingPolicy) String() string {
	switch p {
	case RoutingPolicyFilter:
		return routingPolicyFilterStr
	case RoutingPolicyRoundRobin:
		return routingPolicyRoundRobinStr
	default:
		return "<unknown>"
	}
}

// ServerType specifies the behavior of a server.
type ServerType int

const (
	// ServerTypeUnknown is an invalid server type.
	ServerTypeUnknown ServerType = iota
	// ServerTypeTURN serves TURN: clusters filter the peers a client may reach.
	ServerTypeTURN
	// ServerTypeL4 forwards every client flow to an endpoint load-balanced over its clusters.
	ServerTypeL4
)

const (
	serverTypeTURNStr = "turn"
	serverTypeL4Str   = "l4"
)

// NewServerType parses a server type.
func NewServerType(raw string) (ServerType, error) {
	switch strings.ToLower(raw) {
	case serverTypeTURNStr:
		return ServerTypeTURN, nil
	case serverTypeL4Str:
		return ServerTypeL4, nil
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

// OffloadEngine specifies the type of TURN offload mode.
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

// String returns a string representation of a cluster protocol.
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

// OffloadStatMap defines the TX/RX offload statistics for a particular listener or cluster.
type OffloadDirStat struct {
	Rx OffloadStatInfo `json:"rx"`
	Tx OffloadStatInfo `json:"tx"`
}

// OffloadStatInfo holds the statistics for a listener or cluster in RX or TX direction.
type OffloadStatInfo struct {
	Pkts          uint64 `json:"pkts"`
	Bytes         uint64 `json:"bytes"`
	TimestampLast uint64 `json:"timestamp"`
}
