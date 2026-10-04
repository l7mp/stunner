package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/l7mp/stunner/v2"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// tunnelOptions are the tunnel-mode settings picked up from the command line.
type tunnelOptions struct {
	sni       string
	insecure  bool
	logLevel  string
	logFormat string
	license   *stnrv2.LicenseConfig
	offload   string
}

// runTunnel runs stunnerd in tunnel mode: render an in-memory config with one plain (or stdin)
// listener relaying every client flow through the given TURN server to the pinned peer, and runs
// the normal reconcile machinery on it.
func runTunnel(clientArg, serverArg, peerArg string, opts tunnelOptions) {
	loggerFactory := logger.NewLoggerFactory(opts.logLevel)
	loggerFactory.SetWriter(os.Stderr)
	log := loggerFactory.NewLogger("tunnel-cli")

	conf, err := tunnelConfig(clientArg, serverArg, peerArg, opts)
	if err != nil {
		log.Errorf("error: %s", err.Error())
		os.Exit(1)
	}

	st := stunner.NewStunner(stunner.Options{
		Name:       "tunnel",
		LogOptions: stunner.LogOptions{Level: opts.logLevel, Format: opts.logFormat, Writer: os.Stderr},
	})
	defer st.Close()

	if err := st.Reconcile(conf); err != nil {
		log.Errorf("could not start the tunnel: %s", err.Error())
		os.Exit(1)
	}
	log.Infof("tunnel running: %s -> %s -> %s", clientArg, serverArg, peerArg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// a stdin tunnel is one-shot: exit once its single flow has ended (stdin EOF or idle)
	if clientArg == "-" {
		go func() {
			seen := false
			for {
				n := st.AllocationCount()
				if n > 0 {
					seen = true
				} else if seen {
					stop()
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutting down")
}

// tunnelConfig renders the tunnel-mode stunnerd config from the three positional arguments:
// one plain (or stdin) listener feeding an L4 server, whose single cluster holds the peer and
// reaches it over the peer URI's transport through a tunnel to the TURN server.
func tunnelConfig(clientArg, serverArg, peerArg string, opts tunnelOptions) (*stnrv2.StunnerConfig, error) {

	tunnel, auth, proto, err := tunnelServer(serverArg)
	if err != nil {
		return nil, err
	}
	tunnel.Insecure = opts.insecure
	if opts.sni != "" {
		if proto != stnrv2.ProtocolTURNTLS && proto != stnrv2.ProtocolTURNDTLS {
			return nil, fmt.Errorf("--sni is valid only for the TLS/DTLS transports, "+
				"server transport is %q", proto.String())
		}
		tunnel.SNI = opts.sni
	}

	listener := stnrv2.ListenerConfig{
		Name:    "tunnel-listener",
		Servers: []string{"tunnel-server"},
	}
	if clientArg == "-" {
		listener.Protocol = stnrv2.ProtocolSTDIN.String()
	} else {
		u, err := stnrv2.ParseURI(clientArg)
		if err != nil {
			return nil, fmt.Errorf("invalid client address %q: %w", clientArg, err)
		}
		if p := u.Protocol; p != stnrv2.ProtocolUDP && p != stnrv2.ProtocolTCP {
			return nil, fmt.Errorf("invalid client address %q: expecting udp:// or "+
				"tcp:// (or \"-\" for stdin)", clientArg)
		}
		listener.Protocol = u.Protocol.String()
		listener.Addr = u.Host
		listener.Port = u.Port
	}

	// The peer URI's scheme names the transport the cluster reaches the peer over, and its host
	// and port make the cluster's only endpoint: the host may be a DNS name resolvable only where
	// the tunnel runs.
	scheme, hostport, _ := strings.Cut(peerArg, "://")
	peerProto, err := stnrv2.NewClusterProtocol(scheme)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address %q: expecting "+
			"<udp|tcp>://<host>:<port>", peerArg)
	}
	peer, err := stnrv2.ParseEndpoint(hostport)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address %q: %w", peerArg, err)
	}
	if _, dialable := peer.HostPort(); !dialable {
		return nil, fmt.Errorf("invalid peer address %q: expecting a single host and port",
			peerArg)
	}
	clusterType := stnrv2.ClusterTypeStatic
	if peer.Domain != "" {
		clusterType = stnrv2.ClusterTypeStrictDNS
	}

	noHealthCheck := ""
	c := &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			Name:                "tunnel",
			HealthCheckEndpoint: &noHealthCheck,
			OffloadEngine:       opts.offload,
			LicenseConfig:       opts.license,
		},
		// the plain listener authenticates nobody; the tunnel presents the upstream credentials
		Listeners: []stnrv2.ListenerConfig{listener},
		Servers: []stnrv2.ServerConfig{{
			Name:     "tunnel-server",
			Type:     stnrv2.ServerTypeL4.String(),
			Clusters: []string{"tunnel-cluster"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:          "tunnel-cluster",
			Type:          clusterType.String(),
			Endpoints:     []string{hostport},
			RoutingPolicy: stnrv2.RoutingPolicyRoundRobin.String(),
			Protocol:      peerProto.String(),
			Tunnel:        tunnel,
		}},
	}
	if auth != nil {
		auth.Name, tunnel.Auth = "upstream", "upstream"
		c.Auths = []stnrv2.AuthConfig{*auth}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// tunnelServer parses the TURN server URI into the tunnel to reach the peer through, the
// authentication policy the tunnel presents (nil for none) and the transport of the tunnel.
func tunnelServer(arg string) (*stnrv2.TunnelConfig, *stnrv2.AuthConfig, stnrv2.Protocol, error) {
	u, err := stnrv2.ParseURI(arg)
	if err != nil {
		return nil, nil, stnrv2.ProtocolUnknown, fmt.Errorf("invalid TURN server URI %q: %w", arg, err)
	}
	if !u.Protocol.IsTURN() {
		return nil, nil, stnrv2.ProtocolUnknown, fmt.Errorf("invalid TURN server URI %q: "+
			"not a TURN transport", arg)
	}

	// the URL renders without the credentials
	var auth *stnrv2.AuthConfig
	switch {
	case u.Username != "" && u.Password != "":
		auth = &stnrv2.AuthConfig{Type: "static", Credentials: map[string]string{
			"username": u.Username, "password": u.Password}}
	case u.Username != "":
		// a bare secret mints per-allocation time-windowed credentials
		auth = &stnrv2.AuthConfig{Type: "ephemeral", Credentials: map[string]string{
			"secret": u.Username}}
	}
	return &stnrv2.TunnelConfig{URL: u.String()}, auth, u.Protocol, nil
}
