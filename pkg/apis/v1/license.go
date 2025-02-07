package v1

import (
	"fmt"
	"strings"
)

// LicenseStatus holds the licensing status.
type LicenseStatus struct {
	EnabledFeatures  []string `json:"enabledFeatures"`
	SubscriptionType string   `json:"subscriptionType"`
	ValidUntil       string   `json:"validUntil"`
	LastError        string   `json:"lastError"`
}

func (s LicenseStatus) String() string {
	if s.LastError != "" {
		return fmt.Sprintf("{error:%q}", s.LastError)
	} else {
		return fmt.Sprintf("{tier=%s,unlocked-features=%s,valid-until=%s}", s.SubscriptionType,
			strings.Join(s.EnabledFeatures, ","), s.ValidUntil)
	}
}

// Summary returns a stringified configuration.
func (s LicenseStatus) Summary() string {
	if s.LastError != "" {
		return fmt.Sprintf("License server error: %s", s.LastError)

	}
	features := "NONE"
	if len(s.EnabledFeatures) != 0 {
		features = strings.Join(s.EnabledFeatures, ", ")
	}
	return fmt.Sprintf("License status:\n\tSubscription type: %s\n\tEnabled features: %s\n\tNext renewal due: %s\n",
		s.SubscriptionType, features, s.ValidUntil)
}

func NewEmptyLicenseStatus() LicenseStatus {
	return LicenseStatus{
		EnabledFeatures:  []string{},
		SubscriptionType: "free",
		ValidUntil:       "N/A",
		LastError:        "",
	}
}
