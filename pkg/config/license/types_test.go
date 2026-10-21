package license

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFeatureCatalog(t *testing.T) {
	// every ordinal the enum defines, discovered rather than listed
	var defined []Feature
	for i := 1; ; i++ {
		f := feature(i)
		if f.String() == "unknown" {
			break
		}
		defined = append(defined, f)
	}
	assert.Len(t, defined, 7, "the feature enum grew or shrank")

	for _, f := range defined {
		assert.Equal(t, f, NewFeature(f.String()),
			"%s does not parse back from its own name, so a license key carrying it decodes to "+
				"FeatureNone and the feature silently disappears", f)
		assert.Contains(t, AllFeatures(), f, "%s is missing from AllFeatures", f)
		assert.Contains(t, Features(SubscriptionTypeEnterprise), f,
			"%s is granted by no tier", f)
	}

	assert.ElementsMatch(t, []Feature{FeatureKernelOffload, FeatureUserQuota, FeatureDaemonSet,
		FeatureRelayAddressDiscovery, FeatureDualStack, FeatureHAOperator, FeaturePQC},
		Features(SubscriptionTypeMember), "the member tier changed")
	assert.ElementsMatch(t, AllFeatures(), Features(SubscriptionTypeEnterprise),
		"enterprise grants everything")
	assert.Equal(t, []Feature{FeatureKernelOffload}, Features(SubscriptionTypeFree),
		"free grants the kernel offload only, up to its limit")

	for _, s := range AllSubscriptionTypes() {
		assert.Equal(t, s, NewSubscriptionType(s.String()), "%s does not parse back", s)
	}
	assert.Equal(t, FeatureNone, NewFeature("tcproute"), "a removed feature parses to none")
	assert.Equal(t, FeatureNone, NewFeature("turnoffload"), "the renamed TURN offload parses to none")
}

// TestLimitsSeam pins the open-source default of the limits, unknown on every tier, and that
// GetLimits reads the seam. The default is tested by name: a private build installs its own.
func TestLimitsSeam(t *testing.T) {
	unknown := Limits{SIPGateways: -1, SIPSessions: -1, SRTGateways: -1, SRTSessions: -1, Offloads: -1}
	for _, s := range AllSubscriptionTypes() {
		assert.Equal(t, unknown, unknownLimits(s), "%s", s)
	}
	assert.Equal(t, "TURN: Gateways=unlimited: unlimited sessions, SIP: Gateways=unknown: unknown sessions, "+
		"SRT: Gateways=unknown: unknown sessions, total offloads: unknown", unknown.String())

	defer func(f func(SubscriptionType) Limits) { getLimits = f }(getLimits)
	getLimits = func(SubscriptionType) Limits { return Limits{SIPGateways: 3, SIPSessions: 100, Offloads: 100} }
	assert.Equal(t, "TURN: Gateways=unlimited: unlimited sessions, SIP: Gateways=3: 100 sessions, "+
		"SRT: Gateways=unlimited: unlimited sessions, total offloads: 100", GetLimits(SubscriptionTypeMember).String(),
		"GetLimits reads the seam")
}
