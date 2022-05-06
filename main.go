package main

import (
	"flag"
	"net"
	"fmt"
	"strconv"
	"os"
	"strings"
	"os/signal"
	"syscall"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
)

func newLogger(levelSpec string) *logging.DefaultLoggerFactory{
	logger := logging.NewDefaultLoggerFactory()
	logger.ScopeLevels["turncat"] = logging.LogLevelError

	logLevels := map[string]logging.LogLevel{
		"DISABLE": logging.LogLevelDisabled,
		"ERROR":   logging.LogLevelError,
		"WARN":    logging.LogLevelWarn,
		"INFO":    logging.LogLevelInfo,
		"DEBUG":   logging.LogLevelDebug,
		"TRACE":   logging.LogLevelTrace,
	}

	levels := strings.Split(levelSpec, ",")
	for _, s := range levels {
		scopedLevel := strings.SplitN(s, ":", 2)
		if len(scopedLevel) != 2 {
			continue
		}
		scope := scopedLevel[0]
		level := scopedLevel[1]
		l, found := logLevels[strings.ToUpper(level)]
		if found == false {
			continue
		}

		if strings.ToLower(scope) == "all" {
			logger.DefaultLogLevel = l
			logger.ScopeLevels["turncat"] = l
			continue
		}

		logger.ScopeLevels[scope] = l
	}
	return logger
}

func envDefault(env, def string) string {
	e, ok := os.LookupEnv(env)
	if ok { return e } else { return def }
}

func envDefaultUint(env string, def int) uint16 {
	r, err := strconv.ParseUint(envDefault(env, fmt.Sprintf("%d", def)), 10, 16)
	if err != nil {
		fmt.Printf("Invalid integer value in %s: %d\n", env, def)
		os.Exit(1)
	}
	return uint16(r)
}

////////////////////
func main() {
	usage := "stunner <-u|--user user1=pwd1> [-r|--realm realm] [-l|--log <turn:TRACE,all:INFO>] addr:port"

	// can be overriden on the command line
	dAuth     := envDefault("STUNNER_AUTH",     "static")
	dSecret   := envDefault("STUNNER_SHARED_SECRET", "my-secret")
	dRealm    := envDefault("STUNNER_REALM",    "stunner.l7mp.io")
	dServer   := envDefault("STUNNER_ADDR",     "127.0.0.1")
	dPort 	  := envDefaultUint("STUNNER_PORT",  3478)
	dUser     := envDefault("STUNNER_USERNAME", "user")
	dPasswd	  := envDefault("STUNNER_PASSWORD", "pass")
	dLoglevel := envDefault("STUNNER_LOGLEVEL", "all:ERROR")

	// comes from ENV
	minPort := envDefaultUint("STUNNER_MIN_PORT", 10000)
	maxPort := envDefaultUint("STUNNER_MAX_PORT", 20000)

	var auth, authSecret, realm, user, level string
	var verbose bool
	// general flags
	flag.StringVar(&realm, "r",       dRealm,                fmt.Sprintf("Realm (default: %s)",dRealm))
	flag.StringVar(&realm, "realm",   dRealm,                fmt.Sprintf("Realm (default: %s)",dRealm))
	flag.StringVar(&level, "l",       dLoglevel,             fmt.Sprintf("Log level (default: %s)", dLoglevel))
	flag.StringVar(&level, "log",     dLoglevel,             fmt.Sprintf("Log level (default: %s)", dLoglevel))
	flag.BoolVar(&verbose, "v",       false,                 "Verbose logging, identical to -l all:DEBUG")
	flag.BoolVar(&verbose, "verbose", false,                 "Verbose logging, identical to -l all:DEBUG")

	// authentication mode
	flag.StringVar(&auth,  "a",       dAuth, fmt.Sprintf("Authentication mode: static or long-term (default: %s)", dAuth))
	flag.StringVar(&auth,  "auth",    dAuth, fmt.Sprintf("Authentication mode: static or long-term (default: %s)", dAuth))
	// long-term credential auth
	flag.StringVar(&authSecret, "s",             dSecret, fmt.Sprintf("Shared secret (default: %s)", dSecret))
	flag.StringVar(&authSecret, "shared-secret", dSecret, fmt.Sprintf("Shared secret (default: %s)", dSecret))
	// static username/passwd auth
	flag.StringVar(&user, "u",       dUser + "=" + dPasswd, fmt.Sprintf("Credentials (default: %s)", dUser + ":" + dPasswd))
	flag.StringVar(&user, "user",    dUser + "=" + dPasswd, fmt.Sprintf("Credentials (default: %s)", dUser + ":" + dPasswd))
	flag.Parse()

	if verbose {
		level = "all:DEBUG"
	}
	logger := newLogger(level)
	log := logger.NewLogger("stunner")

	serverAddr := fmt.Sprintf("%s:%d", dServer, dPort);
	if flag.NArg() == 1 { serverAddr = flag.Arg(0) }
	serverIP, _, errSplit := net.SplitHostPort(serverAddr)
	if errSplit != nil {
		fmt.Println(usage)
		log.Errorf("invalid server address %s: %s", serverAddr)
		os.Exit(1)
	}

	// Create a UDP listener to pass into pion/turn
	// pion/turn itself doesn't allocate any UDP sockets, but lets the user pass them in
	// this allows us to add logging, storage or modify inbound/outbound traffic
	udpListener, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		fmt.Println(usage)
		log.Errorf("failed to create TURN server listener at %s: %s", serverAddr, err)
		os.Exit(1)
	}
	defer udpListener.Close()

	var authHandler turn.AuthHandler
	switch strings.ToLower(auth) {
	case "static":
		cred := strings.SplitN(user, "=", 2)
		if len(cred) != 2 || cred[0] == "" || cred[1] == "" {
			fmt.Fprintf(os.Stderr, "cannot parse static credential: '%s'\n", user)
			os.Exit(1)
		}
		passwd := turn.GenerateAuthKey(cred[0], realm, cred[1])
		authHandler = func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			if username == cred[0] {
				return passwd, true
			}
			return nil, false
		}
	case "long-term":
		authHandler = turn.NewLongTermAuthHandler(authSecret, log)
	default:
		log.Errorf("unknown authentication mode: %s", auth)
		os.Exit(1)
	}

	log.Infof("Stunner starting at %s, auth-mode: %s, realm=%s", serverAddr, auth, realm)

	s, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		// Set AuthHandler callback
		// This is called everytime a user tries to authenticate with the TURN server
		// Return the key for that user, or false when no user is found
		AuthHandler: authHandler,
		LoggerFactory:  logger,
		// PacketConnConfigs is a list of UDP Listeners and the configuration around them
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: net.ParseIP(serverIP),
					Address:      serverIP,
					MinPort:      minPort,
					MaxPort:      maxPort,
				},
			},
		},
	})
	if err != nil {
		log.Errorf("cannot set up TURN server: %s", err)
		os.Exit(1)
	}
	defer s.Close()

	// Block until user sends SIGINT or SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
