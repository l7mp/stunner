package v2

import (
	"fmt"
	"strconv"
	"strings"
)

// LicenseStatus holds the licensing status.
type LicenseStatus struct {
	EnabledFeatures  []string `json:"enabledFeatures"`
	SubscriptionType string   `json:"subscriptionType"`
	// Limits are the usage limits of the subscription, nil when the reporter knows none.
	Limits      *LicenseLimits `json:"limits,omitempty"`
	LastUpdated string         `json:"lastUpdated"`
	LastError   string         `json:"lastError"`
}

func (s LicenseStatus) String() string {
	if s.LastError != "" {
		return fmt.Sprintf("{error:%q}", s.LastError)
	} else {
		limits := ""
		if s.Limits != nil {
			limits = fmt.Sprintf(",limits={%s}", s.Limits.String())
		}
		return fmt.Sprintf("{tier=%s,unlocked-features=%s%s,last-updated=%s}", s.SubscriptionType,
			strings.Join(s.EnabledFeatures, ","), limits, s.LastUpdated)
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
	limits := ""
	if s.Limits != nil {
		limits = fmt.Sprintf("\tLimits: %s\n", s.Limits.String())
	}
	return fmt.Sprintf("License status:\n\tSubscription type: %s\n\tEnabled features: %s\n%s\tLast updated: %s\n",
		s.SubscriptionType, features, limits, s.LastUpdated)
}

func NewEmptyLicenseStatus() LicenseStatus {
	return LicenseStatus{
		EnabledFeatures:  []string{},
		SubscriptionType: "free",
		LastUpdated:      "N/A",
		LastError:        "",
	}
}

// LicenseLimits are the usage limits of a subscription type: the SIP and SRT Gateways served, the
// parallel sessions of a Gateway (the calls of a SIP Gateway, the streams of an SRT Gateway), and
// the flows a dataplane pod offloads to the kernel (TURN channels, L4, SRT and RTP flows alike). 0
// means unlimited, a negative value unknown.
type LicenseLimits struct {
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
func (l LicenseLimits) String() string {
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
