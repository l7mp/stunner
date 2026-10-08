package v2

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ServerSpec is base server config shared across all transport servers.
type ServerSpec struct {
	// Name of the server.
	Name string `json:"name"`
	// Type is TURN (default), L4, SIP or RTP.
	Type string `json:"type,omitempty"`
	// Clusters lists the names of the clusters the server uses, in order.
	Clusters []string `json:"clusters,omitempty"`
}

// ServerConfig is a server: its spec and the options of its type. Listeners attach to a server by
// naming it.
type ServerConfig struct {
	ServerSpec `json:",inline"`
	// TURN holds the options of a TURN server.
	TURN *TURNServerConfig `json:"turn,omitempty"`
	// L4 holds the options of an L4 server.
	L4 *L4ServerConfig `json:"l4,omitempty"`
	// SIP holds the options of a SIP server.
	SIP *SIPServerConfig `json:"sip,omitempty"`
	// RTP holds the options of an RTP server.
	RTP *RTPServerConfig `json:"rtp,omitempty"`
}

// TURNServerConfig holds the options of a TURN server.
type TURNServerConfig struct {
	// Auth names the authentication policy the server authenticates its clients by. Default is
	// "default".
	Auth string `json:"auth,omitempty"`
	// PermissionFilters name the filters the peer IP of a permission must pass; rules with ports
	// never match there.
	PermissionFilters []string `json:"permission_filters,omitempty"`
}

// L4ServerConfig holds the options of an L4 server.
type L4ServerConfig struct {
	// Auth names the authentication policy of the server; only its realm is used, to scope the
	// quota of the flows.
	Auth string `json:"auth,omitempty"`
}

// SIPServerConfig holds the options of a SIP server: a stateful SIP proxy of the calls the
// carriers start, handing every SDP to a media proxy.
type SIPServerConfig struct {
	// Auth names the authentication policy the server challenges by digest the carriers its
	// listeners admit but do not trust. Default is none.
	Auth string `json:"auth,omitempty"`
	// PublicAddr is the address the carriers reach the server at; empty is the local address of
	// the carrier's conn.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port the carriers reach the server at; 0 is the local port of the
	// carrier's conn.
	PublicPort int `json:"public_port,omitempty"`
	// MediaProxy names the cluster reaching the ng control endpoint of the media proxy.
	MediaProxy string `json:"media_proxy,omitempty"`
}

// RTPServerConfig holds the options of an RTP server: a media proxy relaying the media of the
// calls a SIP server sets up, controlled over ng on its listeners labeled RoleLabel: RoleControl.
type RTPServerConfig struct {
	// PublicAddr is the address the carriers send media to.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port the carriers send media to.
	PublicPort int `json:"public_port,omitempty"`
}

// RoleLabel is the listener label telling a server what the conns of the listener are for:
// RoleControl marks the ng control conns of an RTP server.
const (
	RoleLabel   = "role"
	RoleControl = "control"
)

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
	// the block of the server's own type is always there; the blocks of other types are ignored
	switch t {
	case ServerTypeTURN:
		if req.TURN == nil {
			req.TURN = &TURNServerConfig{}
		}
		if req.TURN.Auth == "" {
			req.TURN.Auth = DefaultAuthName
		}
		if req.TURN.PermissionFilters == nil {
			req.TURN.PermissionFilters = []string{}
		}
	case ServerTypeL4:
		if req.L4 == nil {
			req.L4 = &L4ServerConfig{}
		}
	case ServerTypeSIP:
		if req.SIP == nil {
			req.SIP = &SIPServerConfig{}
		}
	case ServerTypeRTP:
		if req.RTP == nil {
			req.RTP = &RTPServerConfig{}
		}
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
	if req.TURN != nil {
		t := *req.TURN
		t.PermissionFilters = slices.Clone(req.TURN.PermissionFilters)
		ret.TURN = &t
	}
	if req.L4 != nil {
		l := *req.L4
		ret.L4 = &l
	}
	if req.SIP != nil {
		sip := *req.SIP
		ret.SIP = &sip
	}
	if req.RTP != nil {
		rtp := *req.RTP
		ret.RTP = &rtp
	}
}

// String stringifies the configuration.
func (req *ServerConfig) String() string {
	n := "-"
	if req.Name != "" {
		n = req.Name
	}
	opts := ""
	if t := req.TURN; t != nil {
		if len(t.PermissionFilters) > 0 {
			opts += ",permission_filters=[" + strings.Join(t.PermissionFilters, ",") + "]"
		}
		if t.Auth != "" {
			opts += ",auth=" + t.Auth
		}
	}
	if l := req.L4; l != nil && l.Auth != "" {
		opts += ",auth=" + l.Auth
	}
	if sip := req.SIP; sip != nil {
		if sip.Auth != "" {
			opts += ",auth=" + sip.Auth
		}
		opts += fmt.Sprintf(",public=%s:%d,media_proxy=%s", sip.PublicAddr, sip.PublicPort,
			sip.MediaProxy)
	}
	if rtp := req.RTP; rtp != nil {
		opts += fmt.Sprintf(",public=%s:%d", rtp.PublicAddr, rtp.PublicPort)
	}
	return fmt.Sprintf("%q:{type=%s,clusters=[%s]%s}", n, req.Type, strings.Join(req.Clusters, ","),
		opts)
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
