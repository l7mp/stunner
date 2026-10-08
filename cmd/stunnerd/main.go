package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/l7mp/stunner/v2/pkg/utils/discovery"

	flag "github.com/spf13/pflag"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	// Register the bundled Mozilla CA roots as the fallback verification pool: the stunnerd
	// image is built from scratch with no system CA bundle, and upstream turn-tls/turn-dtls
	// servers and wss:// config origins must still verify. A system bundle, when present,
	// takes precedence.
	_ "golang.org/x/crypto/x509roots/fallback"

	"github.com/l7mp/stunner/v2"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/buildinfo"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
)

var (
	version    = "dev"
	commitHash = "n/a"
	buildDate  = "<unknown>"
)

const usage = `stunnerd: the STUNner gateway dataplane

Usage (the positional argument count selects the mode):
    stunnerd [options]
        Run the dataplane daemon, taking configuration from the config origin (-c).
    stunnerd [options] <turn-listener-uri>
        Run a standalone TURN server with a default configuration, e.g.
        stunnerd turn://user:pass@127.0.0.1:3478?transport=udp
    stunnerd [options] <client-addr> <turn-server-addr> <peer-addr>
        Tunnel a local client to a peer through a remote TURN server:
        client-addr: <udp|tcp>://<addr>:<port>, or "-" to tunnel stdin/stdout
        turn-server-addr: turn://<auth>@<addr>:<port>[?transport=udp|tcp|tls|dtls]
        peer-addr: <udp|tcp>://<addr>:<port>
        auth: <username:password|secret>

Options:
`

func main() {
	os.Args[0] = "stunnerd"

	var config = flag.StringP("config", "c", "", "Config origin, either a valid address in the format IP:port, or HTTP URL to the CDS server, or literal \"k8s\" to discover the CDS server from Kubernetes, or a proper file name URI in the format file://<path-to-config-file> (overrides: STUNNER_CONFIG_ORIGIN)")
	var level = flag.StringP("log", "l", "", "Log level (format: <scope>:<level>, overrides: PION_LOG_*, default: all:INFO)")
	var logFormat = flag.String("log-format", "text", `Log output format: "text" (default) or "json"`)
	var id = flag.StringP("id", "i", "", "Id for identifying with the CDS server (format: <namespace>/<name>, overrides: STUNNER_NAMESPACE/STUNNER_NAME, default: <default/stunnerd-hostname>)")
	var watch = flag.BoolP("watch", "w", false, "Watch config file for updates (default: false)")
	_ = flag.IntP("udp-thread-num", "u", 0, "Number of readloops per UDP listener (deprecated, defaults to GOMAXPROCS)")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false)")
	var noGracefulShutdown = flag.Bool("no-graceful-shutdown", false, "Exit at once on SIGTERM or SIGINT, closing the live sessions, instead of waiting for them to end (default: false)")
	var forceReadyDuringTermination = flag.Bool("force-ready-status", false, "Prevent the server from failing the liveness probe during graceful shutdown as a workaround for buggy kube-proxy implementations (default: false)")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>")
	var sni = flag.String("sni", "", "Server name (SNI) for the TURN/TLS and TURN/DTLS transports (tunnel mode)")
	var insecure = flag.Bool("insecure", false, "Accept self-signed TURN server TLS certificates (tunnel mode, default: false)")
	var licenseArg = flag.String("license", "", `License config as the JSON licensegen writes, {"license_config":{"key":"...","hmac":"..."}}, or just {"key":"...","hmac":"..."} (overrides the config's)`)
	var offload = flag.String("offload", "", `Offload engine on every interface: "none", "xdp", "tc" or "auto" (overrides the config's)`)

	// Kubernetes config flags
	k8sConfigFlags := cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(flag.CommandLine)

	// CDS server discovery flags
	cdsConfigFlags := discovery.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(flag.CommandLine)

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}

	flag.Parse()

	// the license is what licensegen writes, or the bare license config inside it
	var license *stnrv2.LicenseConfig
	if *licenseArg != "" {
		for _, raw := range []string{*licenseArg, `{"license_config":` + *licenseArg + `}`} {
			l := struct {
				LicenseConfig *stnrv2.LicenseConfig `json:"license_config"`
			}{}
			if err := json.Unmarshal([]byte(raw), &l); err == nil && l.LicenseConfig != nil &&
				l.LicenseConfig.Key != "" && l.LicenseConfig.HMAC != "" {
				license = l.LicenseConfig
				break
			}
		}
		if license == nil {
			fmt.Fprint(os.Stderr, "invalid --license: expecting the JSON licensegen writes, "+
				`{"license_config":{"key":"...","hmac":"..."}}, or {"key":"...","hmac":"..."}`+"\n")
			os.Exit(1)
		}
	}
	if *offload != "" {
		if _, err := stnrv2.NewOffloadEngine(*offload); err != nil {
			fmt.Fprintf(os.Stderr, "invalid --offload: %s\n", err.Error())
			os.Exit(1)
		}
	}

	logLevel := stnrv2.DefaultLogLevel
	if *verbose {
		logLevel = "all:DEBUG"
	}

	if *level != "" {
		logLevel = *level
	}

	if flag.NArg() == 2 || flag.NArg() > 3 {
		flag.Usage()
		os.Exit(1)
	}

	configOrigin := stnrv2.DefaultConfigDiscoveryAddress
	if origin, ok := os.LookupEnv(stnrv2.DefaultEnvVarConfigOrigin); ok {
		configOrigin = origin
	}
	if *config != "" {
		configOrigin = *config
	}

	nodeName := ""
	if node, ok := os.LookupEnv(stnrv2.DefaultEnvVarNodeName); ok {
		nodeName = node
	}

	if *id == "" {
		name, ok1 := os.LookupEnv(stnrv2.DefaultEnvVarName)
		namespace, ok2 := os.LookupEnv(stnrv2.DefaultEnvVarNamespace)
		if ok1 && ok2 {
			*id = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	st := stunner.NewStunner(stunner.Options{
		Name:                        *id,
		LogOptions:                  stunner.LogOptions{Level: logLevel, Format: *logFormat},
		DryRun:                      *dryRun,
		NodeName:                    nodeName,
		ForceReadyDuringTermination: *forceReadyDuringTermination,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	log.Infof("starting stunnerd id %q, STUNner %s ", st.GetId(), buildInfo.String())

	conf := make(chan *stnrv2.StunnerConfig, 1)
	defer close(conf)

	var cancelConfigLoader context.CancelFunc
	if flag.NArg() == 1 {
		log.Infof("starting %s with default configuration at TURN URI: %s",
			os.Args[0], flag.Arg(0))

		c, err := stunner.NewDefaultConfig(flag.Arg(0))
		if err != nil {
			log.Errorf("could not load default STUNner config: %s", err.Error())
			os.Exit(1)
		}

		conf <- c

	} else if flag.NArg() == 3 {
		log.Infof("starting %s in tunnel mode: %s -> %s -> %s", os.Args[0], flag.Arg(0),
			flag.Arg(1), flag.Arg(2))
		c, err := tunnelConfig(flag.Arg(0), flag.Arg(1), flag.Arg(2), *sni, *insecure)
		if err != nil {
			log.Errorf("invalid tunnel: %s", err.Error())
			os.Exit(1)
		}
		conf <- c

	} else if !*watch {
		ctx, cancel := context.WithCancel(context.Background())

		if configOrigin == "k8s" {
			log.Info("discovering configuration from Kubernetes")
			cdsAddr, err := discovery.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
				st.GetLogger().NewLogger("cds-fwd"))
			if err != nil {
				log.Errorf("error searching for CDS server: %s", err.Error())
				os.Exit(1)
			}
			configOrigin = cdsAddr.Addr
		}

		log.Infof("loading configuration from origin %q", configOrigin)
		c, err := st.LoadConfig(configOrigin)
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		cancel()

		conf <- c

	} else if *watch {
		log.Info("bootstrapping stunnerd with minimal config")
		z := cdsclient.ZeroConfig(st.GetId())
		conf <- z

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelConfigLoader = cancel

		if configOrigin == "k8s" {
			log.Info("discovering configuration from Kubernetes")
			cdsAddr, err := discovery.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
				st.GetLogger().NewLogger("cds-fwd"))
			if err != nil {
				log.Errorf("error searching for CDS server: %s", err.Error())
				os.Exit(1)
			}
			configOrigin = cdsAddr.Addr
		}

		log.Infof("watching configuration at origin %q (ignoring delete-config updates)", configOrigin)
		if err := st.WatchConfig(ctx, configOrigin, conf, true); err != nil {
			log.Errorf("could not run config watcher: %s", err.Error())
			os.Exit(1)
		}
	} else {
		flag.Usage()
		os.Exit(1)
	}

	sigterm := make(chan os.Signal, 1)
	defer close(sigterm)
	signal.Notify(sigterm, syscall.SIGTERM, syscall.SIGINT)

	exit := make(chan bool, 1)
	defer close(exit)

	for {
		select {
		case <-exit:
			log.Info("normal exit on graceful shutdown")
			os.Exit(0)

		case <-sigterm:
			if *noGracefulShutdown {
				log.Infof("exiting with %d active connection(s): graceful shutdown is disabled",
					st.AllocationCount())
				st.Close()
				os.Exit(0)
			}
			log.Infof("commencing graceful shutdown with %d active connection(s)",
				st.AllocationCount())
			st.Shutdown()

			if cancelConfigLoader != nil {
				log.Info("canceling config loader")
				cancelConfigLoader()
				cancelConfigLoader = nil
			}

			go func() {
				for {
					// check if we can exit
					if st.AllocationCount() == 0 {
						exit <- true
						return
					}
					time.Sleep(time.Second)
				}
			}()

		case c := <-conf:
			// the command line overrides the config
			if *verbose || *level != "" {
				c.Admin.LogLevel = logLevel
			}
			if license != nil {
				c.Admin.LicenseConfig = license
			}
			if *offload != "" {
				c.Admin.OffloadEngine, c.Admin.OffloadInterfaces = *offload, nil
			}
			log.Infof("new configuration available: %q", c.String())

			log.Debug("initiating reconciliation")

			if err := st.Reconcile(c); err != nil {
				if e, ok := err.(stnrv2.ErrRestarted); ok {
					log.Debugf("reconciliation ready: %s", e.Error())
				} else {
					log.Errorf("could not reconcile new configuration "+
						"(running configuration unchanged): %s", err.Error())
				}
			}

			log.Trace("reconciliation ready")
		}
	}
}

// tunnelConfig renders the tunnel-mode stunnerd config from the three positional arguments:
// one plain (or stdin) listener feeding an L4 server, whose single cluster holds the peer and
// reaches it over the peer URI's transport through a tunnel to the TURN server.
func tunnelConfig(clientArg, serverArg, peerArg, sni string, insecure bool) (*stnrv2.StunnerConfig, error) {
	tunnel, auth, proto, err := tunnelServer(serverArg)
	if err != nil {
		return nil, err
	}
	tunnel.Insecure = insecure
	if sni != "" {
		if proto != stnrv2.ProtocolTURNTLS && proto != stnrv2.ProtocolTURNDTLS {
			return nil, fmt.Errorf("--sni is valid only for the TLS/DTLS transports, "+
				"server transport is %q", proto.String())
		}
		tunnel.SNI = sni
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
		},
		// the plain listener authenticates nobody; the tunnel presents the upstream credentials
		Listeners: []stnrv2.ListenerConfig{listener},
		Servers: []stnrv2.ServerConfig{{
			ServerSpec: stnrv2.ServerSpec{
				Name:     "tunnel-server",
				Type:     stnrv2.ServerTypeL4.String(),
				Clusters: []string{"tunnel-cluster"}},
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
