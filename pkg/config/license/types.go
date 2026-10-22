package license

import (
	"fmt"
	"strings"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
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
	// FeatureKernelOffload is retired: the license keys in the field carry it, so it parses, but
	// no tier grants it and nothing checks it; the offload is held to the offload limit of the
	// tier (Limits) alone.
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
	return []Feature{FeatureUserQuota, FeatureDaemonSet, FeatureRelayAddressDiscovery,
		FeatureDualStack, FeatureHAOperator, FeaturePQC}
}

// AllFeaturesString returns all valid features in a string slice.
func AllFeaturesString() []string {
	return ToStringSlice(AllFeatures())
}

// Features returns the features a subscription type grants. What a tier may use of a limited
// premium service (SIP and SRT Gateways and sessions, the kernel offload) is its Limits.
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

// Limits are the usage limits of a subscription type, as the license status carries them.
type Limits = stnrv2.LicenseLimits

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
