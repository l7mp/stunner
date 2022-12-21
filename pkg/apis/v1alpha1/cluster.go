package v1alpha1

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ClusterConfig specifies a set of upstream peers STUNner can open transport relay connections
// to. There are two address resolution policies. In STATIC clusters the allowed peer IP addresses
// are explicitly listed in the endpoint list. In STRICT_DNS clusters the endpoints are assumed to
// be proper DNS domain names. STUNner will resolve each domain name in the background and admits a
// new connection only if the peer address matches one of the IP addresses returned by the DNS
// resolver for one of the endpoints. STRICT_DNS clusters are best used with headless Kubernetes
// services.
type ClusterConfig struct {
	// Name is the name of the cluster.
	Name string `json:"name"`
	// Type specifies the cluster address resolution policy, either STATIC or STRICT_DNS.
	Type string `json:"type,omitempty"`
	// Protocol specifies the protocol to be used with the cluster, either UDP (default) or TCP
	// (not implemented yet).
	Protocol string `json:"protocol,omitempty"`
	// Endpoints specifies the peers that can be reached via this cluster.
	Endpoints []string `json:"endpoints,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *ClusterConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in cluster configuration: %s", req.String())
	}

	if req.Type == "" {
		req.Type = DefaultClusterType
	}
	t, err := NewClusterType(req.Type)
	if err != nil {
		return err
	}
	req.Type = t.String() // normalize

	if req.Protocol == "" {
		req.Protocol = DefaultClusterProtocol
	}
	p, err := NewClusterProtocol(req.Protocol)
	if err != nil {
		return err
	}
	req.Protocol = p.String() // normalize

	sort.Strings(req.Endpoints)
	return nil
}

// Name returns the name of the object to be configured.
func (req *ClusterConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *ClusterConfig) DeepEqual(other Config) bool {
	// endpoints must be sorted in both configs!
	return reflect.DeepEqual(req, other)
}

// String stringifies the configuration.
func (req *ClusterConfig) String() string {
	status := []string{}

	n := "-"
	if req.Name != "" {
		n = req.Name
	}

	if req.Type != "" {
		status = append(status, fmt.Sprintf("type=%q", req.Type))
	}

	if req.Protocol != "" {
		status = append(status, fmt.Sprintf("protocol=%q", req.Protocol))
	}

	status = append(status, fmt.Sprintf("endpoints=[%s]",
		strings.Join(req.Endpoints, ",")))

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}
