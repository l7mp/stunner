package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
	flag "github.com/spf13/pflag"

	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/buildinfo"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
)

const usage = `turncat [options] <client-addr> <turn-server-addr> <peer-addr>
    client-addr: <udp|tcp|unix>://<listener_addr>:<listener_port>
    turn-server-addr: <turn://<auth>@<server_addr>:<server_port> | <k8s://<gateway-namespace>/<gateway-name>:<gateway-listener>
    peer-addr: udp://<peer_addr>:<peer_port>
    auth: <username:password|secret>
`

var (
	k8sConfigFlags  *cliopt.ConfigFlags
	cdsConfigFlags  *cdsclient.CDSConfigFlags
	log             logging.LeveledLogger
	defaultDuration time.Duration
	loggerFactory   logger.LoggerFactory

	version    = "dev"
	commitHash = "n/a"
	buildDate  = "<unknown>"
)

func main() {
	var Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}

	os.Args[0] = "turncat"
	defaultDuration, _ = time.ParseDuration("1h")

	// Kubernetes config flags
	k8sConfigFlags = cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(flag.CommandLine)

	// CDS server discovery flags
	cdsConfigFlags = cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(flag.CommandLine)

	var serverName string
	flag.StringVar(&serverName, "sni", "", "Server name (SNI) for TURN/TLS client connections")
	var insecure = flag.BoolP("insecure", "i", false, "Insecure TLS mode, accept self-signed TURN server certificates (default: false)")
	var level = flag.StringP("log", "l", "all:WARN", "Log level")
	var verbose = flag.BoolP("verbose", "v", false, "Enable verbose logging, identical to -l all:DEBUG")
	var help = flag.BoolP("help", "h", false, "Display this help text and exit")

	flag.Parse()

	if *help {
		Usage()
		os.Exit(0)
	}

	if flag.NArg() != 3 {
		Usage()
		os.Exit(1)
	}

	if *verbose {
		*level = "all:DEBUG"
	}

	loggerFactory = logger.NewLoggerFactory(*level)
	log = loggerFactory.NewLogger("turncat-cli")

	buildInfo := buildinfo.BuildInfo{Version: version, CommitHash: commitHash, BuildDate: buildDate}
	log.Debugf("Starting turncat %s", buildInfo.String())

	uri := flag.Arg(1)
	log.Debugf("Reading STUNner config from URI %q", uri)
	config, err := getStunnerConf(uri)
	if err != nil {
		log.Errorf("Error: %s", err.Error())
		os.Exit(1)
	}

	log.Debug("Generating STUNner authentication client")
	authGen, err := getAuth(config)
	if err != nil {
		log.Errorf("Could not create STUNner authentication client: %s", err.Error())
		os.Exit(1)
	}

	log.Debug("Generating STUNner URI")
	stunnerURI, err := getStunnerURI(config)
	if err != nil {
		log.Errorf("Could not create STUNner URI: %s", err.Error())
		os.Exit(1)
	}

	log.Debugf("Starting turncat with STUNner URI: %s", stunnerURI)
	cfg := &stunner.TurncatConfig{
		ListenerAddr:  flag.Arg(0),
		ServerAddr:    stunnerURI,
		PeerAddr:      flag.Arg(2),
		Realm:         config.Auth.Realm,
		AuthGen:       authGen,
		ServerName:    serverName,
		InsecureMode:  *insecure,
		LoggerFactory: loggerFactory,
	}
	t, err := stunner.NewTurncat(cfg)
	if err != nil {
		log.Errorf("Could not init turncat: %s\n", err)
		os.Exit(1)
	}

	log.Debug("Entering main loop")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	<-ctx.Done()
	t.Close()
}

func getStunnerConf(uri string) (*stnrv1.StunnerConfig, error) {
	s := strings.Split(uri, "://")
	if len(s) < 2 {
		return nil, fmt.Errorf("cannot parse server URI")
	}

	proto, def := s[0], s[1]

	switch proto {
	case "k8s":
		conf, err := getStunnerConfFromK8s(def)
		if err != nil {
			return nil, fmt.Errorf("could not read running STUNner configuration from "+
				"Kubernetes: %w", err)
		}
		return conf, nil
	case "turn":
		conf, err := getStunnerConfFromCLI(def)
		if err != nil {
			return nil, fmt.Errorf("could not generate STUNner configuration from "+
				"URI %q: %w", uri, err)
		}
		return conf, nil
	default:
		return nil, fmt.Errorf("unknown server protocol %q", def)
	}
}

func getStunnerConfFromK8s(def string) (*stnrv1.StunnerConfig, error) {
	namespace, name, listener, err := parseK8sDef(def)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debug("Searching for CDS server")
	cdsAddr, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
		loggerFactory.NewLogger("cds-fwd"))
	if err != nil {
		return nil, fmt.Errorf("error searching for CDS server: %w", err)
	}

	cds, err := cdsclient.NewConfigNamespaceNameAPI(cdsAddr.Addr, namespace, name, "",
		loggerFactory.NewLogger("cds-client"))
	if err != nil {
		return nil, fmt.Errorf("error creating CDS client: %w", err)
	}

	confs, err := cds.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("error obtaining config from CDS client: %w", err)
	}
	if len(confs) != 1 {
		return nil, fmt.Errorf("invalid number of configs returned from CDS client: %d",
			len(confs))
	}
	conf := confs[0]

	// remove all but the named listener
	ls := []stnrv1.ListenerConfig{}
	for _, l := range conf.Listeners {
		// parse out the listener name (as per the Gateway API) from the TURN listener-name
		// (this is in the form: <namespace>/<gatewayname>/<listener>
		s := strings.Split(l.Name, "/")
		if len(s) != 3 {
			return nil, fmt.Errorf("error parsing listener name %q, "+
				"expecting <namespace>/<gatewayname>/<listener>",
				l.Name)
		}

		if s[2] == listener {
			ls = append(ls, l)
		}
	}

	if len(ls) == 0 {
		return nil, fmt.Errorf("cannot find listener %q", listener)
	}

	if len(ls) > 1 {
		return nil, fmt.Errorf("found multiple listeners named %q: "+
			"either disambiguate listener names or use a fully "+
			"specified TURN server URI", listener)
	}

	conf.Listeners = make([]stnrv1.ListenerConfig, 1)
	copy(conf.Listeners, ls)

	return conf, nil
}

func getStunnerConfFromCLI(def string) (*stnrv1.StunnerConfig, error) {
	uri := fmt.Sprintf("turn://%s", def)

	conf, err := stunner.NewDefaultConfig(uri)
	if err != nil {
		return nil, err
	}

	u, err := stunner.ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid STUNner URI %q: %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("username/password must be set: '%s'", uri)
	}

	conf.Listeners[0].PublicAddr = u.Address
	conf.Listeners[0].PublicPort = u.Port

	return conf, nil
}

func getAuth(config *stnrv1.StunnerConfig) (stunner.AuthGen, error) {
	auth := config.Auth
	atype, err := stnrv1.NewAuthType(auth.Type)
	if err != nil {
		return nil, err
	}

	switch atype {
	case stnrv1.AuthTypeEphemeral:
		s, found := auth.Credentials["secret"]
		if !found {
			return nil, fmt.Errorf("cannot find shared secret for %s authentication",
				auth.Type)
		}
		return func() (string, string, error) {
			return turn.GenerateLongTermCredentials(s, defaultDuration)
		}, nil

	case stnrv1.AuthTypeStatic:
		u, found := auth.Credentials["username"]
		if !found {
			return nil, fmt.Errorf("cannot find username for %s authentication",
				auth.Type)
		}

		p, found := auth.Credentials["password"]
		if !found {
			return nil, fmt.Errorf("cannot find password for %s authentication",
				auth.Type)
		}

		return func() (string, string, error) { return u, p, nil }, nil

	default:
		return nil, fmt.Errorf("unknown authentication type %q",
			auth.Type)
	}
}

func getStunnerURI(config *stnrv1.StunnerConfig) (string, error) {
	// we should have only a single listener at this point
	if len(config.Listeners) != 1 {
		return "", fmt.Errorf("cannot find listener in STUNner configuration: %s",
			config.String())
	}

	l := config.Listeners[0]
	if l.PublicAddr == "" {
		return "", fmt.Errorf("no public address for listener %q", l.Name)
	}
	if l.PublicPort == 0 {
		return "", fmt.Errorf("no public port for listener %q", l.Name)
	}
	if l.Protocol == "" {
		return "", fmt.Errorf("no protocol for listener %q", l.Name)
	}

	return stunner.GetStandardURLFromListener(&l)
}

func parseK8sDef(def string) (string, string, string, error) {
	re := regexp.MustCompile(`^/([0-9A-Za-z_-]+):([0-9A-Za-z_-]+)$`)
	xs := re.FindStringSubmatch(def)
	if len(xs) == 3 && k8sConfigFlags.Namespace != nil {
		return *k8sConfigFlags.Namespace, xs[1], xs[2], nil
	}

	re = regexp.MustCompile(`^([0-9A-Za-z_-]+)/([0-9A-Za-z_-]+):([0-9A-Za-z_-]+)$`)
	xs = re.FindStringSubmatch(def)
	if len(xs) == 4 {
		return xs[1], xs[2], xs[3], nil
	}

	return "", "", "", fmt.Errorf("cannot parse STUNner K8s URI: %q", def)
}
