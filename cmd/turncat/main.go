package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v3"
	flag "github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

const usage = "turncat [-l|--log <level>] [-i|--insecure] client server peer\n\tclient: <udp|tcp|unix>://<listener_addr>:<listener_port>\n\tserver: <turn://<auth>@<server_addr>:<server_port> | <k8s://<namesspace>/<name>:listener\n\tpeer: udp://<peer_addr>:<peer_port>\n\tauth: <username:password|secret>\n"
const defaultStunnerdConfigfileName = "stunnerd.conf"

var log logging.LeveledLogger
var defaultDuration time.Duration

func main() {
	var Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}

	os.Args[0] = "turncat"
	defaultDuration, _ = time.ParseDuration("1h")
	var level = flag.StringP("log", "l", "all:WARN", "Log level (default: all:WARN).")
	// var user = flag.StringP("user", "u", "", "Set username. Auth fields in the TURN URI override this.")
	// var passwd = flag.StringP("log", "l", "all:WARN", "Log level (default: all:WARN).")
	var insecure = flag.BoolP("insecure", "i", false, "Insecure TLS mode, accept self-signed certificates (default: false).")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to -l all:DEBUG.")
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

	logger := logger.NewLoggerFactory(*level)
	log = logger.NewLogger("turncat-cli")

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
		InsecureMode:  *insecure,
		LoggerFactory: logger,
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
			return nil, fmt.Errorf("Could not read running STUNner configuration from "+
				"Kubernetes: %w", err)
		}
		return conf, nil
	case "turn":
		conf, err := getStunnerConfFromCLI(def)
		if err != nil {
			return nil, fmt.Errorf("Could not generate STUNner configuration from "+
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

	ctx := context.Background()
	cfg := config.GetConfigOrDie()

	cli, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}

	// get the configmap
	lookupKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cm := &corev1.ConfigMap{}

	err = cli.Get(ctx, lookupKey, cm)
	if err != nil {
		return nil, err
	}

	//parse out the stunnerconf
	jsonConf, found := cm.Data[defaultStunnerdConfigfileName]
	if !found {
		return nil, fmt.Errorf("error unpacking STUNner configmap: %s not found",
			defaultStunnerdConfigfileName)
	}

	conf := stnrv1.StunnerConfig{}
	if err := json.Unmarshal([]byte(jsonConf), &conf); err != nil {
		return nil, err
	}

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

	conf.Listeners = []stnrv1.ListenerConfig{{}}
	copy(conf.Listeners, ls)

	return &conf, nil
}

func getStunnerConfFromCLI(def string) (*stnrv1.StunnerConfig, error) {
	uri := fmt.Sprintf("turn://%s", def)

	conf, err := stunner.NewDefaultConfig(uri)
	if err != nil {
		return nil, err
	}

	u, err := stunner.ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("Invalid STUNner URI %q: %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("Username/password must be set: '%s'", uri)
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
	re := regexp.MustCompile(`([0-9A-Za-z_-]+)/([0-9A-Za-z_-]+):([0-9A-Za-z_-]+)`)
	xs := re.FindStringSubmatch(def)
	if len(xs) != 4 {
		return "", "", "", fmt.Errorf("cannot parse STUNner configmap def: %q", def)
	}

	return xs[1], xs[2], xs[3], nil
}
