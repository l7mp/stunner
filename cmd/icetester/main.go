package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	flag "github.com/spf13/pflag"

	v1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/buildinfo"
	"github.com/l7mp/stunner/pkg/logger"
	"github.com/l7mp/stunner/pkg/whipconn"
)

const (
	// Name of the environment variable specifying the list of ICE servers, default is no ICE servers.
	EnvVarNameICEServers = "ICE_SERVERS"

	// Name of the environment variable specifying the ICE transport policy (either "relay" or "all"), default is "all".
	EnvVarNameICETransportPolicy = "ICE_TRANSPORT_POLICY"

	// HIP bearer token for authenticating WHIP requests, default is no bearer token.
	EnvVarNameBearerToken = "BEARER_TOKEN"

	// WHIP API endpoint, default is "/whip". Must include the leading slash ("/").
	EnvVarNameWHIPEndpoint = "WHIP_ENDPOINT"
)

var (
	version    = "dev"
	commitHash = "n/a"
	buildDate  = "<unknown>"

	defaultICEServers         = []webrtc.ICEServer{}
	defaultICETransportPolicy = webrtc.NewICETransportPolicy("all")
	defaultBearerToken        = ""
	defaultWHIPEndpoint       = "/whip"
	defaultICETesterAddr      = fmt.Sprintf(":%d", v1.DefaultICETesterPort)

	loggerFactory logging.LoggerFactory
	log           logging.LeveledLogger
)

func main() {
	os.Args[0] = "icetester"
	var whipServerAddr = flag.StringP("addr", "a", defaultICETesterAddr, "WHIP server listener address")
	var level = flag.StringP("log", "l", "all:WARN", "Log level")
	var verbose = flag.BoolP("verbose", "v", false, "Enable verbose logging, identical to -l all:DEBUG")

	flag.Parse()

	if *verbose {
		*level = "all:DEBUG"
	}

	loggerFactory = logger.NewLoggerFactory(*level)
	log = loggerFactory.NewLogger("icester")

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	log.Debugf("Starting icetester %s", buildInfo.String())

	iceServers := defaultICEServers
	if os.Getenv(EnvVarNameICEServers) != "" {
		s := []webrtc.ICEServer{}
		if err := json.Unmarshal([]byte(os.Getenv(EnvVarNameICEServers)), &s); err != nil {
			log.Errorf("Environment ICE_SERVERS is invalid: %s", err.Error())
			os.Exit(1)
		}
		iceServers = s
	}

	iceTransportPolicy := defaultICETransportPolicy
	if os.Getenv(EnvVarNameICETransportPolicy) != "" {
		iceTransportPolicy = webrtc.NewICETransportPolicy(os.Getenv(EnvVarNameICETransportPolicy))
	}

	token := defaultBearerToken
	if os.Getenv(EnvVarNameBearerToken) != "" {
		token = os.Getenv(EnvVarNameBearerToken)
	}

	whipEndpoint := defaultWHIPEndpoint
	if os.Getenv(EnvVarNameWHIPEndpoint) != "" {
		endpoint := os.Getenv(EnvVarNameWHIPEndpoint)
		if endpoint[0] != '/' {
			log.Errorf("Environment WHIP_ENDPOINT is invalid: %s, expecting a leading slash '/'", endpoint)
			os.Exit(1)
		}
		whipEndpoint = endpoint
	}

	whipServerConfig := whipconn.Config{
		ICEServers:         iceServers,
		ICETransportPolicy: iceTransportPolicy,
		BearerToken:        token,
		WHIPEndpoint:       whipEndpoint,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := runICETesterListener(ctx, *whipServerAddr, whipServerConfig); err != nil {
		log.Errorf("Could not create WHIP server listener: %s", err.Error())
		os.Exit(1)
	}

	os.Exit(0)
}

func runICETesterListener(ctx context.Context, addr string, config whipconn.Config) error {
	log.Infof("Creating WHIP server listener with config %#v", config)
	l, err := whipconn.NewListener(addr, config, loggerFactory)
	if err != nil {
		return fmt.Errorf("Could not create WHIP server listener: %s", err.Error())
	}

	log.Debug("Creating echo service")
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			log.Debugf("Accepting WHIP server connection with resource ID: %s",
				conn.(*whipconn.ListenerConn).ResourceUrl)

			// readloop
			go func() {
				buf := make([]byte, 100)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}

					_, err = conn.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	<-ctx.Done()

	for _, conn := range l.GetConns() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) &&
			!errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("WHIP connection close error: %s", err.Error())
		}
	}

	l.Close()

	return nil
}
