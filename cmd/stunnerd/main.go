package main

import (
	"os"
	"log"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"
        "github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"

	"github.com/l7mp/stunner"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// usage: stunnerd -v turn://user1:passwd1@127.0.0.1:3478?transport=udp

const configUpdatePeriod := "1s"

func main() {
	// general flags
	os.Args[0] = "stunnerd"
	var config  = flag.StringP("config", "c", "",      "Config file.")
	var level   = flag.StringP("log", "l", "all:INFO", "Log level (default: all:INFO).")
	var watch   = flag.BoolP("watch", "w", false,      "Watch config file for updates (default: false).")
	var verbose = flag.BoolP("verbose", "v", false,    "Verbose logging, identical to <-l all:DEBUG>.")
	flag.Parse()

	if *verbose {
		*level = "all:DEBUG"
	}

        var s *stunner.Stunner
        defer (func(){ if s != nil { s.Close() }})()

        // watcher
        var watCh *(chan fsnotify.Event)
        var errCh *(chan error)

        // no configfile but we have an url on the command line: start stunner with default config
        if *config == "" && flag.NArg() == 1 {
                conf, err := stunner.NewDefaultConfig(flag.Arg(0))
                if err != nil {
                        log.Fatal("Could not load default STUNner config: %s", err.Error())
                }

                s = makeStunner(s, conf, *level)

                // create dummy channels that will never fire
                watCh = &make(chan fsnotify.Event)
                errCh = &make(chan error)
        } else if *config != "" {
                // we have a config file
                if *watch {
                        watcher, err := fsnotify.NewWatcher()
                        if err != nil {
                                log.Fatalf("Could not create config file watcher%s", err.Error())
                        }
                        defer watcher.Close()

                        if err := watcher.Add(*config); err != nil {
                                log.Fatalf("Could not add config file %q to config file watcher: %s",
                                        *config, err.Error())
                        }

                        watCh = &watcher.Events
                        errCh = &watcher.Errors
                } else {

                        // create dummy channels
                        watCh = &make(chan fsnotify.Event)
                        errCh = &make(chan error)
                        defer close(*watCh)
                        defer close(*errCh)
                }
        } else {
                flag.Usage()
                os.Exit(1)
        }

        // issue a write event so that we immediately load the config
        watCh <- fsnotify.Event{
                Name: *config,
                Op:   fsnotify.Write,
        }

	sigs := make(chan os.Signal, 1)
        signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

        for {
                select {
                case <-sigs:
                        log.Print("Normal exit")
                        os.Exit(0)
                case e := <- watCh:
                        if e.Op != fsnotify.Write {
                                log.Print("Unhnadled notify op on config file %q (ignoring): %s",
                                        e.Name, e.Op.String())
                        }

                        conf, err := stunner.LoadConfig(*config)
                        if err != nil {
                                log.Fatal("Could not (re)read config file: %s",
                                        err.Error())
                        }

                        s = makeStunner(s, conf, *level)
                case e := <- errCh:
                        log.Printf("Error on watcher: %s", e.Error())
                }
        }
}

// starts a new STUNner daemon or reconciles an existing one
func makeStunner(s *stunner.Stunner, conf *v1alpha1.StunnerConfig, level string) *stunner.Stunner {
        // command line loglevel overrides config
        if level != "" {
                conf.Admin.LogLevel = level
        }

        if s == nil {
                st, err := stunner.NewStunner(conf)
                if err != nil {
                        log.Fatalf("Could not create STUNner instance: %s",
                                err.Error())
                }
                s = st

                if err := s.Start(); err != nil {
                        log.Fatalf("Could not start STUNner server: %s",
                                err.Error())
                }
        } else {
                err := s.Reconcile(conf)
                if err != nil {
                        if err == v1alpha1.ErrRestartRequired {
                                s.Close()
                                if err := s.Start(); err != nil {
                                        log.Fatalf("Could not restart STUNner server: %s",
                                                err.Error())
                                }
                        } else {
                                log.Printf("Could not reconcile new configuration (ignoring): %s",
                                        err)
                        }
                }
        }
        return s
}
