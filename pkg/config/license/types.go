package license

import (
	"fmt"
	"strconv"
	"strings"
)

// subscriptionType is an enum of known subscription types.
type subscriptionType int

var _ SubscriptionType = subscriptionType(0)

const (
	SubscriptionTypeNone subscriptionType = iota
	SubscriptionTypeFree
	SubscriptionTypeMember
	SubscriptionTypeEnterprise
)

// String stringifies a subscriptionType.
func (f subscriptionType) String() string {
	switch f {
	case SubscriptionTypeNone:
		return "none"
	case SubscriptionTypeFree:
		return "free"
	case SubscriptionTypeMember:
		return "member"
	case SubscriptionTypeEnterprise:
		return "enterprise"
	default:
		return "unknown"
	}
}

func NewSubscriptionType(f string) SubscriptionType {
	switch strings.ToLower(f) {
	case "enterprise":
		return SubscriptionTypeEnterprise
	case "member":
		return SubscriptionTypeMember
	case "free":
		return SubscriptionTypeFree
	default:
		return SubscriptionTypeNone
	}
}

// AllSubscriptionTypes returns all valid subscription types.
func AllSubscriptionTypes() []SubscriptionType {
	return []SubscriptionType{SubscriptionTypeFree, SubscriptionTypeMember, SubscriptionTypeEnterprise}
}

// AllSubscriptionTypesString returns all valid subscription types in a string slice.
func AllSubscriptionTypesString() []string {
	return ToStringSlice(AllSubscriptionTypes())
}

type feature int

var _ Feature = feature(0)

const (
	FeatureNone feature = iota
	FeatureKernelOffload
	FeatureUserQuota
	FeatureDaemonSet
	FeatureRelayAddressDiscovery
	FeatureDualStack
	FeatureHAOperator
	FeaturePQC
)

// NewFeature parses a string into an enum.
func NewFeature(f string) Feature {
	switch strings.ToLower(f) {
	case "kerneloffload":
		return FeatureKernelOffload
	case "userquota":
		return FeatureUserQuota
	case "daemonset":
		return FeatureDaemonSet
	case "relayaddressdiscovery":
		return FeatureRelayAddressDiscovery
	case "dualstack":
		return FeatureDualStack
	case "haoperator":
		return FeatureHAOperator
	case "pqc":
		return FeaturePQC
	case "none":
		fallthrough
	default:
		return FeatureNone
	}
}

// String stringifies a feature.
func (f feature) String() string {
	switch f {
	case FeatureNone:
		return "None"
	case FeatureKernelOffload:
		return "KernelOffload"
	case FeatureUserQuota:
		return "UserQuota"
	case FeatureDaemonSet:
		return "DaemonSet"
	case FeatureRelayAddressDiscovery:
		return "RelayAddressDiscovery"
	case FeatureDualStack:
		return "DualStack"
	case FeatureHAOperator:
		return "HAOperator"
	case FeaturePQC:
		return "PQC"
	default:
		return "unknown"
	}
}

// AllFeatures returns all defined valid features.
func AllFeatures() []Feature {
	return []Feature{FeatureKernelOffload, FeatureUserQuota, FeatureDaemonSet,
		FeatureRelayAddressDiscovery, FeatureDualStack, FeatureHAOperator, FeaturePQC}
}

// AllFeaturesString returns all valid features in a string slice.
func AllFeaturesString() []string {
	return ToStringSlice(AllFeatures())
}

// Features returns the features a subscription type grants. Every tier gets the kernel offload, up
// to the offload limit of the tier.
func Features(t SubscriptionType) []Feature {
	switch t {
	case SubscriptionTypeMember:
		return []Feature{FeatureKernelOffload, FeatureUserQuota, FeatureDaemonSet,
			FeatureRelayAddressDiscovery, FeatureDualStack, FeatureHAOperator, FeaturePQC}
	case SubscriptionTypeEnterprise:
		return AllFeatures()
	default:
		return []Feature{FeatureKernelOffload}
	}
}

// Limits are the usage limits of a subscription type: the SIP and SRT Gateways served, the
// parallel sessions of a Gateway (the calls of a SIP Gateway, the streams of an SRT Gateway), and
// the flows a dataplane pod offloads to the kernel (TURN channels, L4, SRT and RTP flows alike). 0
// means unlimited, a negative value unknown.
type Limits struct {
	SIPGateways int `json:"sipGateways"`
	SIPSessions int `json:"sipSessions"`
	SRTGateways int `json:"srtGateways"`
	SRTSessions int `json:"srtSessions"`
	Offloads    int `json:"offloads"`
}

// String returns the limits per protocol and the offloads all protocols share, e.g., "TURN:
// Gateways=unlimited: unlimited sessions, SIP: Gateways=3: 100 sessions, SRT: Gateways=3: 100
// sessions, total offloads: 100". TURN has no Gateway or session limit. A limit is a number,
// "unknown" or "unlimited".
func (l Limits) String() string {
	limit := func(n int) string {
		switch {
		case n < 0:
			return "unknown"
		case n == 0:
			return "unlimited"
		}
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("TURN: Gateways=unlimited: unlimited sessions, SIP: Gateways=%s: %s sessions, "+
		"SRT: Gateways=%s: %s sessions, total offloads: %s", limit(l.SIPGateways), limit(l.SIPSessions),
		limit(l.SRTGateways), limit(l.SRTSessions), limit(l.Offloads))
}

// getLimits returns the limits of a subscription type: unknownLimits by default.
var getLimits = unknownLimits

// unknownLimits are the limits of the open-source build, which does not know the limits of any
// tier.
func unknownLimits(_ SubscriptionType) Limits {
	return Limits{SIPGateways: -1, SIPSessions: -1, SRTGateways: -1, SRTSessions: -1, Offloads: -1}
}

// GetLimits returns the usage limits of a subscription type.
func GetLimits(t SubscriptionType) Limits { return getLimits(t) }

// helper
func ToStringSlice[T fmt.Stringer](items []T) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.String()
	}
	return result
}
