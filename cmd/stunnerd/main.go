package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/buildinfo"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

var (
	version    = "dev"
	commitHash = "n/a"
	buildDate  = "<unknown>"
)

func main() {
	os.Args[0] = "stunnerd"
	var config = flag.StringP("config", "c", "", "Config origin, either a valid address in the format IP:port, or HTTP URL to the CDS server, or literal \"k8s\" to discover the CDS server from Kubernetes, or a proper file name URI in the format file://<path-to-config-file> (overrides: STUNNER_CONFIG_ORIGIN)")
	var level = flag.StringP("log", "l", "", "Log level (format: <scope>:<level>, overrides: PION_LOG_*, default: all:INFO)")
	var id = flag.StringP("id", "i", "", "Id for identifying with the CDS server (format: <namespace>/<name>, overrides: STUNNER_NAMESPACE/STUNNER_NAME, default: <default/stunnerd-hostname>)")
	var watch = flag.BoolP("watch", "w", false, "Watch config file for updates (default: false)")
	var udpThreadNum = flag.IntP("udp-thread-num", "u", 0,
		"Number of readloop threads (CPU cores) per UDP listener. Zero disables UDP multithreading (default: 0)")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false)")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>")

	// Kubernetes config flags
	k8sConfigFlags := cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(flag.CommandLine)

	// CDS server discovery flags
	cdsConfigFlags := cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(flag.CommandLine)

	flag.Parse()

	logLevel := stnrv1.DefaultLogLevel
	if *verbose {
		logLevel = "all:DEBUG"
	}

	if *level != "" {
		logLevel = *level
	}

	configOrigin := stnrv1.DefaultConfigDiscoveryAddress
	if origin, ok := os.LookupEnv(stnrv1.DefaultEnvVarConfigOrigin); ok {
		configOrigin = origin
	}
	if *config != "" {
		configOrigin = *config
	}

	if *id == "" {
		name, ok1 := os.LookupEnv(stnrv1.DefaultEnvVarName)
		namespace, ok2 := os.LookupEnv(stnrv1.DefaultEnvVarNamespace)
		if ok1 && ok2 {
			*id = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	st := stunner.NewStunner(stunner.Options{
		Name:                 *id,
		LogLevel:             logLevel,
		DryRun:               *dryRun,
		UDPListenerThreadNum: *udpThreadNum,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	log.Infof("Starting stunnerd id %q, STUNner %s ", st.GetId(), buildInfo.String())

	conf := make(chan *stnrv1.StunnerConfig, 1)
	defer close(conf)

	var cancelConfigLoader context.CancelFunc
	if flag.NArg() == 1 {
		log.Infof("Starting %s with default configuration at TURN URI: %s",
			os.Args[0], flag.Arg(0))

		c, err := stunner.NewDefaultConfig(flag.Arg(0))
		if err != nil {
			log.Errorf("Could not load default STUNner config: %s", err.Error())
			os.Exit(1)
		}

		conf <- c

	} else if !*watch {
		ctx, cancel := context.WithCancel(context.Background())

		if configOrigin == "k8s" {
			log.Info("Discovering configuration from Kubernetes")
			cdsAddr, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
				st.GetLogger().NewLogger("cds-fwd"))
			if err != nil {
				log.Errorf("Error searching for CDS server: %s", err.Error())
				os.Exit(1)
			}
			configOrigin = cdsAddr.Addr
		}

		log.Infof("Loading configuration from origin %q", configOrigin)
		c, err := st.LoadConfig(configOrigin)
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		cancel()

		conf <- c

	} else if *watch {
		log.Info("Bootstrapping stunnerd with minimal config")
		z := cdsclient.ZeroConfig(st.GetId())
		conf <- z

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelConfigLoader = cancel

		if configOrigin == "k8s" {
			log.Info("Discovering configuration from Kubernetes")
			cdsAddr, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
				st.GetLogger().NewLogger("cds-fwd"))
			if err != nil {
				log.Errorf("Error searching for CDS server: %s", err.Error())
				os.Exit(1)
			}
			configOrigin = cdsAddr.Addr
		}

		log.Infof("Watching configuration at origin %q (ignoring delete-config updates)", configOrigin)
		if err := st.WatchConfig(ctx, configOrigin, conf, true); err != nil {
			log.Errorf("Could not run config watcher: %s", err.Error())
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
			log.Info("Normal exit on graceful shutdown")
			os.Exit(0)

		case <-sigterm:
			log.Infof("Commencing graceful shutdown with %d active connection(s)",
				st.AllocationCount())
			st.Shutdown()

			if cancelConfigLoader != nil {
				log.Info("Canceling config loader")
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
			log.Infof("New configuration available: %q", c.String())

			// command line loglevel overrides config
			if *verbose || *level != "" {
				c.Admin.LogLevel = logLevel
			}

			log.Debug("Initiating reconciliation")

			if err := st.Reconcile(c); err != nil {
				if e, ok := err.(stnrv1.ErrRestarted); ok {
					log.Debugf("Reconciliation ready: %s", e.Error())
				} else {
					log.Errorf("Could not reconcile new configuration "+
						"(running configuration unchanged): %s", err.Error())
				}
			}

			log.Trace("Reconciliation ready")
		}
	}
}
