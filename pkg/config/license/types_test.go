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

	assert.ElementsMatch(t, []Feature{FeatureUserQuota, FeatureDaemonSet,
		FeatureRelayAddressDiscovery, FeatureDualStack, FeatureHAOperator, FeaturePQC},
		Features(SubscriptionTypeMember), "the member tier changed")
	assert.NotContains(t, Features(SubscriptionTypeMember), FeatureTURNOffload,
		"offload is enterprise only")
	assert.ElementsMatch(t, AllFeatures(), Features(SubscriptionTypeEnterprise),
		"enterprise grants everything")
	assert.Empty(t, Features(SubscriptionTypeFree), "free grants nothing")

	for _, s := range AllSubscriptionTypes() {
		assert.Equal(t, s, NewSubscriptionType(s.String()), "%s does not parse back", s)
	}
	assert.Equal(t, FeatureNone, NewFeature("tcproute"), "a removed feature parses to none")
}

// TestLimitsSeam pins the open-source default of the limits, unknown on every tier, and that
// GetLimits reads the seam. The default is tested by name: a private build installs its own.
func TestLimitsSeam(t *testing.T) {
	unknown := Limits{SIPGateways: -1, Calls: -1, SRTGateways: -1, Streams: -1}
	for _, s := range AllSubscriptionTypes() {
		assert.Equal(t, unknown, unknownLimits(s), "%s", s)
	}
	assert.Equal(t, "sip=-/-,srt=-/-", unknown.String())

	defer func(f func(SubscriptionType) Limits) { getLimits = f }(getLimits)
	getLimits = func(SubscriptionType) Limits { return Limits{SIPGateways: 3, Calls: 100} }
	assert.Equal(t, "sip=3/100,srt=unlimited/unlimited", GetLimits(SubscriptionTypeMember).String(),
		"GetLimits reads the seam")
}
