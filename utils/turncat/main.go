package main

import (
	"fmt"
	"context"
	"os"
	"os/signal"
	"time"

	// "bytes"
	// "reflect"

	flag "github.com/spf13/pflag"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/v1"
)

func main() {
	var Usage = func() {
		fmt.Fprintf(os.Stderr, "turncat [-s|--secret=<my-secret> [-d|--duration=<duration>] [-r|--realm realm] [-l|--log <level>] <udp|tcp|unix>://<listener_addr>:<listener_port> turn://<username>:<password>@<server_addr>:<server_port>[?transport=<udp|tcp>] udp://<peer_addr>:<peer_port>")
		flag.PrintDefaults()
	}

	os.Args[0] = "turncat"
	defaultDuration, _ := time.ParseDuration("1h")
 	var realm    = flag.StringP("realm",      "r", "stunner.l7mp.io", "Realm (default: stunner.l7mp.io).")
	var level    = flag.StringP("log",        "l", "all:WARN",        "Log level (default: all:WARN).")
	var verbose  = flag.BoolP("verbose",      "v", false,             "Verbose logging, identical to -l all:DEBUG.")
 	var secret   = flag.StringP("secret",     "s", "",                "Long-term credential shared secret.")
	var duration = flag.DurationP("duration", "d", defaultDuration,   "Long-term credential duration (default: 1h)")
	flag.Parse()

	if flag.NArg() != 3 {
		Usage()
		os.Exit(1)
	}

	var authGen stunner.AuthGen
	if len(*secret) != 0 {
		// switch to long-term credential auth mode
		authGen = func() (string, string, error) {
			return turn.GenerateLongTermCredentials(*secret, *duration)
		}
	} else {
		// plaintext username/passwd
		u := flag.Arg(1)
		uri, err := stunner.ParseUri(u)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could parse Stunner URI '%s': %s\n", u, err)
			os.Exit(1)
		}

		if len(uri.Username) == 0 || len(uri.Password) == 0 {
			fmt.Fprintf(os.Stderr, "no username/password available in Stunner URI: '%s'\n", u)
			os.Exit(1)
		}
		authGen = func() (string, string, error) {
			return uri.Username, uri.Password, nil
		}
	}

	if *verbose {
		*level = "all:DEBUG"
	}
	logger := stunner.NewLoggerFactory(*level)

	cfg := &stunner.TurncatConfig{
		ListenerAddr:  flag.Arg(0),
		ServerAddr:    flag.Arg(1),
		PeerAddr:      flag.Arg(2),
		Realm:         *realm,
		AuthGen:       authGen,
		LoggerFactory: logger,
	}

	t, err := stunner.NewTurncat(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not init turncat: %s\n", err)
		os.Exit(1)
	}

	// exitCh := make(chan os.Signal, 1)
	// signal.Notify(exitCh, os.Interrupt)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	<- ctx.Done()
	t.Close()

}
