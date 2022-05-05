package stunner

import (
	"fmt"
	"github.com/pion/transport/vnet"
)

const ApiVersion string = "v1alpha1"
const DefaultPort int = 3478
const DefaultLogLevel = "all:INFO"
const DefaultRealm = "stunner.l7mp.io"
const DefaultAuthType = "plaintext"
const DefaultUsername = "user1"
const DefaultPassword = "passwd1"
const DefaultMinRelayPort int = 1<<10
const DefaultMaxRelayPort int = 1<<16-1

// AdminConfig holds the administrative configuration
type AdminConfig struct {
	// Name is the name of the server, optional
	Name string			`json:"name",omitempty`
	// LogLevel is the desired log verbosity, e.g.: "stunner:TRACE,all:INFO"
	LogLevel string			`json:"logLevel",omitempty`
	// Realm is the STUN/TURN realm
	Realm string                    `json:"realm",omitempty`
}

// Auth defines the specification of the STUN/TURN authentication mechanism used by STUNner
type AuthConfig struct {
	// Type is the type of the STUN/TURN authentication mechanism ("plaintext" or "longterm")
	Type string                     `json:"type",omitempty`
	// Credentials specifies the authententication credentials: for "plaintext" at least the
	// keys "username" and "password" must be set, for "longterm" the key "secret" will hold
	// the shared authentication secret
	Credentials map[string]string 	`json:"credentials"`
}

// ListenerConfig specifies a particular listener for the STUNner deamon
type ListenerConfig struct {
	// Name is the name of the listener
	Name string			`json:"name",omitempty`
	// Protocol is the transport protocol used by the listener ("UDP", "TCP", "TLS", "DTLS")
	Protocol string			`json:"protocol",omitempty`
	// Addr is the IP address for the listener
	Addr string			`json:"address",omitempty`
	// Port is the port for the listener
	Port int			`json:"port",omitempty`
	// MinRelayPort is the smallest relay port assigned for the relay connections spawned by
	// the listener
	MinRelayPort int		`json:"minPort",omitempty`
	// MaxRelayPort is the highest relay port assigned for the relay connections spawned by the
	// listener
	MaxRelayPort int		`json:"maxPort",omitempty`
	// Cert is the TLS cert
	Cert string			`json:"cert",omitempty`
	// Key is the TLS key
	Key string			`json:"key",omitempty`
}

// String returns a string ID for the Listener, suitable only for being stored as a key in a map or for logging
func (l ListenerConfig) String() string {
	return fmt.Sprintf("%s://%s:%d", l.Protocol, l.Addr, l.Port)
}

// StaticResourceConfig defines the static resources for the daemon
type StaticResourceConfig struct {
	// Auth defines the specification of the STUN/TURN authentication mechanism used by STUNner
	Auth AuthConfig			`json:"auth"`
	// Listeners defines the listeners for the STUNner deamon
	Listeners []ListenerConfig	`json:"listeners",omitempty`
}

// StunnerConfig configures the STUnner daemon
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented
	ApiVersion string		`json:"version"`
	// AdminConfig holds the administrative configuration
	Admin AdminConfig		`json:"admin",omitempty`
	// StaticResourceConfig defines the static resources for the daemon
	Static StaticResourceConfig	`json:"static"`
	// Net is used for testing
	Net *vnet.Net
}
