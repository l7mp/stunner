package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"github.com/l7mp/stunner/v1"
)

// usage: stunnerd -v turn://user1:passwd1@127.0.0.1:3478?transport=udp

func main() {
	// general flags
	os.Args[0] = "stunnerd"
	var config  = flag.StringP("config", "c", "",      "Config file.")
	var level   = flag.StringP("log", "l", "all:INFO", "Log level (default: all:INFO).")
	var verbose = flag.BoolP("verbose", "v", false,    "Verbose logging, identical to <-l all:DEBUG>.")
	flag.Parse()

	if *verbose {
		*level = "all:DEBUG"
	}

	var stunnerConfig *stunner.StunnerConfig
	if *config == "" && flag.NArg() == 1 {
		// no configfile and we have an url on the command line
		c, err := stunner.NewDefaultStunnerConfig(flag.Arg(0), *level)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err.Error())
			os.Exit(1)
		}
		stunnerConfig = c
	} else if *config != "" {
		c, err := os.ReadFile(*config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not read config: %s\n", err.Error())
			os.Exit(1)
		}

		// substitute environtment variables
		e := os.ExpandEnv(string(c))

		s := stunner.StunnerConfig{}
		if err = yaml.Unmarshal([]byte(e), &s); err != nil {
			fmt.Fprintf(os.Stderr, "Could not parse config file at '%s': %s\n",
				*config, err.Error())
			os.Exit(1)
		}

		// command line loglevel overrides config
		if *level != "" {
			s.Admin.LogLevel = *level
		}

		stunnerConfig = &s
	} else {
		flag.Usage()
		os.Exit(1)
	}

	stunner, err := stunner.NewStunner(stunnerConfig)
	if err != nil {
			fmt.Fprintf(os.Stderr, "Could not create STUNner instance: %s\n",
				err.Error())
		os.Exit(1)
	}
	defer stunner.Close()

	// Block until user sends SIGINT or SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
