package stunner

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pion/transport/v3"

	"github.com/l7mp/stunner/internal/resolver"
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
	// LogLevel specifies the required loglevel for STUNner and each of its sub-objects, e.g.,
	// "all:TRACE" will force maximal loglevel throughout, "all:ERROR,auth:TRACE,turn:DEBUG"
	// will suppress all logs except in the authentication subsystem and the TURN protocol
	// logic.
	LogLevel string
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
	// VNet will switch on testing mode, using a vnet.Net instance to run STUNner over an
	// emulated data-plane.
	Net transport.Net
}

// NewDefaultConfig builds a default configuration from a TURN server URI. Example: the URI
// `turn://user:pass@127.0.0.1:3478?transport=udp` will be parsed into a STUNner configuration with
// a server running on the localhost at UDP port 3478, with plain-text authentication using the
// username/password pair `user:pass`. Health-checks and metric scarping are disabled.
func NewDefaultConfig(uri string) (*stnrv1.StunnerConfig, error) {
	u, err := ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid URI '%s': %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("username/password must be set: '%s'", uri)
	}

	h := ""
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

// GetConfig returns the configuration of the running STUNner daemon.
func (s *Stunner) GetConfig() *stnrv1.StunnerConfig {
	s.log.Tracef("GetConfig")

	// singletons, but we want to avoid panics when GetConfig is called on an uninitialized
	// STUNner object
	adminConf := stnrv1.AdminConfig{}
	if len(s.adminManager.Keys()) > 0 {
		adminConf = *s.GetAdmin().GetConfig().(*stnrv1.AdminConfig)
	}

	authConf := stnrv1.AuthConfig{}
	if len(s.authManager.Keys()) > 0 {
		authConf = *s.GetAuth().GetConfig().(*stnrv1.AuthConfig)
	}

	listeners := s.listenerManager.Keys()
	clusters := s.clusterManager.Keys()

	c := stnrv1.StunnerConfig{
		ApiVersion: s.version,
		Admin:      adminConf,
		Auth:       authConf,
		Listeners:  make([]stnrv1.ListenerConfig, len(listeners)),
		Clusters:   make([]stnrv1.ClusterConfig, len(clusters)),
	}

	for i, name := range listeners {
		c.Listeners[i] = *s.GetListener(name).GetConfig().(*stnrv1.ListenerConfig)
	}

	for i, name := range clusters {
		c.Clusters[i] = *s.GetCluster(name).GetConfig().(*stnrv1.ClusterConfig)
	}

	return &c
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
