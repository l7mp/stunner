package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
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
                          or k8s://<gw-namespace>/<gw-name>:<listener>
        peer-addr: <udp|tcp>://<addr>:<port>
                   or k8s://<namespace>/<service>:<port-name-or-number>
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
	var udpThreadNum = flag.IntP("udp-thread-num", "u", 0,
		"Number of readloop threads (CPU cores) per UDP listener. Zero disables UDP multithreading (default: 0)")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false)")
	var forceReadyDuringTermination = flag.Bool("force-ready-status", false, "Prevent the server from failing the liveness probe during graceful shutdown as a workaround for buggy kube-proxy implementations (default: false)")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>")
	var sni = flag.String("sni", "", "Server name (SNI) for the TURN/TLS and TURN/DTLS transports (tunnel mode)")
	var insecure = flag.Bool("insecure", false, "Accept self-signed TURN server TLS certificates (tunnel mode, default: false)")

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

	logLevel := stnrv1.DefaultLogLevel
	if *verbose {
		logLevel = "all:DEBUG"
	}

	if *level != "" {
		logLevel = *level
	}

	// Three positional arguments select tunnel mode, with turncat's argument shape. The
	// tunnel is a pipe-friendly CLI: quiet by default, unless a log level is set.
	if flag.NArg() == 3 {
		if !*verbose && *level == "" {
			logLevel = "all:WARN"
		}
		runTunnel(flag.Arg(0), flag.Arg(1), flag.Arg(2), tunnelOptions{
			sni:       *sni,
			insecure:  *insecure,
			logLevel:  logLevel,
			logFormat: *logFormat,
		}, k8sConfigFlags, cdsConfigFlags)
		return
	}
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(1)
	}

	configOrigin := stnrv1.DefaultConfigDiscoveryAddress
	if origin, ok := os.LookupEnv(stnrv1.DefaultEnvVarConfigOrigin); ok {
		configOrigin = origin
	}
	if *config != "" {
		configOrigin = *config
	}

	nodeName := ""
	if node, ok := os.LookupEnv(stnrv1.DefaultEnvVarNodeName); ok {
		nodeName = node
	}

	if *id == "" {
		name, ok1 := os.LookupEnv(stnrv1.DefaultEnvVarName)
		namespace, ok2 := os.LookupEnv(stnrv1.DefaultEnvVarNamespace)
		if ok1 && ok2 {
			*id = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	st := stunner.NewStunner(stunner.Options{
		Name:                        *id,
		LogOptions:                  stunner.LogOptions{Level: logLevel, Format: *logFormat},
		DryRun:                      *dryRun,
		NodeName:                    nodeName,
		UDPListenerThreadNum:        *udpThreadNum,
		ForceReadyDuringTermination: *forceReadyDuringTermination,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	log.Infof("starting stunnerd id %q, STUNner %s ", st.GetId(), buildInfo.String())

	conf := make(chan *stnrv1.StunnerConfig, 1)
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
			log.Infof("new configuration available: %q", c.String())

			// command line loglevel overrides config
			if *verbose || *level != "" {
				c.Admin.LogLevel = logLevel
			}

			log.Debug("initiating reconciliation")

			if err := st.Reconcile(c); err != nil {
				if e, ok := err.(stnrv1.ErrRestarted); ok {
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
