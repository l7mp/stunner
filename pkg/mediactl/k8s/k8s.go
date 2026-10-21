// Package k8s resolves the k8s:// addresses of mediactl and the stunnerd tunnel from a Kubernetes
// cluster, through the kubeconfig of the caller:
//
//	k8s://<namespace>/<gateway>[/<listener>]  the public address of a listener of a STUNner
//	                                           Gateway, read from its served dataplane config
//	k8s://<namespace>/<service>:<port>        the first ready pod of a Service, at the target
//	                                           port of the Service port of that number or name
//
// The argument an address is given for decides which form it takes. The listener may be left out
// when the Gateway has a single one, and an empty namespace (k8s:///...) is the namespace of the
// kubeconfig context. A TURN listener resolves to its TURN URI too, with the credentials of its
// auth policy.
package k8s

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pion/logging"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
	"github.com/l7mp/stunner/v2/pkg/mediactl/turn"
	"github.com/l7mp/stunner/v2/pkg/utils/discovery"
)

// Scheme is the prefix of the addresses this package resolves.
const Scheme = "k8s://"

// IsAddress tells whether an address is a k8s:// address.
func IsAddress(addr string) bool { return strings.HasPrefix(addr, Scheme) }

// Listener is a resolved Gateway listener.
type Listener struct {
	// Addr is the public host:port of the listener.
	Addr string
	// TURN is the TURN URI of a TURN listener, with credentials; nil for other listeners.
	TURN *stnrv2.URI
	// Config is the listener's config.
	Config stnrv2.ListenerConfig
}

// Resolver resolves k8s:// addresses.
type Resolver struct {
	K8s *cliopt.ConfigFlags
	CDS *discovery.CDSConfigFlags
	Log logging.LeveledLogger
}

// split splits a k8s:// address into its namespace, the namespace of the kubeconfig context when
// empty, and the rest of its path.
func (r Resolver) split(addr string) (string, []string, error) {
	parts := strings.Split(strings.TrimPrefix(addr, Scheme), "/")
	ns := parts[0]
	if ns == "" {
		ns, _, _ = r.K8s.ToRawKubeConfigLoader().Namespace()
	}
	if ns == "" {
		return "", nil, fmt.Errorf("no namespace in %q and none in the kubeconfig context", addr)
	}
	return ns, parts[1:], nil
}

// Listener resolves k8s://<namespace>/<gateway>[/<listener>].
func (r Resolver) Listener(ctx context.Context, addr string) (Listener, error) {
	ns, path, err := r.split(addr)
	if err != nil {
		return Listener{}, err
	}
	if len(path) < 1 || len(path) > 2 || path[0] == "" {
		return Listener{}, fmt.Errorf("invalid Gateway address %q: want %s<namespace>/<gateway>[/<listener>]",
			addr, Scheme)
	}
	gw := path[0]
	pod, err := discovery.DiscoverK8sCDSServer(ctx, r.K8s, r.CDS, r.Log)
	if err != nil {
		return Listener{}, fmt.Errorf("cannot find the config discovery server: %w", err)
	}
	// the config as served, as stunnerctl config shows it
	api, err := cdsclient.NewConfigNamespaceNameAPI(pod.Addr, ns, gw, nil, r.Log)
	if err != nil {
		return Listener{}, err
	}
	confs, err := api.Get(ctx)
	if err != nil || len(confs) != 1 {
		return Listener{}, fmt.Errorf("cannot read the config of Gateway %s/%s: %v", ns, gw, err)
	}
	conf := confs[0]

	var l stnrv2.ListenerConfig
	switch {
	case len(path) == 2:
		if l, err = conf.GetListenerConfig(ns + "/" + gw + "/" + path[1]); err != nil {
			return Listener{}, fmt.Errorf("no listener %[3]q in Gateway %[1]s/%[2]s", ns, gw, path[1])
		}
	case len(conf.Listeners) == 1:
		l = conf.Listeners[0]
	default:
		names := []string{}
		for _, l := range conf.Listeners {
			names = append(names, strings.TrimPrefix(l.Name, ns+"/"+gw+"/"))
		}
		return Listener{}, fmt.Errorf("several listeners in Gateway %s/%s, %v: name one in %s%s/%s/<listener>",
			ns, gw, names, Scheme, ns, gw)
	}

	host := l.PublicAddr
	if host == "" && len(l.PublicAddrs) > 0 {
		host = l.PublicAddrs[0]
	}
	if host == "" {
		return Listener{}, fmt.Errorf("listener %s has no public address yet", l.Name)
	}
	port := l.PublicPort
	if port == 0 {
		port = l.Port
	}
	res := Listener{Addr: net.JoinHostPort(host, strconv.Itoa(port)), Config: l}
	if uris, err := conf.GetTURNURIForListener(l.Name, "", time.Time{}); err == nil {
		res.TURN = &uris[0]
	}
	return res, nil
}

// Address resolves a k8s:// Gateway address to the listener's public host:port; any other address
// is returned as it is.
func (r Resolver) Address(ctx context.Context, addr string) (string, error) {
	if !IsAddress(addr) {
		return addr, nil
	}
	l, err := r.Listener(ctx, addr)
	if err != nil {
		return "", err
	}
	return l.Addr, nil
}

// TURNDialer resolves a k8s:// Gateway TURN listener to a dialer through it, with the listener's
// credentials.
func (r Resolver) TURNDialer(ctx context.Context, addr string) (turn.Dialer, error) {
	l, err := r.Listener(ctx, addr)
	if err != nil {
		return turn.Dialer{}, err
	}
	if l.TURN == nil {
		return turn.Dialer{}, fmt.Errorf("%s is not a TURN listener", addr)
	}
	return turn.Dialer{URI: l.TURN.String(), Username: l.TURN.Username, Password: l.TURN.Password}, nil
}

// Peer resolves k8s://<namespace>/<service>:<port> to the protocol of the Service port of that
// number or name and the address of the first ready pod of the Service at the target port.
func (r Resolver) Peer(ctx context.Context, addr string) (stnrv2.Protocol, string, error) {
	ns, path, err := r.split(addr)
	if err != nil {
		return stnrv2.ProtocolUnknown, "", err
	}
	var name, port string
	if len(path) == 1 {
		name, port, err = net.SplitHostPort(path[0])
	}
	if len(path) != 1 || err != nil || name == "" || port == "" {
		return stnrv2.ProtocolUnknown, "", fmt.Errorf("invalid Service address %q: want %s<namespace>/<service>:<port>",
			addr, Scheme)
	}
	conf, err := r.K8s.ToRESTConfig()
	if err != nil {
		return stnrv2.ProtocolUnknown, "", err
	}
	cs, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return stnrv2.ProtocolUnknown, "", err
	}
	svc, err := cs.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return stnrv2.ProtocolUnknown, "", fmt.Errorf("cannot find Service %s/%s: %w", ns, name, err)
	}
	i := slices.IndexFunc(svc.Spec.Ports, func(p corev1.ServicePort) bool {
		return p.Name == port || strconv.Itoa(int(p.Port)) == port
	})
	if i < 0 {
		return stnrv2.ProtocolUnknown, "", fmt.Errorf("no port %q in Service %s/%s", port, ns, name)
	}
	sp := svc.Spec.Ports[i]
	proto, err := stnrv2.NewClusterProtocol(string(sp.Protocol))
	if err != nil {
		return stnrv2.ProtocolUnknown, "", fmt.Errorf("unsupported protocol %q of port %q in Service %s/%s",
			sp.Protocol, port, ns, name)
	}
	eps, err := cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + name})
	if err != nil {
		return stnrv2.ProtocolUnknown, "", fmt.Errorf("cannot list the endpoints of Service %s/%s: %w", ns, name, err)
	}
	for _, es := range eps.Items {
		// the endpoint port carries the name of the Service port and the target port
		j := slices.IndexFunc(es.Ports, func(p discoveryv1.EndpointPort) bool {
			return p.Port != nil && (p.Name == nil && sp.Name == "" || p.Name != nil && *p.Name == sp.Name)
		})
		if j < 0 {
			continue
		}
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready || len(ep.Addresses) == 0 {
				continue
			}
			return proto, net.JoinHostPort(ep.Addresses[0], strconv.Itoa(int(*es.Ports[j].Port))), nil
		}
	}
	return stnrv2.ProtocolUnknown, "", fmt.Errorf("no ready endpoint in Service %s/%s", ns, name)
}
