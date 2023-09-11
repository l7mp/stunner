package v1alpha1

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

// AdminConfig holds the administrative configuration.
type AdminConfig struct {
	// Name is the name of the server, optional.
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
	// specified then the default port is 8086. If pointer value nil then the default is to
	// enable health-checking at `http://0.0.0.0:8086`, set to a pointer to an enpty string if
	// you want to disable health-checking.
	HealthCheckEndpoint *string `json:"healthcheck_endpoint,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *AdminConfig) Validate() error {
	//FIXME: no validation for loglevel (we'd need to create a new logger and it's not worth)
	if req.LogLevel == "" {
		req.LogLevel = DefaultLogLevel
	}

	if req.Name == "" {
		req.Name = DefaultStunnerName
	}

	if req.MetricsEndpoint != "" {
		//metrics endpoint set: validate. empty string is valid
		if _, err := url.Parse(req.MetricsEndpoint); err != nil {
			return fmt.Errorf("invalid metric server endpoint URL %s: %s",
				req.MetricsEndpoint, err.Error())
		}
	}

	if req.HealthCheckEndpoint == nil {
		// no healtchcheck endpoint given: use default URL
		e := fmt.Sprintf("http://:%d", DefaultHealthCheckPort)
		req.HealthCheckEndpoint = &e
	} else {
		//healtcheck endpoint set: validate. empty string is valid
		if _, err := url.Parse(*req.HealthCheckEndpoint); err != nil {
			return fmt.Errorf("invalid health-check server endpoint URL %s: %s",
				*req.HealthCheckEndpoint, err.Error())
		}
	}

	return nil
}

// Name returns the name of the object to be configured.
func (req *AdminConfig) ConfigName() string {
	// singleton!
	return DefaultAdminName
}

// DeepEqual compares two configurations.
func (req *AdminConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *AdminConfig) DeepCopyInto(dst Config) {
	ret := dst.(*AdminConfig)
	// admin conf contians primitive types only so this is safe
	*ret = *req
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
	return fmt.Sprintf("admin:{%s}", strings.Join(status, ","))
}
