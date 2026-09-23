package v1

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

// PQCMode selects the post-quantum key exchange policy of a TURN-TLS listener. Honouring it is a
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
// which subset is valid in a given context (listener, cluster, ...) is enforced at the use site, not
// by this type. Parse a protocol name with NewProtocol; check context validity with the
// IsListenerProtocol/IsClusterProtocol predicates.
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
// protocol token and errors only on an unrecognized string. Whether the result is admissible in a
// particular context is a separate question, answered by the IsListenerProtocol/IsClusterProtocol
// predicates.
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

// ListenerProtocol is an alias for Protocol, retained for backward compatibility.
type ListenerProtocol = Protocol

const (
	ListenerProtocolUnknown  = ProtocolUnknown
	ListenerProtocolUDP      = ProtocolUDP
	ListenerProtocolTCP      = ProtocolTCP
	ListenerProtocolTLS      = ProtocolTLS
	ListenerProtocolDTLS     = ProtocolDTLS
	ListenerProtocolTURNUDP  = ProtocolTURNUDP
	ListenerProtocolTURNTCP  = ProtocolTURNTCP
	ListenerProtocolTURNTLS  = ProtocolTURNTLS
	ListenerProtocolTURNDTLS = ProtocolTURNDTLS
)

// NewListenerProtocol parses a protocol name and rejects any protocol that is not valid for a
// listener.
func NewListenerProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil {
		switch p {
		case ProtocolUDP, ProtocolTCP, ProtocolTLS, ProtocolDTLS, ProtocolSTDIN,
			ProtocolTURNUDP, ProtocolTURNTCP, ProtocolTURNTLS, ProtocolTURNDTLS:
			return p, nil
		}
	}
	return ProtocolUnknown, fmt.Errorf("unknown listener protocol: \"%s\"", raw)
}

// ClusterType specifies the cluster address resolution policy.
type ClusterType int

const (
	ClusterTypeStatic ClusterType = iota + 1
	ClusterTypeStrictDNS
	ClusterTypeUnknown
)

const (
	clusterTypeStaticStr    = "STATIC"
	clusterTypeStrictDNSStr = "STRICT_DNS"
)

func NewClusterType(raw string) (ClusterType, error) {
	switch strings.ToUpper(raw) {
	case clusterTypeStaticStr:
		return ClusterTypeStatic, nil
	case clusterTypeStrictDNSStr:
		return ClusterTypeStrictDNS, nil
	default:
		return ClusterType(ClusterTypeUnknown),
			fmt.Errorf("unknown cluster type: \"%s\"", raw)
	}
}

func (l ClusterType) String() string {
	switch l {
	case ClusterTypeStatic:
		return clusterTypeStaticStr
	case ClusterTypeStrictDNS:
		return clusterTypeStrictDNSStr
	default:
		return "<unknown>"
	}
}

// ClusterProtocol is an alias for Protocol, retained for backward compatibility.
type ClusterProtocol = Protocol

const (
	ClusterProtocolUDP      = ProtocolUDP
	ClusterProtocolTCP      = ProtocolTCP
	ClusterProtocolTURNUDP  = ProtocolTURNUDP
	ClusterProtocolTURNTCP  = ProtocolTURNTCP
	ClusterProtocolTURNTLS  = ProtocolTURNTLS
	ClusterProtocolTURNDTLS = ProtocolTURNDTLS
	ClusterProtocolUnknown  = ProtocolUnknown
)

// NewClusterProtocol parses a protocol name and rejects any protocol that is not valid for a
// cluster (backend). Direct clusters relay to the peers over plain UDP or TCP; TURN-* clusters
// relay through an upstream TURN server reached over the transport encoded in the protocol.
func NewClusterProtocol(raw string) (Protocol, error) {
	if p, err := NewProtocol(raw); err == nil {
		switch p {
		case ProtocolUDP, ProtocolTCP,
			ProtocolTURNUDP, ProtocolTURNTCP, ProtocolTURNTLS, ProtocolTURNDTLS:
			return p, nil
		}
	}
	return ProtocolUnknown, fmt.Errorf("unknown cluster protocol: \"%s\"", raw)
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

// OffloadStatus holds offload runtime status and traffic counters.
type OffloadStatus struct {
	Engine     string                    `json:"engine,omitempty"`
	Interfaces []string                  `json:"interfaces,omitempty"`
	Listeners  map[string]OffloadDirStat `json:"listeners,omitempty"`
	Clusters   map[string]OffloadDirStat `json:"clusters,omitempty"`
}

// String stringifies the offload status.
func (s *OffloadStatus) String() string {
	if s == nil {
		return "offload:{}"
	}
	return fmt.Sprintf("offload:{engine=%q,interfaces=[%s],listeners=%d,clusters=%d}",
		s.Engine, strings.Join(s.Interfaces, ","), len(s.Listeners), len(s.Clusters))
}
