package v1alpha1

import (
	"fmt"
	"strings"
)

// AuthType species the type of the STUN/TURN authentication mechanism used by STUNner
type AuthType int

const (
	AuthTypePlainText AuthType = iota + 1
	AuthTypeLongTerm
	AuthTypeUnknown
)

const (
	authTypePlainTextStr = "plaintext"
	authTypeLongTermStr  = "longterm"
)

// NewAuthType parses the authentication mechanism specification
func NewAuthType(raw string) (AuthType, error) {
	switch raw {
	case authTypePlainTextStr:
		return AuthTypePlainText, nil
	case authTypeLongTermStr:
		return AuthTypeLongTerm, nil
	default:
		return AuthTypeUnknown, fmt.Errorf("unknown authentication type: \"%s\"", raw)
	}
}

// String returns a string representation for the authentication mechanism
func (a AuthType) String() string {
	switch a {
	case AuthTypePlainText:
		return authTypePlainTextStr
	case AuthTypeLongTerm:
		return authTypeLongTermStr
	default:
		return "<unknown>"
	}
}

// ListenerProtocol specifies the network protocol for a listener
type ListenerProtocol int

const (
	ListenerProtocolUDP ListenerProtocol = iota + 1
	ListenerProtocolTCP
	ListenerProtocolTLS
	ListenerProtocolDTLS
	ListenerProtocolUnknown
)

const (
	listenerProtocolUDPStr  = "UDP"
	listenerProtocolTCPStr  = "TCP"
	listenerProtocolTLSStr  = "TLS"
	listenerProtocolDTLSStr = "DTLS"
)

// NewListenerProtocol parses the protocol specification
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
	default:
		return ListenerProtocol(ListenerProtocolUnknown),
			fmt.Errorf("unknown listener protocol: \"%s\"", raw)
	}
}

// String returns a string representation of a listener protocol
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
	default:
		return "<unknown>"
	}
}

// ClusterType specifies the cluster address resolution policy
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
		return ClusterType(ClusterTypeUnknown), fmt.Errorf("unknown cluster type: \"%s\"", raw)
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

// ClusterProtocol specifies the network protocol for a cluster
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

// NewClusterProtocol parses the protocol specification
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

// String returns a string representation of a cluster protocol
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
