package v2

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// ServerSpec is base server config shared across all transport servers.
type ServerSpec struct {
	// Name of the server.
	Name string `json:"name"`
	// Type is TURN (default), L4, SIP, RTP or SRT.
	Type string `json:"type,omitempty"`
	// Clusters lists the names of the clusters the server uses, in order.
	Clusters []string `json:"clusters,omitempty"`
	// Filters name the filters a server relaying to the peers others choose (TURN, RTP) checks
	// those peers against, in order: a peer no rule allows is refused. Other servers ignore them.
	Filters []string `json:"filters,omitempty"`
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
	// SRT holds the options of an SRT server.
	SRT *SRTServerConfig `json:"srt,omitempty"`
}

// TURNServerConfig holds the options of a TURN server.
type TURNServerConfig struct {
	// Auth names the authentication policy the server authenticates its clients by. Default is
	// "default".
	Auth string `json:"auth,omitempty"`
}

// L4ServerConfig holds the options of an L4 server.
type L4ServerConfig struct {
	// Auth names the authentication policy of the server; only its realm is used, to scope the
	// quota of the flows.
	Auth string `json:"auth,omitempty"`
	// LBPolicy is how the server chooses the peer of a new flow among the targets of its
	// clusters: ROUND_ROBIN (the default).
	LBPolicy string `json:"lb_policy,omitempty"`
}

// SIPServerConfig holds the options of a SIP server: a stateful SIP proxy of the calls the
// carriers start, handing every SDP to a media proxy.
type SIPServerConfig struct {
	// Auth names the authentication policy the server challenges by digest the carriers its
	// listeners admit but do not trust. Default is none.
	Auth string `json:"auth,omitempty"`
	// PublicAddr is the address the carriers reach the server at when the listener of the
	// carrier's conn has no public address; empty is the local address of the conn.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port the carriers reach the server at when the listener of the carrier's
	// conn has no public address; 0 is the local port of the conn.
	PublicPort int `json:"public_port,omitempty"`
	// MediaProxy names the cluster reaching the ng control endpoint of the media proxy.
	MediaProxy string `json:"media_proxy,omitempty"`
	// LBPolicy is how the server chooses the in-cluster SIP target of a new dialog among the
	// targets of its clusters: ROUND_ROBIN (the default).
	LBPolicy string `json:"lb_policy,omitempty"`
	// Registrations are the registrations the server keeps up with registrar-based carriers,
	// each over the conns of the listeners labeled RegistrationLabel: its name.
	Registrations []SIPRegistrationConfig `json:"registrations,omitempty"`
}

// SIPRegistrationConfig is a registration of an address of record with a carrier's registrar.
type SIPRegistrationConfig struct {
	// Name of the registration, the value of the RegistrationLabel of its listeners.
	Name string `json:"name"`
	// Auth names the authentication policy holding the credentials the server answers the
	// registrar's digest challenge with. Default is none.
	Auth string `json:"auth,omitempty"`
	// AOR is the address of record to register, a sip: or sips: URI.
	AOR string `json:"aor"`
	// Expires is the registration lifetime the server asks for, as a duration string. Default
	// is 600s.
	Expires string `json:"expires,omitempty"`
}

// RTPServerConfig holds the options of an RTP server: a media proxy relaying the media of the
// calls a SIP server sets up, controlled over ng on its listeners labeled RoleLabel: RoleControl.
type RTPServerConfig struct {
	// PublicAddr is the address the carriers send media to.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port the carriers send media to.
	PublicPort int `json:"public_port,omitempty"`
}

// SRTServerConfig holds the options of an SRT server: the first flow naming a stream places the
// stream on an endpoint of the server's clusters, and every later flow naming it, publisher or
// reader, goes to the same endpoint.
type SRTServerConfig struct {
	// Linger is how long a stream keeps its endpoint after its last flow ended, as a duration
	// string, so a publisher reconnecting returns to its readers. Default is 30s.
	Linger string `json:"linger,omitempty"`
	// LBPolicy is how the server places a new stream among the targets of its clusters. Default
	// is ROUND_ROBIN.
	LBPolicy string `json:"lb_policy,omitempty"`
}

// RoleLabel is the listener label telling a server what the conns of the listener are for:
// RoleControl marks the ng control conns of an RTP server.
const (
	RoleLabel   = "role"
	RoleControl = "control"
)

// RegistrationLabel is the listener label naming the SIP registration the conns of the listener
// carry.
const RegistrationLabel = "sip.registration"

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
	if req.Filters == nil {
		req.Filters = []string{}
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
	case ServerTypeL4:
		if req.L4 == nil {
			req.L4 = &L4ServerConfig{}
		}
		if req.L4.LBPolicy == "" {
			req.L4.LBPolicy = DefaultLBPolicy
		}
		p, err := NewLBPolicy(req.L4.LBPolicy)
		if err != nil {
			return err
		}
		req.L4.LBPolicy = p.String()
	case ServerTypeSIP:
		if req.SIP == nil {
			req.SIP = &SIPServerConfig{}
		}
		if req.SIP.LBPolicy == "" {
			req.SIP.LBPolicy = DefaultLBPolicy
		}
		p, err := NewLBPolicy(req.SIP.LBPolicy)
		if err != nil {
			return err
		}
		req.SIP.LBPolicy = p.String()
		for i := range req.SIP.Registrations {
			r := &req.SIP.Registrations[i]
			if r.Name == "" {
				return fmt.Errorf("missing name in SIP registration of server %q", req.Name)
			}
			if aor := strings.ToLower(r.AOR); !strings.HasPrefix(aor, "sip:") &&
				!strings.HasPrefix(aor, "sips:") {
				return fmt.Errorf("invalid AOR %q in SIP registration %q: want a sip: or sips: URI",
					r.AOR, r.Name)
			}
			if r.Expires == "" {
				r.Expires = DefaultSIPRegistrationExpires
			}
			if d, err := time.ParseDuration(r.Expires); err != nil || d <= 0 {
				return fmt.Errorf("invalid expires %q in SIP registration %q", r.Expires, r.Name)
			}
		}
	case ServerTypeRTP:
		if req.RTP == nil {
			req.RTP = &RTPServerConfig{}
		}
	case ServerTypeSRT:
		if req.SRT == nil {
			req.SRT = &SRTServerConfig{}
		}
		if req.SRT.Linger == "" {
			req.SRT.Linger = DefaultSRTLinger
		}
		if d, err := time.ParseDuration(req.SRT.Linger); err != nil || d < 0 {
			return fmt.Errorf("invalid linger %q in SRT server %q", req.SRT.Linger, req.Name)
		}
		if req.SRT.LBPolicy == "" {
			req.SRT.LBPolicy = DefaultLBPolicy
		}
		p, err := NewLBPolicy(req.SRT.LBPolicy)
		if err != nil {
			return err
		}
		req.SRT.LBPolicy = p.String()
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
	ret.Filters = slices.Clone(req.Filters)
	if req.TURN != nil {
		t := *req.TURN
		ret.TURN = &t
	}
	if req.L4 != nil {
		l := *req.L4
		ret.L4 = &l
	}
	if req.SIP != nil {
		sip := *req.SIP
		sip.Registrations = slices.Clone(req.SIP.Registrations)
		ret.SIP = &sip
	}
	if req.RTP != nil {
		rtp := *req.RTP
		ret.RTP = &rtp
	}
	if req.SRT != nil {
		srt := *req.SRT
		ret.SRT = &srt
	}
}

// String stringifies the configuration.
func (req *ServerConfig) String() string {
	n := "-"
	if req.Name != "" {
		n = req.Name
	}
	opts := ""
	if len(req.Filters) > 0 {
		opts += ",filters=[" + strings.Join(req.Filters, ",") + "]"
	}
	if t := req.TURN; t != nil {
		if t.Auth != "" {
			opts += ",auth=" + t.Auth
		}
	}
	if l := req.L4; l != nil {
		if l.Auth != "" {
			opts += ",auth=" + l.Auth
		}
		opts += ",lb_policy=" + l.LBPolicy
	}
	if sip := req.SIP; sip != nil {
		if sip.Auth != "" {
			opts += ",auth=" + sip.Auth
		}
		opts += fmt.Sprintf(",public=%s:%d,media_proxy=%s,lb_policy=%s", sip.PublicAddr,
			sip.PublicPort, sip.MediaProxy, sip.LBPolicy)
		if len(sip.Registrations) > 0 {
			names := []string{}
			for _, r := range sip.Registrations {
				names = append(names, r.Name)
			}
			opts += ",registrations=[" + strings.Join(names, ",") + "]"
		}
	}
	if rtp := req.RTP; rtp != nil {
		opts += fmt.Sprintf(",public=%s:%d", rtp.PublicAddr, rtp.PublicPort)
	}
	if srt := req.SRT; srt != nil {
		opts += ",linger=" + srt.Linger + ",lb_policy=" + srt.LBPolicy
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
