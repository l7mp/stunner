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

// ListenerProtocol specifies the network protocol for a listener.
type ListenerProtocol int

const (
	ListenerProtocolUnknown ListenerProtocol = iota
	ListenerProtocolUDP
	ListenerProtocolTCP
	ListenerProtocolTLS
	ListenerProtocolDTLS
	ListenerProtocolTURNUDP
	ListenerProtocolTURNTCP
	ListenerProtocolTURNTLS
	ListenerProtocolTURNDTLS
)

const (
	listenerProtocolUDPStr      = "UDP"
	listenerProtocolTCPStr      = "TCP"
	listenerProtocolTLSStr      = "TLS"
	listenerProtocolDTLSStr     = "DTLS"
	listenerProtocolTURNUDPStr  = "TURN-UDP"
	listenerProtocolTURNTCPStr  = "TURN-TCP"
	listenerProtocolTURNTLSStr  = "TURN-TLS"
	listenerProtocolTURNDTLSStr = "TURN-DTLS"
)

// NewListenerProtocol parses the protocol specification.
func NewListenerProtocol(raw string) (ListenerProtocol, error) {
	switch strings.ToUpper(raw) {
	case listenerProtocolUDPStr:
		return ListenerProtocolUDP, nil
	case listenerProtocolTCPStr:
		return ListenerProtocolTCP, nil
	case listenerProtocolTLSStr:
		return ListenerProtocolTLS, nil
	case listenerProtocolDTLSStr:
		return ListenerProtocolDTLS, nil
	case listenerProtocolTURNUDPStr:
		return ListenerProtocolTURNUDP, nil
	case listenerProtocolTURNTCPStr:
		return ListenerProtocolTURNTCP, nil
	case listenerProtocolTURNTLSStr:
		return ListenerProtocolTURNTLS, nil
	case listenerProtocolTURNDTLSStr:
		return ListenerProtocolTURNDTLS, nil
	default:
		return ListenerProtocol(ListenerProtocolUnknown),
			fmt.Errorf("unknown listener protocol: \"%s\"", raw)
	}
}

// String returns a string representation of a listener protocol.
func (l ListenerProtocol) String() string {
	switch l {
	case ListenerProtocolUDP:
		return listenerProtocolUDPStr
	case ListenerProtocolTCP:
		return listenerProtocolTCPStr
	case ListenerProtocolTLS:
		return listenerProtocolTLSStr
	case ListenerProtocolDTLS:
		return listenerProtocolDTLSStr
	case ListenerProtocolTURNUDP:
		return listenerProtocolTURNUDPStr
	case ListenerProtocolTURNTCP:
		return listenerProtocolTURNTCPStr
	case ListenerProtocolTURNTLS:
		return listenerProtocolTURNTLSStr
	case ListenerProtocolTURNDTLS:
		return listenerProtocolTURNDTLSStr
	default:
		return "<unknown>"
	}
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

// ClusterProtocol specifies the network protocol for a cluster.
type ClusterProtocol int

const (
	ClusterProtocolUDP ClusterProtocol = iota + 1
	ClusterProtocolTCP
	ClusterProtocolUnknown
)

const (
	clusterProtocolUDPStr = "UDP"
	clusterProtocolTCPStr = "TCP"
)

// NewClusterProtocol parses the protocol specification.
func NewClusterProtocol(raw string) (ClusterProtocol, error) {
	switch strings.ToUpper(raw) {
	case clusterProtocolUDPStr:
		return ClusterProtocolUDP, nil
	case clusterProtocolTCPStr:
		return ClusterProtocolTCP, nil
	default:
		return ClusterProtocol(ClusterProtocolUnknown),
			fmt.Errorf("unknown cluster protocol: \"%s\"", raw)
	}
}

// String returns a string representation of a cluster protocol.
func (p ClusterProtocol) String() string {
	switch p {
	case ClusterProtocolUDP:
		return clusterProtocolUDPStr
	case ClusterProtocolTCP:
		return clusterProtocolTCPStr
	default:
		return "<unknown>"
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

type StatType int

const (
	// ListenerStat is a marker used to signal that the caller wants the listener statistics.
	ListenerStat StatType = iota + 1
	// ClusterStat is a marker used to signal that the caller wants the cluster statistics.
	ClusterStat
)

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
