package v2

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ServerConfig is the behavior of the dataplane and the clusters it reaches peers through, in
// order. Listeners attach to a server by naming it.
type ServerConfig struct {
	// Name of the server.
	Name string `json:"name"`
	// Type is the behavior of the server: "turn" serves TURN, admitting the peers its clusters'
	// endpoints contain and relaying through the first cluster that can make the relay
	// transport, "l4" forwards every client flow to an endpoint of the first cluster that
	// yields one. Default is "turn".
	Type string `json:"type,omitempty"`
	// Clusters lists the names of the clusters the server uses, in order.
	Clusters []string `json:"clusters,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *ServerConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in server configuration: %s", req.String())
	}
	if req.Type == "" {
		req.Type = DefaultServerType
	}
	t, err := NewServerType(req.Type)
	if err != nil {
		return err
	}
	req.Type = t.String()
	if req.Clusters == nil {
		req.Clusters = []string{}
	}
	return nil
}

// ConfigName returns the name of the object to be configured.
func (req *ServerConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *ServerConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *ServerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*ServerConfig)
	*ret = *req
	ret.Clusters = slices.Clone(req.Clusters)
}

// String stringifies the configuration.
func (req *ServerConfig) String() string {
	n := "-"
	if req.Name != "" {
		n = req.Name
	}
	return fmt.Sprintf("%q:{type=%s,clusters=[%s]}", n, req.Type, strings.Join(req.Clusters, ","))
}

// ServerStatus is the status of a server: its config and its live session count (TURN
// allocations or L4 flows).
type ServerStatus struct {
	*ServerConfig
	Sessions int `json:"sessions"`
}

// String stringifies the status.
func (req *ServerStatus) String() string {
	return fmt.Sprintf("%s,sessions=%d", req.ServerConfig.String(), req.Sessions)
}
