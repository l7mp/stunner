package v1

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// AdminConfig holds the administrative configuration.
type AdminConfig struct {
	// Name of the server. Default is "default-stunnerd".
	Name string `json:"name,omitempty"`
	// LogLevel is the desired log verbosity, e.g.: "stunner:TRACE,all:INFO". Default is
	// "all:INFO".
	LogLevel string `json:"loglevel,omitempty"`
	// MetricsEndpoint is the URI in the form `http://address:port/path` at which HTTP metric
	// requests are served. The scheme (`http://`") is mandatory. Default is to expose no
	// metric endpoints.
	MetricsEndpoint string `json:"metrics_endpoint,omitempty"`
	// HealthCheckEndpoint is the URI of the form `http://address:port` exposed for external
	// HTTP health-checking. A liveness probe responder will be exposed on path `/live` and
	// readiness probe on path `/ready`. The scheme (`http://`) is mandatory, and if no port is
	// specified then the default port is 8086. If ignored, then the default is to enable
	// health-checking at `http://0.0.0.0:8086`. Set to a pointer to an empty string to disable
	// health-checking.
	HealthCheckEndpoint *string `json:"healthcheck_endpoint,omitempty"`
	// UserQuota defines the number of permitted TURN allocatoins per username. Affects
	// allocation created on any listener. Default is 0, meaning no quota is enforced.
	UserQuota int `json:"user_quota,omitempty"`
	// OffloadEngine defines the dataplane offload mode, either "None", "XDP", "TC", or
	// "Auto". Set to "Auto" to let STUNner find the optimal offload mode. Default is "None".
	OffloadEngine string `json:"offload_engine,omitempty"`
	// OffloadInterfaces explicitly specifies the interfaces on which to enable the offload
	// engine. Empty list means to enable offload on all interfaces (this is the default).
	OffloadInterfaces []string `json:"offload_interfaces,omitempty"`
	// LicenseConfig describes the licensing info to be used to check subscription status with
	// the license server.
	LicenseConfig *LicenseConfig `json:"license_config,omitempty"`
}

// Licensing info to be used to check subscription status with the license server.
type LicenseConfig struct {
	// Key is a comma-separated list of unlocked features plus a time-window during which the
	// key is considered valid.
	Key string `json:"key"`
	// HMAC is a hash-based message authentication code for validating the license key.
	HMAC string `json:"hmac"`
}

// Validate checks a configuration and injects defaults.
func (req *AdminConfig) Validate() error {
	if req.LogLevel == "" {
		req.LogLevel = DefaultLogLevel
	}

	if req.Name == "" {
		req.Name = DefaultStunnerName
	}

	if req.MetricsEndpoint != "" {
		//Metrics endpoint set: validate. The empty string is valid
		if _, err := url.Parse(req.MetricsEndpoint); err != nil {
			return fmt.Errorf("invalid metric server endpoint URL %s: %s",
				req.MetricsEndpoint, err.Error())
		}
	}

	if req.HealthCheckEndpoint == nil {
		// No healtchcheck endpoint given: use default URL
		e := fmt.Sprintf("http://:%d", DefaultHealthCheckPort)
		req.HealthCheckEndpoint = &e
	} else {
		// Healtcheck endpoint set: validate. Empty string is valid
		if _, err := url.Parse(*req.HealthCheckEndpoint); err != nil {
			return fmt.Errorf("invalid health-check server endpoint URL %s: %s",
				*req.HealthCheckEndpoint, err.Error())
		}
	}

	if req.UserQuota < 0 {
		req.UserQuota = 0
	}

	// Normalize
	if req.OffloadEngine == "" {
		req.OffloadEngine = OffloadEngineNone.String()
	}
	t, err := NewOffloadEngine(req.OffloadEngine)
	if err != nil {
		return err
	}
	req.OffloadEngine = t.String()

	if req.OffloadInterfaces == nil {
		req.OffloadInterfaces = []string{}
	}
	sort.Strings(req.OffloadInterfaces)

	return nil
}

// Name returns the name of the object to be configured.
func (req *AdminConfig) ConfigName() string {
	// Singleton!
	return DefaultAdminName
}

// DeepEqual compares two configurations.
func (req *AdminConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *AdminConfig) DeepCopyInto(dst Config) {
	ret := dst.(*AdminConfig)
	*ret = *req
	ret.OffloadInterfaces = make([]string, len(req.OffloadInterfaces))
	copy(ret.OffloadInterfaces, req.OffloadInterfaces)
}

// String stringifies the configuration.
func (req *AdminConfig) String() string {
	status := []string{}
	if req.Name != "" {
		status = append(status, fmt.Sprintf("name=%q", req.Name))
	}
	if req.LogLevel != "" {
		status = append(status, fmt.Sprintf("logLevel=%q", req.LogLevel))
	}
	if req.MetricsEndpoint != "" {
		status = append(status, fmt.Sprintf("metrics=%q", req.MetricsEndpoint))
	}
	if req.HealthCheckEndpoint != nil {
		status = append(status, fmt.Sprintf("health-check=%q", *req.HealthCheckEndpoint))
	}
	if req.UserQuota > 0 {
		status = append(status, fmt.Sprintf("quota=%d", req.UserQuota))
	}
	if req.OffloadEngine != "" {
		intfs := "all"
		if req.OffloadEngine != "None" && len(req.OffloadInterfaces) > 0 {
			intfs = fmt.Sprintf("<%s>", strings.Join(req.OffloadInterfaces, ","))
		}
		status = append(status, fmt.Sprintf("offload=%s[%s]", req.OffloadEngine, intfs))
	}
	status = append(status, fmt.Sprintf("license_info=%s", LicensingStatus(req.LicenseConfig)))

	return fmt.Sprintf("admin:{%s}", strings.Join(status, ","))
}

func LicensingStatus(req *LicenseConfig) string {
	if req != nil {
		key := "<MISSING>"
		if req != nil {
			key = req.Key
		}
		pass := "<MISSING>"
		if req != nil {
			pass = "<SECRET>"
		}
		return fmt.Sprintf("{key=%s,pass=%s}", key, pass)
	}

	return "<MISSING>"
}

// AdminStatus represents the administrative status.
type AdminStatus struct {
	Name                string `json:"name,omitempty"`
	LogLevel            string `json:"loglevel,omitempty"`
	MetricsEndpoint     string `json:"metrics_endpoint,omitempty"`
	HealthCheckEndpoint string `json:"healthcheck_endpoint,omitempty"`
	UserQuota           string `json:"quota,omitempty"`
	OffloadStatus       string `json:"offload,omitempty"`
	LicensingInfo       string `json:"licensing_info,omitempty"`
}

// String returns a string reprsentation of the administrative status.
func (a *AdminStatus) String() string {
	status := []string{fmt.Sprintf("id=%q", a.Name)}
	if a.LogLevel != "" {
		status = append(status, fmt.Sprintf("logLevel=%q", a.LogLevel))
	}
	if a.MetricsEndpoint != "" {
		status = append(status, fmt.Sprintf("metrics=%q", a.MetricsEndpoint))
	}
	if a.HealthCheckEndpoint != "" {
		status = append(status, fmt.Sprintf("health-check=%q", a.HealthCheckEndpoint))
	}
	status = append(status, fmt.Sprintf("quota=%s", a.UserQuota))
	if a.LicensingInfo != "" {
		status = append(status, fmt.Sprintf("license-info=%s", a.LicensingInfo))
	}
	if a.OffloadStatus != "" {
		status = append(status, fmt.Sprintf("offload=%s", a.OffloadStatus))
	}

	return fmt.Sprintf("admin:{%s}", strings.Join(status, ","))
}
