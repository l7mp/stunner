package main

import (
	"flag"
	"net"
	"fmt"
	"os"
	"strings"
	"os/signal"
	"regexp"
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

func main() {
	var realm, level, user string
	var verbose bool
	var port int
	usage := "stunner <-u|--user user1=pwd1> [-r|--realm realm] [-l|--log <turn:TRACE,all:INFO>] -p|--port server_port [server_addr]"
	flag.IntVar(&port,     "p",       3478,              "Port (default: 3478)")
	flag.IntVar(&port,     "port",    3478,              "Port (default: 3478)")
	flag.StringVar(&user,  "u",       "user=pass",       "A pair of username and password (default: \"user=pass\")")
	flag.StringVar(&user,  "user",    "user=pass",       "A pair of username and password (default: \"user=pass\")")
	flag.StringVar(&realm, "r",       "stunner.l7mp.io", "Realm (default: \"stunner.l7mp.io\")")
	flag.StringVar(&realm, "realm",   "stunner.l7mp.io", "Realm (defaults to \"stunner.l7mp.io\")")
	flag.StringVar(&level, "l",       "all:ERROR",       "Log level (default: all:ERROR)")
	flag.StringVar(&level, "log",     "all:ERROR",       "Log level (default: all:ERROR)")
	flag.BoolVar(&verbose, "v",       false,             "Verbose logging, identical to -l all:DEBUG")
	flag.BoolVar(&verbose, "verbose", false,             "Verbose logging, identical to -l all:DEBUG")
	flag.Parse()

	if len(user) == 0 {
		fmt.Println(usage)
		os.Exit(1)
	}

	server := os.Getenv("SERVER_ADDR")
	if len(server) == 0 {
		if flag.NArg() == 1 {
			server = flag.Arg(0)
		} else {
			server = "0.0.0.0"
		}
	}
	
	if verbose {
		level = "all:DEBUG"
	}
	logger := newLogger(level)
	log := logger.NewLogger("stunner")

	// Create a UDP listener to pass into pion/turn
	// pion/turn itself doesn't allocate any UDP sockets, but lets the user pass them in
	// this allows us to add logging, storage or modify inbound/outbound traffic
	serverAddr := fmt.Sprintf("%s:%d", server, port);
	udpListener, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		log.Errorf("failed to create TURN server listener at %s: %s", serverAddr, err)
		os.Exit(1)
	}
	defer udpListener.Close()
	
	log.Infof("Stunner starting at %s, realm='%s'", serverAddr, realm)

	// Cache -users flag for easy lookup later
	// If passwords are stored they should be saved to your DB hashed using turn.GenerateAuthKey
	usersMap := map[string][]byte{}
	for _, kv := range regexp.MustCompile(`(\w+)=(\w+)`).FindAllStringSubmatch(user, -1) {
		usersMap[kv[1]] = turn.GenerateAuthKey(kv[1], realm, kv[2])
	}

	s, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		// Set AuthHandler callback
		// This is called everytime a user tries to authenticate with the TURN server
		// Return the key for that user, or false when no user is found
		AuthHandler: func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			if key, ok := usersMap[username]; ok {
				return key, true
			}
			return nil, false
		},
		LoggerFactory:  logger,
		// PacketConnConfigs is a list of UDP Listeners and the configuration around them
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP(server), // Claim that we are listening on server_addr
					Address:      "0.0.0.0",           // But actually be listening on every interface
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
