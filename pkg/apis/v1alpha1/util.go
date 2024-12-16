package v1alpha1

import (
	"fmt"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// AuthType species the type of the STUN/TURN authentication mechanism used by STUNner
type AuthType stnrv1.AuthType

const (
	AuthTypePlainText AuthType = AuthType(stnrv1.AuthTypeStatic)
	AuthTypeLongTerm  AuthType = AuthType(stnrv1.AuthTypeLongTerm)
	AuthTypeUnknown   AuthType = AuthType(stnrv1.AuthTypeNone)
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
type ListenerProtocol = stnrv1.ListenerProtocol

// ClusterType specifies the cluster address resolution policy
type ClusterType = stnrv1.ClusterType

// ClusterProtocol specifies the network protocol for a cluster
type ClusterProtocol = stnrv1.ClusterProtocol
