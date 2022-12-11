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
	"github.com/pion/turn/v2"
	flag "github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/l7mp/stunner"
	"github.com/l7mp/stunner/internal/logger"
	stunnerv1alpha1 "github.com/l7mp/stunner/pkg/apis/v1alpha1"
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
	var insecure = flag.BoolP("insecure", "i", false, "Insecure TLS mode, accept self-signed certificates (default: false).")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to -l all:DEBUG.")
	flag.Parse()

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
		log.Errorf("Could not read running STUNner configuration: %s", err.Error())
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

func getStunnerConf(uri string) (*stunnerv1alpha1.StunnerConfig, error) {
	s := strings.Split(uri, "://")
	if len(s) < 2 {
		return nil, fmt.Errorf("cannot parse server URI")
	}

	proto, def := s[0], s[1]

	switch proto {
	case "k8s":
		return getStunnerConfFromK8s(def)
	case "turn":
		return getStunnerConfFromCLI(def)
	default:
		return nil, fmt.Errorf("unknown server protocol %q", def)
	}
}

func getStunnerConfFromK8s(def string) (*stunnerv1alpha1.StunnerConfig, error) {
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

	conf := stunnerv1alpha1.StunnerConfig{}
	if err := json.Unmarshal([]byte(jsonConf), &conf); err != nil {
		return nil, err
	}

	// remove all but the named listener
	ls := []stunnerv1alpha1.ListenerConfig{}
	for _, l := range conf.Listeners {
		if l.Name == listener {
			ls = append(ls, l)
		}
	}

	if len(ls) != 1 {
		return nil, fmt.Errorf("cannot find listener %q in STUNner configmap", listener)
	}

	conf.Listeners = []stunnerv1alpha1.ListenerConfig{{}}
	copy(conf.Listeners, ls)

	return &conf, nil
}

func getStunnerConfFromCLI(def string) (*stunnerv1alpha1.StunnerConfig, error) {
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

func getAuth(config *stunnerv1alpha1.StunnerConfig) (stunner.AuthGen, error) {
	auth := config.Auth
	atype, err := stunnerv1alpha1.NewAuthType(auth.Type)
	if err != nil {
		return nil, err
	}

	switch atype {
	case stunnerv1alpha1.AuthTypeLongTerm:
		s, found := auth.Credentials["secret"]
		if !found {
			return nil, fmt.Errorf("cannot find shared secret for %s authentication",
				auth.Type)
		}
		return func() (string, string, error) {
			return turn.GenerateLongTermCredentials(s.String(), defaultDuration)
		}, nil

	case stunnerv1alpha1.AuthTypePlainText:
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

		return func() (string, string, error) { return u.String(), p.String(), nil }, nil

	default:
		return nil, fmt.Errorf("unknown authentication type %q",
			auth.Type)
	}
}

func getStunnerURI(config *stunnerv1alpha1.StunnerConfig) (string, error) {
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

	return fmt.Sprintf("%s://%s:%d", l.Protocol, l.PublicAddr, l.PublicPort), nil
}

func parseK8sDef(def string) (string, string, string, error) {
	re := regexp.MustCompile(`([0-9A-Za-z_-]+)/([0-9A-Za-z_-]+):([0-9A-Za-z_-]+)`)
	xs := re.FindStringSubmatch(def)
	if len(xs) != 4 {
		return "", "", "", fmt.Errorf("cannot parse STUNner configmap def: %q", def)
	}

	return xs[1], xs[2], xs[3], nil
}
