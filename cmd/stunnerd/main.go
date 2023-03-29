package main

import (
	// "fmt"
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/l7mp/stunner"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// usage: stunnerd -v turn://user1:passwd1@127.0.0.1:3478?transport=udp

const (
	defaultLoglevel  = "all:INFO"
	confUpdatePeriod = 1 * time.Second
)

func main() {
	os.Args[0] = "stunnerd"
	var config = flag.StringP("config", "c", "", "Config file.")
	var level = flag.StringP("log", "l", "", "Log level (default: all:INFO).")
	var watch = flag.BoolP("watch", "w", false, "Watch config file for updates (default: false).")
	var udpThreadNum = flag.IntP("udp-thread-num", "u", 0, "Number of readloop threads (CPU cores) per UDP listener. Zero disables UDP multithreading (default: 0).")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false).")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>.")
	flag.Parse()

	logLevel := defaultLoglevel
	if *verbose {
		// verbose mode on, override any loglevel
		logLevel = "all:DEBUG"
	}
	if *level != "" {
		// loglevel set on the comman line, use that one instead
		logLevel = *level
	}

	st := stunner.NewStunner(stunner.Options{
		LogLevel:             logLevel,
		DryRun:               *dryRun,
		UDPListenerThreadNum: *udpThreadNum,
	})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	conf := make(chan v1alpha1.StunnerConfig, 1)
	defer close(conf)

	var cancelWatcher context.CancelFunc
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
		log.Infof("loading configuration from config file %q", *config)

		c, err := stunner.LoadConfig(*config)
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}

		conf <- *c

	} else if *config != "" && *watch {
		log.Infof("watching configuration file at %q", *config)

		// init stunnerd with an empty config: this bootstraps it with the default
		// resources (above all, starts the health-checker)
		initConf := stunner.NewZeroConfig()
		log.Debug("bootstrapping with zero reconciliation")
		if err := st.Reconcile(*initConf); err != nil {
			log.Errorf("could not reconcile initial configuratoin: %s", err.Error())
			os.Exit(1)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelWatcher = cancel

		if err := stunner.WatchConfig(ctx, stunner.Watcher{
			ConfigFile:    *config,
			ConfigChannel: conf,
			Logger:        st.GetLogger(),
		}); err != nil {
			log.Errorf("could not create config file watcher: %s", err.Error())
			os.Exit(1)
		}
	} else {
		flag.Usage()
		os.Exit(1)
	}

	sigint := make(chan os.Signal, 1)
	defer close(sigint)
	signal.Notify(sigint, syscall.SIGINT)

	sigterm := make(chan os.Signal, 1)
	defer close(sigterm)
	signal.Notify(sigterm, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	for {
		select {
		case <-exit:
			log.Info("normal exit on graceful shutdown")
			os.Exit(0)

		case <-sigint:
			log.Info("normal exit")
			os.Exit(0)

		case <-sigterm:
			log.Info("caught SIGTERM: performing a graceful shutdown")
			st.Shutdown()

			// cancel the config watcher
			if cancelWatcher != nil {
				log.Info("canceling config watcher")
				cancelWatcher()
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
				if e, ok := err.(v1alpha1.ErrRestarted); ok {
					log.Debugf("reconciliation ready: %s", e.Error())
				} else {
					log.Errorf("could not reconcile new configuration: %s, "+
						"rolling back to last running config", err.Error())
				}
			}
		}
	}
}
