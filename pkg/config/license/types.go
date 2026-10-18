package license

import (
	"fmt"
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
	FeatureTURNOffload
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
	case "turnoffload":
		return FeatureTURNOffload
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
	case FeatureTURNOffload:
		return "TURNOffload"
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
	return []Feature{FeatureTURNOffload, FeatureUserQuota, FeatureDaemonSet,
		FeatureRelayAddressDiscovery, FeatureDualStack, FeatureHAOperator, FeaturePQC}
}

// AllFeaturesString returns all valid features in a string slice.
func AllFeaturesString() []string {
	return ToStringSlice(AllFeatures())
}

// Features returns the features a subscription type grants.
func Features(t SubscriptionType) []Feature {
	switch t {
	case SubscriptionTypeMember:
		return []Feature{FeatureUserQuota, FeatureDaemonSet, FeatureRelayAddressDiscovery,
			FeatureDualStack, FeatureHAOperator, FeaturePQC}
	case SubscriptionTypeEnterprise:
		return AllFeatures()
	default:
		return []Feature{}
	}
}

// Limits are the usage limits of a subscription type; 0 means unlimited.
type Limits struct {
	// SIPGateways is the number of Gateways the operator renders a SIP gateway for.
	SIPGateways int
	// Calls is the number of parallel calls of a SIP gateway.
	Calls int
}

// GetLimits returns the usage limits of a subscription type. The SIP gateway is experimental, so
// every tier has the same limits.
func GetLimits(_ SubscriptionType) Limits {
	return Limits{SIPGateways: 1, Calls: 5}
}

// helper
func ToStringSlice[T fmt.Stringer](items []T) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.String()
	}
	return result
}
