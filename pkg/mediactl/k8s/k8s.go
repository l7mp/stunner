// Package k8s resolves the k8s:// addresses of mediactl from a Kubernetes cluster, through the
// kubeconfig of the caller:
//
//	k8s://<namespace>/<gateway>[/<listener>]   the public address of a listener of a STUNner
//	                                            Gateway, read from its served dataplane config
//	k8s://svc/<namespace>/<service>:<port>     a ready pod of a Service, at the port
//
// The listener may be left out when the Gateway has a single one. A TURN listener resolves to its
// TURN URI too, with the credentials of its auth policy.
package k8s

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/logging"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
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

// Listener resolves k8s://<namespace>/<gateway>[/<listener>].
func (r Resolver) Listener(ctx context.Context, addr string) (Listener, error) {
	parts := strings.Split(strings.TrimPrefix(addr, Scheme), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "svc" {
		return Listener{}, fmt.Errorf("invalid Gateway address %q: want %s<namespace>/<gateway>[/<listener>]",
			addr, Scheme)
	}
	ns, gw := parts[0], parts[1]
	pod, err := discovery.DiscoverK8sCDSServer(ctx, r.K8s, r.CDS, r.Log)
	if err != nil {
		return Listener{}, fmt.Errorf("cannot find the config discovery server: %w", err)
	}
	client, err := cdsclient.NewCDSClient(pod.Addr, ns+"/"+gw, nil, r.Log)
	if err != nil {
		return Listener{}, err
	}
	conf, err := client.Load()
	if err != nil {
		return Listener{}, fmt.Errorf("cannot read the config of Gateway %s/%s: %w", ns, gw, err)
	}

	var l stnrv2.ListenerConfig
	switch {
	case len(parts) == 3:
		if l, err = conf.GetListenerConfig(ns + "/" + gw + "/" + parts[2]); err != nil {
			return Listener{}, fmt.Errorf("no listener %[3]q in Gateway %[1]s/%[2]s", ns, gw, parts[2])
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
	if uris, err := conf.GetTURNURIForListener(l.Name, "mediactl", time.Time{}); err == nil {
		res.TURN = &uris[0]
	}
	return res, nil
}

// Peer resolves k8s://svc/<namespace>/<service>:<port> to the address of a ready pod of the
// Service at the port.
func (r Resolver) Peer(ctx context.Context, addr string) (string, error) {
	rest, ok := strings.CutPrefix(addr, Scheme+"svc/")
	parts := strings.Split(rest, "/")
	if !ok || len(parts) != 2 {
		return "", fmt.Errorf("invalid Service address %q: want %ssvc/<namespace>/<service>:<port>",
			addr, Scheme)
	}
	ns := parts[0]
	svc, port, err := net.SplitHostPort(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid Service address %q: %w", addr, err)
	}
	conf, err := r.K8s.ToRESTConfig()
	if err != nil {
		return "", err
	}
	cs, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return "", err
	}
	slices, err := cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + svc})
	if err != nil {
		return "", fmt.Errorf("cannot list the endpoints of Service %s/%s: %w", ns, svc, err)
	}
	for _, es := range slices.Items {
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready || len(ep.Addresses) == 0 {
				continue
			}
			return net.JoinHostPort(ep.Addresses[0], port), nil
		}
	}
	return "", fmt.Errorf("no ready endpoint in Service %s/%s", ns, svc)
}
