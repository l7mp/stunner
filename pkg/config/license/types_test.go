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
	assert.Len(t, defined, 9, "the feature enum grew or shrank")

	for _, f := range defined {
		assert.Equal(t, f, NewFeature(f.String()),
			"%s does not parse back from its own name, so a license key carrying it decodes to "+
				"FeatureNone and the feature silently disappears", f)
		assert.Contains(t, AllFeatures(), f, "%s is missing from AllFeatures", f)
		assert.Contains(t, Features(SubscriptionTypeEnterprise), f,
			"%s is granted by no tier")
	}

	assert.ElementsMatch(t, []Feature{FeatureUserQuota, FeatureDaemonSet, FeatureSTUNServer,
		FeatureRelayAddressDiscovery, FeatureTCPRoute, FeatureDualStack, FeatureHAOperator,
		FeaturePQC},
		Features(SubscriptionTypeMember), "the member tier changed")
	assert.NotContains(t, Features(SubscriptionTypeMember), FeatureTURNOffload,
		"offload is enterprise only")
	assert.ElementsMatch(t, AllFeatures(), Features(SubscriptionTypeEnterprise),
		"enterprise grants everything")
	assert.Empty(t, Features(SubscriptionTypeFree), "free grants nothing")

	for _, s := range AllSubscriptionTypes() {
		assert.Equal(t, s, NewSubscriptionType(s.String()), "%s does not parse back", s)
	}
}
