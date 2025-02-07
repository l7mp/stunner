package v1

import (
	"fmt"
	"strings"
)

// LicenseStatus holds the licensing status.
type LicenseStatus struct {
	EnabledFeatures  []string `json:"enabledFeatures"`
	SubscriptionType string   `json:"subscriptionType"`
	LastUpdated      string   `json:"lastUpdated"`
	LastError        string   `json:"lastError"`
}

func (s LicenseStatus) String() string {
	if s.LastError != "" {
		return fmt.Sprintf("{error:%q}", s.LastError)
	} else {
		return fmt.Sprintf("{tier=%s,unlocked-features=%s,last-updated=%s}", s.SubscriptionType,
			strings.Join(s.EnabledFeatures, ","), s.LastUpdated)
	}
}

// Summary returns a stringified configuration.
func (s LicenseStatus) Summary() string {
	if s.LastError != "" {
		return fmt.Sprintf("License server error: %s (last updated: %s)\n", s.LastError, s.LastUpdated)

	}
	features := "NONE"
	if len(s.EnabledFeatures) != 0 {
		features = strings.Join(s.EnabledFeatures, ", ")
	}
	return fmt.Sprintf("License status:\n\tSubscription type: %s\n\tEnabled features: %s\n\tLast updated: %s\n",
		s.SubscriptionType, features, s.LastUpdated)
}

func NewEmptyLicenseStatus() LicenseStatus {
	return LicenseStatus{
		EnabledFeatures:  []string{},
		SubscriptionType: "free",
		LastUpdated:      "N/A",
		LastError:        "",
	}
}
