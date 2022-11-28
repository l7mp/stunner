package main

import (
	// "fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
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

	st := stunner.NewStunner().WithOptions(stunner.Options{LogLevel: logLevel})
	defer st.Close()

	log := st.GetLogger().NewLogger("stunnerd")

	conf := make(chan *v1alpha1.StunnerConfig, 1)
	defer close(conf)

	if *config == "" && flag.NArg() == 1 {
		log.Infof("starting %s with default configuration at TURN URI: %s",
			os.Args[0], flag.Arg(0))

		c, err := stunner.NewDefaultConfig(flag.Arg(0))
		if err != nil {
			log.Errorf("could not load default STUNner config: %s", err.Error())
			os.Exit(1)
		}

		conf <- c

	} else if *config != "" && !*watch {
		log.Infof("loading configuration from config file %q", *config)

		c, err := stunner.LoadConfig(*config)
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}

		conf <- c

	} else if *config != "" && *watch {
		log.Infof("watching configuration file at %q", *config)

		watcherEnabled := false
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Errorf("could not create config file watcher: %s", err.Error())
			os.Exit(1)
		}
		defer watcher.Close()

		if err := watcher.Add(*config); err != nil {
			log.Warnf("could not add config file %q watcher: %s (ignoring as %s is running "+
				"in watch mode)", *config, err.Error(), os.Args[0])
		} else {
			log.Tracef("loading configuration file: %s", *config)
			c, err := stunner.LoadConfig(*config)
			if err != nil {
				log.Warnf("could not load config file: %s", err.Error())
			} else {
				conf <- c
			}
			watcherEnabled = true
		}

		ticker := time.NewTicker(confUpdatePeriod)
		defer ticker.Stop()

		go func() {
			for {
				select {
				case <-ticker.C:
					// log.Tracef("periodic watcher tick: watchlist: %s, watcher enabled: %t",
					//         watcher.WatchList(), watcherEnabled)
					if watcherEnabled {
						continue
					}

					log.Tracef("watcher inactive for config file %q: trying to activate it",
						*config)
					if err := watcher.Add(*config); err != nil {
						log.Warnf("could not add config file %q to watcher: %s",
							*config, err.Error())
						continue
					}
					watcherEnabled = true

					log.Tracef("loading configuration file: %s", *config)
					c, err := stunner.LoadConfig(*config)
					if err != nil {
						log.Warnf("could not load config file: %s", err.Error())
						continue
					}

					conf <- c

				case e := <-watcher.Events:
					log.Debugf("received watcher event: %s", e.String())

					if e.Op == fsnotify.Remove {
						log.Warnf("config file deleted %q, disabling watcher",
							e.Op.String())

						if watcherEnabled {
							if err := watcher.Remove(*config); err != nil {
								log.Warnf("could not remove config file %q "+
									"from watcher: %s", *config, err.Error())
							}
						}

						watcherEnabled = false
						continue
					}

					if e.Op != fsnotify.Write {
						log.Warnf("unhandled notify op on config file %q (ignoring): %s",
							e.Name, e.Op.String())
						continue
					}

					log.Tracef("loading configuration file: %s", *config)
					c, err := stunner.LoadConfig(*config)
					if err != nil {
						log.Warnf("could not load config file: %s", err.Error())
						continue
					}

					conf <- c

				case err := <-watcher.Errors:
					log.Debugf("watcher error, deactivating watcher: %s", err.Error())

					if watcherEnabled {
						if err := watcher.Remove(*config); err != nil {
							log.Warnf("could not remove config file %q from watcher: %s",
								*config, err.Error())
							continue
						}
					}

					watcherEnabled = false
				}
			}
		}()
	} else {
		flag.Usage()
		os.Exit(1)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigs:
			log.Info("normal exit")
			os.Exit(0)

		case c := <-conf:
			log.Trace("new configuration file available")

			// command line loglevel overrides config
			if *verbose || *level != "" {
				c.Admin.LogLevel = logLevel
			}

			// we have working stunnerd: reconcile
			log.Debug("initiating reconciliation")
			err := st.Reconcile(*c)
			log.Trace("reconciliation ready")
			if err != nil {
				if err == v1alpha1.ErrRestartRequired {
					log.Debugf("reconciliation ready: server restarted")
				} else {
					log.Errorf("could not reconcile new configuration: %s, "+
						"rolling back to last running config", err.Error())
				}
			}
		}
	}
}
