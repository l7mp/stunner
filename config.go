package stunner

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client"
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
	// UDPListenerThreadNum determines the number of readloop threads spawned per UDP listener
	// (default is 4, must be >0 integer). TURN allocations will be automatically load-balanced
	// by the kernel UDP stack based on the client 5-tuple. This setting controls the maximum
	// number of CPU cores UDP listeners can scale to. Note that all other listener protocol
	// types (TCP, TLS and DTLS) use per-client threads, so this setting affects only UDP
	// listeners. For more info see https://github.com/pion/turn/pull/295.
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
// a server running on the localhost at UDP port 3478, with plain-text authentication using the
// username/password pair `user:pass`. Health-checking is enabled at the default endpoint, metric
// scraping is disabled.
func NewDefaultConfig(uri string) (*stnrv1.StunnerConfig, error) {
	u, err := ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid URI '%s': %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("username/password must be set: '%s'", uri)
	}

	// Health-checking is enabled at the default endpoint; this is what Validate would default a
	// nil pointer to anyway, we just make the intention explicit.
	h := fmt.Sprintf("http://:%d", stnrv1.DefaultHealthCheckPort)
	c := &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			LogLevel: stnrv1.DefaultLogLevel,
			// MetricsEndpoint: "http://:8088",
			HealthCheckEndpoint: &h,
		},
		Auth: stnrv1.AuthConfig{
			Type:  "plaintext",
			Realm: stnrv1.DefaultRealm,
			Credentials: map[string]string{
				"username": u.Username,
				"password": u.Password,
			},
		},
		Listeners: []stnrv1.ListenerConfig{{
			Name:     "default-listener",
			Protocol: u.Protocol,
			Addr:     u.Address,
			Port:     u.Port,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name:      "allow-any",
			Type:      "STATIC",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	}

	p := strings.ToUpper(u.Protocol)
	if p == "TLS" || p == "DTLS" || p == "TURN-TLS" || p == "TURN-DTLS" {
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
// the StunnerConfig from its descendants — see internal/object/stunner.go.
func (s *Stunner) GetConfig() *stnrv1.StunnerConfig {
	s.log.Tracef("getConfig")
	if c, ok := s.rt.GetConfig(runtime.TypeStunner, "").(*stnrv1.StunnerConfig); ok && c != nil {
		c.ApiVersion = s.version
		return c
	}
	return &stnrv1.StunnerConfig{ApiVersion: s.version}
}

// LoadConfig loads a configuration from an origin. This is a shim wrapper around configclient.Load.
func (s *Stunner) LoadConfig(origin string) (*stnrv1.StunnerConfig, error) {
	client, err := client.New(origin, s.name, s.node, s.logger)
	if err != nil {
		return nil, err
	}

	return client.Load()
}

// WatchConfig watches a configuration from an origin. This is a shim wrapper around configclient.Watch.
func (s *Stunner) WatchConfig(ctx context.Context, origin string, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	client, err := client.New(origin, s.name, s.node, s.logger)
	if err != nil {
		return err
	}

	return client.Watch(ctx, ch, suppressDelete)
}
