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

	"github.com/l7mp/stunner/v2/pkg/mediactl/k8s"
	"github.com/l7mp/stunner/v2/pkg/utils/discovery"
	"github.com/pion/logging"

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
	"github.com/l7mp/stunner/v2/pkg/udp"
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
        turn-server-addr: turn://<auth>@<addr>:<port>[?transport=udp|tcp|tls|dtls],
                          or k8s://<gw-namespace>/<gw-name>[/<listener>]
        peer-addr: <udp|tcp>://<addr>:<port>, or k8s://<namespace>/<service>:<port-name-or-number>
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
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false)")
	var noGracefulShutdown = flag.Bool("no-graceful-shutdown", false, "Exit at once on SIGTERM or SIGINT, closing the live sessions, instead of waiting for them to end (default: false)")
	var forceReadyDuringTermination = flag.Bool("force-ready-status", false, "Prevent the server from failing the readiness probe during graceful shutdown as a workaround for buggy kube-proxy implementations (default: false)")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>")
	var sni = flag.String("sni", "", "Server name (SNI) for the TURN/TLS and TURN/DTLS transports (tunnel mode)")
	var insecure = flag.Bool("insecure", false, "Accept self-signed TURN server TLS certificates (tunnel mode, default: false)")
	var licenseArg = flag.String("license", "", `License config as the JSON licensegen writes, {"license_config":{"key":"...","hmac":"..."}}, or just {"key":"...","hmac":"..."} (overrides the config's)`)
	var offload = flag.String("offload", "", `Offload engine on every interface: "none", "xdp", "tc" or "auto" (overrides the config's)`)
	var udpConnect = flag.Bool("enable-udp-listener-connect", false, "Connect a socket back to every client of a UDP listener so the kernel demuxes the clients instead of the read loops, where reuse-port is available (default: false)")

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

	if *udpConnect {
		udp.DefaultListenConfig.ConnectBack = true
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

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	st := stunner.NewStunner(stunner.Options{
		Name:                        *id,
		LogOptions:                  stunner.LogOptions{Level: logLevel, Format: *logFormat},
		DryRun:                      *dryRun,
		NodeName:                    nodeName,
		ForceReadyDuringTermination: *forceReadyDuringTermination,
		BuildInfo:                   buildInfo,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

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
		server, err := resolveServer(flag.Arg(1), k8sConfigFlags, cdsConfigFlags,
			st.GetLogger())
		if err != nil {
			log.Errorf("invalid tunnel: %s", err.Error())
			os.Exit(1)
		}
		peerProto, peer, err := resolvePeer(flag.Arg(2), k8sConfigFlags)
		if err != nil {
			log.Errorf("invalid tunnel: %s", err.Error())
			os.Exit(1)
		}
		c, err := tunnelConfig(flag.Arg(0), server, peerProto, peer, *sni, *insecure)
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

	} else {
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

// tunnelConfig renders the tunnel-mode stunnerd config: one plain (or stdin) listener at the
// client address feeding an L4 server, whose single cluster holds the peer endpoint and reaches it
// over the peer transport through a tunnel to the TURN server, presenting the credentials of the
// server URI.
func tunnelConfig(clientArg string, server *stnrv2.URI, peerProto stnrv2.Protocol, peer, sni string, insecure bool) (*stnrv2.StunnerConfig, error) {
	if !server.Protocol.IsTURN() {
		return nil, fmt.Errorf("invalid TURN server %q: not a TURN transport", server.String())
	}
	tunnel := &stnrv2.TunnelConfig{URL: server.String()}
	if insecure {
		tunnel.TLS = &stnrv2.TLSConfig{Insecure: true}
	}
	if sni != "" {
		if server.Protocol != stnrv2.ProtocolTURNTLS && server.Protocol != stnrv2.ProtocolTURNDTLS {
			return nil, fmt.Errorf("--sni is valid only for the TLS/DTLS transports, "+
				"server transport is %q", server.Protocol.String())
		}
		if tunnel.TLS == nil {
			tunnel.TLS = &stnrv2.TLSConfig{}
		}
		tunnel.TLS.SNI = sni
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

	clusterType := stnrv2.ClusterTypeStatic
	if ep, err := stnrv2.ParseEndpoint(peer); err == nil && ep.Domain != "" {
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
			Name:      "tunnel-cluster",
			Type:      clusterType.String(),
			Endpoints: []string{peer},
			Protocol:  peerProto.String(),
			Tunnel:    tunnel,
		}},
	}
	if auth := server.AuthConfig(); auth != nil {
		auth.Name, tunnel.Auth = "upstream", "upstream"
		c.Auths = []stnrv2.AuthConfig{*auth}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// resolveServer resolves the TURN server argument of the tunnel mode into a TURN URI with the
// credentials the tunnel presents: a TURN URI with the credentials as its userinfo, or a k8s://
// Gateway listener address, the TURN URI of the listener from the Gateway's dataplane config.
func resolveServer(arg string, k8sConfigFlags *cliopt.ConfigFlags, cdsConfigFlags *discovery.CDSConfigFlags, loggerFactory logging.LoggerFactory) (*stnrv2.URI, error) {
	if !k8s.IsAddress(arg) {
		uri, err := stnrv2.ParseURI(arg)
		if err != nil {
			return nil, fmt.Errorf("invalid TURN server URI %q: %w", arg, err)
		}
		return uri, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := k8s.Resolver{K8s: k8sConfigFlags, CDS: cdsConfigFlags, Log: loggerFactory.NewLogger("k8s")}
	l, err := r.Listener(ctx, arg)
	if err != nil {
		return nil, err
	}
	if l.TURN == nil {
		return nil, fmt.Errorf("%s is not a TURN listener", arg)
	}
	return l.TURN, nil
}

// resolvePeer resolves the peer argument of the tunnel mode into the transport and the endpoint
// of the peer: "<udp|tcp>://<host>:<port>", the host possibly a DNS name resolvable only where
// the tunnel runs, or a k8s:// Service address, the first ready pod of the Service with the
// transport of the Service port.
func resolvePeer(arg string, k8sConfigFlags *cliopt.ConfigFlags) (stnrv2.Protocol, string, error) {
	if !k8s.IsAddress(arg) {
		scheme, hostport, _ := strings.Cut(arg, "://")
		proto, err := stnrv2.NewClusterProtocol(scheme)
		if err != nil {
			return stnrv2.ProtocolUnknown, "", fmt.Errorf("invalid peer address %q: "+
				"expecting <udp|tcp>://<host>:<port>", arg)
		}
		ep, err := stnrv2.ParseEndpoint(hostport)
		if err != nil {
			return stnrv2.ProtocolUnknown, "", fmt.Errorf("invalid peer address %q: %w",
				arg, err)
		}
		if _, dialable := ep.HostPort(); !dialable {
			return stnrv2.ProtocolUnknown, "", fmt.Errorf("invalid peer address %q: "+
				"expecting a single host and port", arg)
		}
		return proto, hostport, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return k8s.Resolver{K8s: k8sConfigFlags}.Peer(ctx, arg)
}
