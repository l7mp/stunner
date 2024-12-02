package v1alpha1

import (
	"fmt"
	"maps"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Config is the main interface for STUNner configuration objects
type Config = stnrv1.Config

// AdminConfig holds the administrative configuration.
type AdminConfig = stnrv1.AdminConfig

// ClusterConfig specifies a set of upstream peers STUNner can open transport relay connections
// to. There are two address resolution policies. In STATIC clusters the allowed peer IP addresses
// are explicitly listed in the endpoint list. In STRICT_DNS clusters the endpoints are assumed to
// be proper DNS domain names. STUNner will resolve each domain name in the background and admits a
// new connection only if the peer address matches one of the IP addresses returned by the DNS
// resolver for one of the endpoints. STRICT_DNS clusters are best used with headless Kubernetes
// services.
type ClusterConfig = stnrv1.ClusterConfig

// ListenerConfig specifies a server socket on which STUN/TURN connections will be served.
type ListenerConfig = stnrv1.ListenerConfig

// StunnerConfig specifies the configuration of the the STUnner daemon.
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented.
	ApiVersion string `json:"version"`
	// AdminConfig holds administrative configuration.
	Admin AdminConfig `json:"admin,omitempty"`
	// Auth defines the STUN/TURN authentication mechanism.
	Auth AuthConfig `json:"auth"`
	// Listeners defines the server sockets exposed to clients.
	Listeners []ListenerConfig `json:"listeners,omitempty"`
	// Clusters defines the upstream endpoints to which relay transport connections can be made
	// by clients.
	Clusters []ClusterConfig `json:"clusters,omitempty"`
}

// Validate checks if a listener configuration is correct.
func (req *StunnerConfig) Validate() error {
	// ApiVersion
	if req.ApiVersion != ApiVersion {
		return fmt.Errorf("unsupported API version: %q", req.ApiVersion)
	}

	// validate admin
	if err := req.Admin.Validate(); err != nil {
		return err
	}

	// validate auth
	if err := req.Auth.Validate(); err != nil {
		return err
	}

	// validate listeners
	for i, l := range req.Listeners {
		if err := l.Validate(); err != nil {
			return err
		}
		req.Listeners[i] = l
	}
	// // listeners are sorted by name
	// sort.Slice(req.Listeners, func(i, j int) bool {
	// 	return req.Listeners[i].Name < req.Listeners[j].Name
	// })

	// validate clusters
	for i, c := range req.Clusters {
		if err := c.Validate(); err != nil {
			return err
		}
		req.Clusters[i] = c
	}

	// // clusters are sorted by name
	// sort.Slice(req.Clusters, func(i, j int) bool {
	// 	return req.Clusters[i].Name < req.Clusters[j].Name
	// })

	return nil
}

// Name returns the name of the object to be configured.
func (req *StunnerConfig) ConfigName() string {
	return req.Admin.Name
}

// DeepEqual compares two configurations.
func (req *StunnerConfig) DeepEqual(conf Config) bool {
	other, ok := conf.(*StunnerConfig)
	if !ok {
		return false
	}

	if req.ApiVersion != other.ApiVersion {
		return false
	}
	if !req.Admin.DeepEqual(&other.Admin) {
		return false
	}
	if !req.Auth.DeepEqual(&other.Auth) {
		return false
	}

	for i := range req.Listeners {
		if i >= len(other.Listeners) {
			return false
		}
		if !req.Listeners[i].DeepEqual(&other.Listeners[i]) {
			return false
		}
	}

	for i := range req.Clusters {
		if i >= len(other.Clusters) {
			return false
		}
		if !req.Clusters[i].DeepEqual(&other.Clusters[i]) {
			return false
		}
	}

	return true
}

// DeepCopyInto copies a configuration.
func (req *StunnerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*StunnerConfig)
	ret.ApiVersion = req.ApiVersion
	req.Admin.DeepCopyInto(&ret.Admin)
	req.Auth.DeepCopyInto(&ret.Auth)

	ret.Listeners = make([]ListenerConfig, len(req.Listeners))
	for i := range req.Listeners {
		req.Listeners[i].DeepCopyInto(&ret.Listeners[i])
	}

	ret.Clusters = make([]ClusterConfig, len(req.Clusters))
	for i := range req.Clusters {
		req.Clusters[i].DeepCopyInto(&ret.Clusters[i])
	}
}

// String stringifies the configuration.
func (req *StunnerConfig) String() string {
	status := []string{}
	status = append(status, fmt.Sprintf("version=%q", req.ApiVersion))
	status = append(status, req.Admin.String())
	status = append(status, req.Auth.String())

	ls := []string{}
	for _, l := range req.Listeners {
		ls = append(ls, l.String())
	}
	status = append(status, fmt.Sprintf("listeners=[%s]", strings.Join(ls, ",")))

	cs := []string{}
	for _, c := range req.Clusters {
		cs = append(cs, c.String())
	}
	status = append(status, fmt.Sprintf("clusters=[%s]", strings.Join(cs, ",")))

	return fmt.Sprintf("{%s}", strings.Join(status, ","))
}

// GetListenerConfig finds a Listener by name in a StunnerConfig or returns an error.
func (req *StunnerConfig) GetListenerConfig(name string) (ListenerConfig, error) {
	for _, l := range req.Listeners {
		if l.Name == name {
			return l, nil
		}
	}

	return ListenerConfig{}, ErrNoSuchListener
}

// GetClusterConfig finds a Cluster by name in a StunnerConfig or returns an error.
func (req *StunnerConfig) GetClusterConfig(name string) (ClusterConfig, error) {
	for _, c := range req.Clusters {
		if c.Name == name {
			return c, nil
		}
	}

	return ClusterConfig{}, ErrNoSuchCluster
}

// ConvertToV1 upgrades a v1alpha1 StunnerConfig to a v1.
func ConvertToV1(sv1a1 *StunnerConfig) (*stnrv1.StunnerConfig, error) {
	sv1 := stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
	}

	(*stnrv1.AdminConfig)(&sv1a1.Admin).DeepCopyInto(&sv1.Admin)

	// auth needs to be converted
	at, err := stnrv1.NewAuthType(sv1a1.Auth.Type)
	if err != nil {
		return nil, err
	}

	sv1.Auth = stnrv1.AuthConfig{
		Type:        at.String(),
		Realm:       sv1a1.Auth.Realm,
		Credentials: make(map[string]string),
	}
	maps.Copy(sv1.Auth.Credentials, sv1a1.Auth.Credentials)

	sv1.Listeners = make([]stnrv1.ListenerConfig, len(sv1a1.Listeners))
	for i := range sv1a1.Listeners {
		(*stnrv1.ListenerConfig)(&sv1a1.Listeners[i]).DeepCopyInto(&sv1.Listeners[i])
	}

	sv1.Clusters = make([]stnrv1.ClusterConfig, len(sv1a1.Clusters))
	for i := range sv1a1.Clusters {
		(*stnrv1.ClusterConfig)(&sv1a1.Clusters[i]).DeepCopyInto(&sv1.Clusters[i])
	}

	return &sv1, nil
}
