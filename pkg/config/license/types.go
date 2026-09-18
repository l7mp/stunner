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

// AllSubscriptionTypesString returns all valid subscription types as a string slice.
func AllSubscriptionTypes() []SubscriptionType {
	return []SubscriptionType{SubscriptionTypeFree, SubscriptionTypeMember, SubscriptionTypeEnterprise}
}

// AllSubscriptionTypesString returns all valid features in a string slice.
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
	FeatureSTUNServer
	FeatureTCPRoute
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
	case "stunserver":
		return FeatureSTUNServer
	case "relayaddressdiscovery":
		return FeatureRelayAddressDiscovery
	case "tcproute":
		return FeatureTCPRoute
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
	case FeatureSTUNServer:
		return "STUNServer"
	case FeatureTCPRoute:
		return "TCPRoute"
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
	return []Feature{FeatureTURNOffload, FeatureUserQuota, FeatureDaemonSet, FeatureSTUNServer,
		FeatureRelayAddressDiscovery, FeatureTCPRoute, FeatureDualStack, FeatureHAOperator,
		FeaturePQC}
}

// AllFeaturesString returns all valid features in a string slice.
func AllFeaturesString() []string {
	return ToStringSlice(AllFeatures())
}

// Features returns the features a subscription type grants.
func Features(t SubscriptionType) []Feature {
	switch t {
	case SubscriptionTypeMember:
		return []Feature{FeatureUserQuota, FeatureDaemonSet, FeatureSTUNServer,
			FeatureRelayAddressDiscovery, FeatureTCPRoute, FeatureDualStack, FeatureHAOperator,
			FeaturePQC}
	case SubscriptionTypeEnterprise:
		return AllFeatures()
	default:
		return []Feature{}
	}
}

// helper
func ToStringSlice[T fmt.Stringer](items []T) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.String()
	}
	return result
}
