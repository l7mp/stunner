package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/l7mp/stunner/v2/pkg/utils/discovery"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	"github.com/l7mp/stunner/v2"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// tunnelOptions are the tunnel-mode settings picked up from the command line.
type tunnelOptions struct {
	sni       string
	insecure  bool
	logLevel  string
	logFormat string
}

// runTunnel runs stunnerd in tunnel mode: render an in-memory config with one plain (or stdin)
// listener relaying every client flow through the given TURN server to the pinned peer, and runs
// the normal reconcile machinery on it.
func runTunnel(clientArg, serverArg, peerArg string, opts tunnelOptions, k8sConfigFlags *cliopt.ConfigFlags, cdsConfigFlags *discovery.CDSConfigFlags) {
	loggerFactory := logger.NewLoggerFactory(opts.logLevel)
	loggerFactory.SetWriter(os.Stderr)
	log := loggerFactory.NewLogger("tunnel-cli")

	defaultNamespace := ""
	if k8sConfigFlags.Namespace != nil {
		defaultNamespace = *k8sConfigFlags.Namespace
	}
	conf, err := tunnelConfig(clientArg, serverArg, peerArg, defaultNamespace, opts,
		func(u k8sName) (*stnrv1.StunnerConfig, error) {
			return tunnelConfFromK8s(u, k8sConfigFlags, cdsConfigFlags, loggerFactory)
		},
		func(u k8sName) (string, error) {
			return tunnelPeerFromK8s(u, k8sConfigFlags)
		})
	if err != nil {
		log.Errorf("error: %s", err.Error())
		os.Exit(1)
	}

	st := stunner.NewStunner(stunner.Options{
		Name:       "tunnel",
		LogOptions: stunner.LogOptions{Level: opts.logLevel, Format: opts.logFormat, Writer: os.Stderr},
	})
	defer st.Close()

	if err := st.Reconcile(conf); err != nil {
		log.Errorf("could not start the tunnel: %s", err.Error())
		os.Exit(1)
	}
	log.Infof("tunnel running: %s -> %s -> %s", clientArg, serverArg, peerArg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// a stdin tunnel is one-shot: exit once its single flow has ended (stdin EOF or idle)
	if clientArg == "-" {
		go func() {
			seen := false
			for {
				n := st.AllocationCount()
				if n > 0 {
					seen = true
				} else if seen {
					stop()
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutting down")
}

// tunnelConfig renders the tunnel-mode stunnerd config from the three positional arguments:
// one plain (or stdin) listener pinned to the peer, routing to a single TURN-* protocol
// cluster naming the server. Both k8s:// meta-URI families share one parser; the parsed
// names are resolved against the cluster by the fromK8s (gateway listener) and peerFromK8s
// (Service port) callbacks.
func tunnelConfig(clientArg, serverArg, peerArg, defaultNamespace string, opts tunnelOptions,
	fromK8s func(k8sName) (*stnrv1.StunnerConfig, error),
	peerFromK8s func(k8sName) (string, error)) (*stnrv1.StunnerConfig, error) {

	srv, proto, err := tunnelServer(serverArg, defaultNamespace, fromK8s)
	if err != nil {
		return nil, err
	}
	srv.Insecure = opts.insecure
	if opts.sni != "" {
		if proto != stnrv1.ProtocolTURNTLS && proto != stnrv1.ProtocolTURNDTLS {
			return nil, fmt.Errorf("--sni is valid only for the TLS/DTLS transports, "+
				"server transport is %q", proto.String())
		}
		srv.SNI = opts.sni
	}

	listener := stnrv1.ListenerConfig{
		Name:   "tunnel-listener",
		Routes: []string{"tunnel-cluster"},
	}
	if clientArg == "-" {
		listener.Protocol = stnrv1.ProtocolSTDIN.String()
	} else {
		u, err := stunner.ParseURI(clientArg)
		if err != nil {
			return nil, fmt.Errorf("invalid client address %q: %w", clientArg, err)
		}
		p, err := stnrv1.NewProtocol(u.Protocol)
		if err != nil || (p != stnrv1.ProtocolUDP && p != stnrv1.ProtocolTCP) {
			return nil, fmt.Errorf("invalid client address %q: expecting udp:// or "+
				"tcp:// (or \"-\" for stdin)", clientArg)
		}
		listener.Protocol = p.String()
		listener.Addr = u.Address
		listener.Port = u.Port
	}

	// A k8s:// peer names a Service port: resolve it into a peer address once at startup
	// (the Service port spec also names the peer transport). Otherwise the peer URI maps
	// straight onto the listener's peer address: the scheme names the peer transport and
	// the host may be a DNS name resolvable only where the tunnel runs (syntax is checked
	// by the config validation, resolution happens per flow in the engine).
	if u, ok, err := parseK8sURI(peerArg, defaultNamespace); ok {
		if err != nil {
			return nil, err
		}
		resolved, err := peerFromK8s(u)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve peer service %q: %w", peerArg, err)
		}
		peerArg = resolved
	}
	if lower := strings.ToLower(peerArg); !strings.HasPrefix(lower, "udp://") &&
		!strings.HasPrefix(lower, "tcp://") {
		return nil, fmt.Errorf("invalid peer address %q: expecting "+
			"<udp|tcp>://<host>:<port>", peerArg)
	}
	listener.PeerAddr = peerArg

	noHealthCheck := ""
	c := &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			Name:                "tunnel",
			HealthCheckEndpoint: &noHealthCheck,
		},
		// the plain listener authenticates nobody; the upstream credentials live on the
		// TURN server block of the cluster
		Auth:      stnrv1.AuthConfig{Type: "none"},
		Listeners: []stnrv1.ListenerConfig{listener},
		Clusters: []stnrv1.ClusterConfig{{
			Name:       "tunnel-cluster",
			Protocol:   proto.String(),
			TURNServer: srv,
		}},
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// tunnelServer resolves the TURN server argument into an upstream server description and the
// transport to reach it over: directly from a turn:// URI, or from the running dataplane config
// of the named gateway listener for the k8s:// meta-URI.
func tunnelServer(arg, defaultNamespace string, fromK8s func(k8sName) (*stnrv1.StunnerConfig, error)) (*stnrv1.TURNServer, stnrv1.Protocol, error) {
	if u, ok, err := parseK8sURI(arg, defaultNamespace); ok {
		if err != nil {
			return nil, stnrv1.ProtocolUnknown, err
		}
		conf, err := fromK8s(u)
		if err != nil {
			return nil, stnrv1.ProtocolUnknown, err
		}
		l := conf.Listeners[0]
		if l.PublicAddr == "" || l.PublicPort == 0 {
			return nil, stnrv1.ProtocolUnknown,
				fmt.Errorf("no public address/port for listener %q", l.Name)
		}
		proto, err := stnrv1.NewListenerProtocol(l.Protocol)
		if err != nil || !proto.IsTURN() {
			return nil, stnrv1.ProtocolUnknown,
				fmt.Errorf("listener %q is not a TURN listener", l.Name)
		}
		auth := stnrv1.AuthConfig{}
		conf.Auth.DeepCopyInto(&auth)
		return &stnrv1.TURNServer{
			Address: l.PublicAddr,
			Port:    l.PublicPort,
			Auth:    &auth,
		}, proto, nil
	}

	u, err := stunner.ParseURI(arg)
	if err != nil {
		return nil, stnrv1.ProtocolUnknown, fmt.Errorf("invalid TURN server URI %q: %w", arg, err)
	}
	proto, err := stnrv1.NewProtocol(u.Protocol)
	if err != nil || !proto.IsTURN() {
		return nil, stnrv1.ProtocolUnknown, fmt.Errorf("invalid TURN server URI %q: "+
			"not a TURN transport", arg)
	}

	srv := &stnrv1.TURNServer{Address: u.Address, Port: u.Port}
	switch {
	case u.Username != "" && u.Password != "":
		srv.Auth = &stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{
			"username": u.Username, "password": u.Password}}
	case u.Username != "":
		// a bare secret mints per-allocation time-windowed credentials
		srv.Auth = &stnrv1.AuthConfig{Type: "ephemeral", Credentials: map[string]string{
			"secret": u.Username}}
	}
	return srv, proto, nil
}

// tunnelConfFromK8s fetches the running dataplane config of the gateway named in a parsed
// k8s:// URI from the cluster's CDS server and narrows it to the single named listener.
func tunnelConfFromK8s(u k8sName, k8sConfigFlags *cliopt.ConfigFlags, cdsConfigFlags *discovery.CDSConfigFlags, loggerFactory logger.LoggerFactory) (*stnrv1.StunnerConfig, error) {
	namespace, name, listener := u.Namespace, u.Name, u.Component

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cdsAddr, err := discovery.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
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

	// narrow to the named listener; TURN listener names are "<namespace>/<gateway>/<listener>"
	ls := []stnrv1.ListenerConfig{}
	for _, l := range conf.Listeners {
		s := strings.Split(l.Name, "/")
		if len(s) != 3 {
			return nil, fmt.Errorf("error parsing listener name %q, "+
				"expecting <namespace>/<gatewayname>/<listener>", l.Name)
		}
		if s[2] == listener {
			ls = append(ls, l)
		}
	}
	if len(ls) == 0 {
		return nil, fmt.Errorf("cannot find listener %q", listener)
	}
	if len(ls) > 1 {
		return nil, fmt.Errorf("found multiple listeners named %q: disambiguate "+
			"listener names", listener)
	}

	conf.Listeners = conf.Listeners[:1]
	copy(conf.Listeners, ls)
	return conf, nil
}

// tunnelPeerFromK8s resolves a parsed "k8s://<namespace>/<service>:<port-name-or-number>"
// peer URI into a "<udp|tcp>://host:port" peer address from the Service's cluster IP and
// port spec, which also names the peer transport. Resolution happens once at tunnel startup:
// cluster IPs are stable for the Service's lifetime.
func tunnelPeerFromK8s(u k8sName, k8sConfigFlags *cliopt.ConfigFlags) (string, error) {
	namespace, name, portName := u.Namespace, u.Name, u.Component

	restConf, err := k8sConfigFlags.ToRESTConfig()
	if err != nil {
		return "", err
	}
	cs, err := kubernetes.NewForConfig(restConf)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc, err := cs.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return "", fmt.Errorf("service %s/%s has no cluster IP", namespace, name)
	}

	for _, p := range svc.Spec.Ports {
		if p.Name != portName && strconv.Itoa(int(p.Port)) != portName {
			continue
		}
		proto := ""
		switch p.Protocol {
		case corev1.ProtocolUDP, "":
			proto = "udp"
		case corev1.ProtocolTCP:
			proto = "tcp"
		default:
			return "", fmt.Errorf("unsupported protocol %q on port %q of service %s/%s",
				p.Protocol, portName, namespace, name)
		}
		return fmt.Sprintf("%s://%s", proto,
			net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(int(p.Port)))), nil
	}
	return "", fmt.Errorf("no port %q on service %s/%s", portName, namespace, name)
}

// k8sName is a parsed "k8s://<namespace>/<name>:<component>" meta-URI: a gateway and its
// listener in a TURN server URI, a Service and its port in a peer URI.
type k8sName struct {
	Namespace, Name, Component string
}

// k8sURIRe matches the body of a k8s:// meta-URI; the namespace may be empty (the
// "k8s:///<name>:<component>" form), defaulting from the kubeconfig context.
var k8sURIRe = regexp.MustCompile(`^([0-9A-Za-z_-]*)/([0-9A-Za-z_-]+):([0-9A-Za-z_-]+)$`)

// parseK8sURI reports whether the argument is a k8s:// meta-URI and parses it, defaulting an
// empty namespace component to the given one (the kubeconfig context namespace).
func parseK8sURI(arg, defaultNamespace string) (k8sName, bool, error) {
	def, ok := strings.CutPrefix(arg, "k8s://")
	if !ok {
		return k8sName{}, false, nil
	}
	xs := k8sURIRe.FindStringSubmatch(def)
	if xs == nil {
		return k8sName{}, true, fmt.Errorf("cannot parse k8s:// URI %q: expecting "+
			"k8s://<namespace>/<name>:<component>", arg)
	}
	u := k8sName{Namespace: xs[1], Name: xs[2], Component: xs[3]}
	if u.Namespace == "" {
		u.Namespace = defaultNamespace
	}
	if u.Namespace == "" {
		return k8sName{}, true, fmt.Errorf("no namespace in k8s:// URI %q and none in "+
			"the kubeconfig context", arg)
	}
	return u, true, nil
}
