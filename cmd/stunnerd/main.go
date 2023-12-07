package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// usage: stunnerd -v turn://user1:passwd1@127.0.0.1:3478?transport=udp

const (
	defaultLoglevel = "all:INFO"
	// environment for the config poller
	// defaultDiscoveryAddress = "ws://localhost:13478/api/v1/config/watch"
	envVarName         = "STUNNER_NAME"
	envVarNamespace    = "STUNNER_NAMESPACE"
	envVarConfigOrigin = "STUNNER_CONFIG_ORIGIN"
)

func main() {
	os.Args[0] = "stunnerd"
	var config = flag.StringP("config", "c", "", "Config origin, either a valid URL to the CDS server or a file name (overrides: STUNNER_CONFIG_ORIGIN, default: none).")
	var level = flag.StringP("log", "l", "", "Log level (format: <scope>:<level>, overrides: PION_LOG_*, default: all:INFO).")
	var id = flag.StringP("id", "i", "", "Id for identifying with the CDS server (format: <namespace>/<name>, overrides: STUNNER_NAMESPACE/STUNNER_NAME, default: <hostname>).")
	var watch = flag.BoolP("watch", "w", false, "Watch config file for updates (default: false).")
	var udpThreadNum = flag.IntP("udp-thread-num", "u", 0,
		"Number of readloop threads (CPU cores) per UDP listener. Zero disables UDP multithreading (default: 0).")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false).")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>.")
	flag.Parse()

	logLevel := defaultLoglevel
	if *verbose {
		logLevel = "all:DEBUG"
	}

	if *level != "" {
		logLevel = *level
	}

	if *config == "" {
		origin, ok := os.LookupEnv(envVarConfigOrigin)
		if ok {
			*config = origin
		}
	}

	if *id == "" {
		name, ok1 := os.LookupEnv(envVarName)
		namespace, ok2 := os.LookupEnv(envVarNamespace)
		if ok1 && ok2 {
			*id = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	st := stunner.NewStunner(stunner.Options{
		Id:                   *id,
		LogLevel:             logLevel,
		DryRun:               *dryRun,
		UDPListenerThreadNum: *udpThreadNum,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	log.Infof("starting stunnerd instance %q", *id)

	conf := make(chan stnrv1.StunnerConfig, 1)
	defer close(conf)

	var cancelConfigLoader context.CancelFunc
	if *config == "" && flag.NArg() == 1 {
		log.Infof("starting %s with default configuration at TURN URI: %s",
			os.Args[0], flag.Arg(0))

		c, err := stunner.NewDefaultConfig(flag.Arg(0))
		if err != nil {
			log.Errorf("could not load default STUNner config: %s", err.Error())
			os.Exit(1)
		}

		conf <- *c

	} else if *config != "" && !*watch {
		log.Infof("loading configuration from origin %q", *config)

		c, err := st.LoadConfig(*config)
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}

		conf <- *c

	} else if *config != "" && *watch {
		log.Infof("watching configuration at origin %q", *config)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelConfigLoader = cancel

		// Watch closes the channel
		if err := st.WatchConfig(ctx, *config, conf); err != nil {
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
			log.Infof("performing a graceful shutdown with %d active connection(s)",
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
			log.Trace("new configuration file available")

			// command line loglevel overrides config
			if *verbose || *level != "" {
				c.Admin.LogLevel = logLevel
			}

			// we have working stunnerd: reconcile
			log.Debug("initiating reconciliation")
			err := st.Reconcile(c)
			log.Trace("reconciliation ready")
			if err != nil {
				if e, ok := err.(stnrv1.ErrRestarted); ok {
					log.Debugf("reconciliation ready: %s", e.Error())
				} else {
					log.Errorf("could not reconcile new configuration "+
						"(running configuration unchanged): %s", err.Error())
				}
			}
		}
	}
}
