package stunner

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/pion/transport/v5"

	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/config/client"
)

// Options defines various options for the STUNner server.
type Options struct {
	// Name is the identifier of this stunnerd daemon instance. Defaults to hostname.
	Name string
	// DryRun suppresses sideeffects: STUNner will not initialize listener sockets and bring up
	// the TURN server, and it will not fire up the health-check and the metrics
	// servers. Intended for testing, default is false.
	DryRun bool
	// SuppressRollback controls whether to rollback to the last working configuration after a
	// failed reconciliation request. Default is false, which means to always do a rollback.
	SuppressRollback bool
	// LogOptions controls logger settings (level, optional rate limiter settings).
	LogOptions LogOptions
	// Resolver swaps the internal DNS resolver with a custom implementation. Intended for
	// testing.
	Resolver resolver.DnsResolver
	// UDPListenerThreadNum is ignored: every UDP listener runs one read loop per GOMAXPROCS.
	//
	// Deprecated: the number of UDP read loops is not configurable.
	UDPListenerThreadNum int
	// NodeName is the name of the Kubernetes node the TURN server is running on (if any).
	NodeName string
	// ForceReadyDuringTermination is flag to prevent the server failing the readiness
	// check during graceful shutdown. Normally an app should fail the readiness check once it
	// has entered into the graceful shutdown phase. Unfortunately, this will cause some buggy
	// kube-proxy implementations to stop delivering UDP packets to the pod after a short
	// timeout (usually 30 secs). This flag, is set, can be used to workaround such buggy
	// Kubernetes implementations by forcing the server to pass the liveness probe during
	// termination.
	ForceReadyDuringTermination bool
	// VNet will switch on testing mode, using a vnet.Net instance to run STUNner over an
	// emulated data-plane.
	Net transport.Net
}

// NewDefaultConfig builds a default configuration from a TURN server URI. Example: the URI
// `turn://user:pass@127.0.0.1:3478?transport=udp` will be parsed into a STUNner configuration with
// a TURN server listening at UDP port 3478 on every address and advertising its relays at
// 127.0.0.1, with plain-text authentication using the username/password pair `user:pass`.
// Health-checking is enabled at the default endpoint, metric scraping is disabled.
func NewDefaultConfig(uri string) (*stnrv2.StunnerConfig, error) {
	u, err := stnrv2.ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid URI '%s': %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("username/password must be set: '%s'", uri)
	}

	// the TURN URI names a TURN transport: the listener takes the transport underneath
	if !u.Protocol.IsTURN() {
		return nil, fmt.Errorf("not a TURN URI: '%s'", uri)
	}
	transport := map[stnrv2.Protocol]stnrv2.Protocol{
		stnrv2.ProtocolTURNUDP:  stnrv2.ProtocolUDP,
		stnrv2.ProtocolTURNTCP:  stnrv2.ProtocolTCP,
		stnrv2.ProtocolTURNTLS:  stnrv2.ProtocolTLS,
		stnrv2.ProtocolTURNDTLS: stnrv2.ProtocolDTLS,
	}[u.Protocol]

	// Health-checking is enabled at the default endpoint; this is what Validate would default a
	// nil pointer to anyway, we just make the intention explicit.
	h := fmt.Sprintf("http://:%d", stnrv2.DefaultHealthCheckPort)
	c := &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			LogLevel: stnrv2.DefaultLogLevel,
			// MetricsEndpoint: "http://:8088",
			HealthCheckEndpoint: &h,
		},
		Auth: stnrv2.AuthConfig{
			Type:  "plaintext",
			Realm: stnrv2.DefaultRealm,
			Credentials: map[string]string{
				"username": u.Username,
				"password": u.Password,
			},
		},
		Listeners: []stnrv2.ListenerConfig{{
			Name:     "default-listener",
			Protocol: transport.String(),
			Port:     u.Port,
			Servers:  []string{"default-server"},
		}},
		Servers: []stnrv2.ServerConfig{{
			Name:     "default-server",
			Type:     stnrv2.ServerTypeTURN.String(),
			Clusters: []string{"allow-any"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0", "::/0"},
			Protocol:  "UDP",
			Addrs:     []string{u.Host},
		}},
	}

	if transport == stnrv2.ProtocolTLS || transport == stnrv2.ProtocolDTLS {
		certPem, keyPem, err := GenerateSelfSignedKey()
		if err != nil {
			return nil, err
		}
		c.Listeners[0].Cert = base64.StdEncoding.EncodeToString(certPem)
		c.Listeners[0].Key = base64.StdEncoding.EncodeToString(keyPem)
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// GetConfig returns the configuration of the running STUNner daemon. The root Object assembles
// the StunnerConfig from its descendants, see internal/object/stunner.go.
func (s *Stunner) GetConfig() *stnrv2.StunnerConfig {
	s.log.Tracef("getConfig")
	if c, ok := s.rt.GetConfig(runtime.TypeStunner, "").(*stnrv2.StunnerConfig); ok && c != nil {
		c.ApiVersion = s.version
		return c
	}
	return &stnrv2.StunnerConfig{ApiVersion: s.version}
}

// LoadConfig loads a configuration from an origin. This is a shim wrapper around configclient.Load.
func (s *Stunner) LoadConfig(origin string) (*stnrv2.StunnerConfig, error) {
	client, err := client.New(origin, s.name, s.cdsLabels, s.logger)
	if err != nil {
		return nil, err
	}

	return client.Load()
}

// WatchConfig watches a configuration from an origin. This is a shim wrapper around configclient.Watch.
func (s *Stunner) WatchConfig(ctx context.Context, origin string, ch chan<- *stnrv2.StunnerConfig, suppressDelete bool) error {
	client, err := client.New(origin, s.name, s.cdsLabels, s.logger)
	if err != nil {
		return err
	}

	return client.Watch(ctx, ch, suppressDelete)
}
