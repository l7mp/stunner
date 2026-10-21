// Package icetester runs the ICE test of stunnerctl: it installs test Gateways and a WebRTC echo
// backend in a namespace of its own, then connects to the backend through every test Gateway, over
// TURN by WHIP, and streams packets to the echo. Every step is reported as an Event. The test
// removes everything it installed when it ends.
package icetester

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/mediactl"
	"github.com/l7mp/stunner/v2/pkg/utils/discovery"
	"github.com/l7mp/stunner/v2/pkg/whipconn"
)

const (
	// DefaultImage is the image of the ICE tester backend.
	DefaultImage = "docker.io/l7mp/icetester:latest"
	// DefaultNamespace is the namespace the test runs in.
	DefaultNamespace = "icetest"
	// DefaultTimeout bounds a whole test.
	DefaultTimeout = 5 * time.Minute
	// DefaultPacketRate is the packets per second of the test stream.
	DefaultPacketRate = 50

	// streamLength is the size of a test packet: the backend echoes at most 100 bytes
	streamLength = 100
	// streamTime is how long the test stream of a connection runs
	streamTime = 20 * time.Second
	// cleanupTimeout bounds the removal of the test resources, which runs after the test
	// context may have ended
	cleanupTimeout = 30 * time.Second
)

// crdGVR is the resource of the custom resource definitions.
var crdGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1",
	Resource: "customresourcedefinitions"}

// crds are the custom resources the test needs.
var crds = []string{
	// Gateway API
	"gatewayclasses.gateway.networking.k8s.io",
	"gateways.gateway.networking.k8s.io",
	"tcproutes.gateway.networking.k8s.io",
	"udproutes.gateway.networking.k8s.io",
	// STUNner
	"dataplanes.stunner.l7mp.io",
	"gatewayconfigs.stunner.l7mp.io",
	"staticservices.stunner.l7mp.io",
	"udproutes.stunner.l7mp.io",
}

// Config is the configuration of an ICE test.
type Config struct {
	// EventChannel receives the events of the test; the test waits for every send, so the
	// channel must be drained. Nil reports nothing.
	EventChannel chan<- Event

	// K8sConfigFlags, CDSConfigFlags and AuthConfigFlags find the cluster, the operator and the
	// auth service, as for the other stunnerctl commands.
	K8sConfigFlags  *cliopt.ConfigFlags
	CDSConfigFlags  *discovery.CDSConfigFlags
	AuthConfigFlags *discovery.AuthConfigFlags

	// Namespace is the namespace the test creates and runs in, DefaultNamespace if empty. A
	// namespace that exists stops the test, unless ForceCleanup removes it first.
	Namespace    string
	ForceCleanup bool
	// TURNTransports are the TURN transports to test, TURN-UDP and TURN-TCP.
	TURNTransports []stnrv2.Protocol
	// Image is the image of the backend, DefaultImage if empty.
	Image string
	// PacketRate is the packets per second of the test stream, DefaultPacketRate if zero.
	PacketRate int
	// OffloadEngine is the offload engine of the test Dataplane.
	OffloadEngine stnrv2.OffloadMode
	// AllowNodePort accepts a Gateway exposed on a NodePort, on any public port.
	AllowNodePort bool

	Logger logger.LoggerFactory
}

// Tester runs an ICE test.
type Tester struct {
	cfg    Config
	dyn    *dynamic.DynamicClient
	cs     *kubernetes.Clientset
	mapper meta.RESTMapper
	log    logging.LeveledLogger
}

// New creates an ICE tester, filling the defaults of the config.
func New(cfg Config) *Tester {
	if cfg.Logger == nil {
		cfg.Logger = logger.NewLoggerFactory("all:INFO")
	}
	if cfg.Namespace == "" {
		cfg.Namespace = DefaultNamespace
	}
	if cfg.Image == "" {
		cfg.Image = DefaultImage
	}
	if cfg.PacketRate == 0 {
		cfg.PacketRate = DefaultPacketRate
	}
	return &Tester{cfg: cfg, log: cfg.Logger.NewLogger("icetester")}
}

// Run runs the test until it ends or fails, or the context ends. It returns the error of the
// failed step, also reported in the step's event.
func (t *Tester) Run(ctx context.Context) error {
	ns := newICETesterNamespace(t.cfg.Namespace)

	t.begin(ctx, EventInit, "")
	created, diag, err := t.init(ctx, ns)
	if created {
		// only what the test created is removed, on a context of its own: the test context
		// may have ended
		defer t.cleanup(ns)
	}
	if err != nil {
		return t.end(ctx, EventInit, "", err, diag)
	}
	t.end(ctx, EventInit, "", nil, "") //nolint:errcheck

	t.begin(ctx, EventInstallationComplete, "")
	ep, diag, err := t.install(ctx)
	if err != nil {
		return t.end(ctx, EventInstallationComplete, "", err, diag)
	}
	t.end(ctx, EventInstallationComplete, "", nil, "") //nolint:errcheck

	t.begin(ctx, EventGatewayAvailable, "")
	for _, proto := range t.cfg.TURNTransports {
		if diag, err := t.awaitGateway(ctx, ep.cds, gwFromProto(proto, t.cfg.Namespace)); err != nil {
			return t.end(ctx, EventGatewayAvailable, "", err, diag)
		}
	}
	t.end(ctx, EventGatewayAvailable, "", nil, "") //nolint:errcheck

	t.begin(ctx, EventICEConfigAvailable, "")
	iceConfigs := map[stnrv2.Protocol]webrtc.Configuration{}
	for _, proto := range t.cfg.TURNTransports {
		c, err := t.iceConfig(ctx, ep.auth, gwFromProto(proto, t.cfg.Namespace).GetName())
		if err != nil {
			return t.end(ctx, EventICEConfigAvailable, "", err, diagAuthServiceConnectionFailed)
		}
		iceConfigs[proto] = c
	}
	t.end(ctx, EventICEConfigAvailable, "", nil, "") //nolint:errcheck

	for _, typ := range []EventType{EventAsymmetricICETest, EventSymmetricICETest} {
		for _, proto := range t.cfg.TURNTransports {
			if err := t.iceTest(ctx, typ, proto, ep.backend, iceConfigs[proto]); err != nil {
				return err
			}
		}
	}
	return nil
}

// init creates the test namespace, removing a leftover one if asked to, and the test Dataplane. It
// reports whether it created the namespace.
func (t *Tester) init(ctx context.Context, ns *unstructured.Unstructured) (bool, string, error) {
	k8sConfig, err := t.cfg.K8sConfigFlags.ToRESTConfig()
	if err != nil {
		return false, diagK8sConfigUnavailable, fmt.Errorf("error building a Kubernetes config: %w", err)
	}
	if t.dyn, err = dynamic.NewForConfig(k8sConfig); err != nil {
		return false, diagK8sClientError, fmt.Errorf("error creating a Kubernetes client: %w", err)
	}
	if t.cs, err = kubernetes.NewForConfig(k8sConfig); err != nil {
		return false, diagK8sClientError, fmt.Errorf("error creating a Kubernetes client: %w", err)
	}
	dc, err := k8sdiscovery.NewDiscoveryClientForConfig(k8sConfig)
	if err != nil {
		return false, diagK8sClientError, fmt.Errorf("error creating a Kubernetes discovery client: %w", err)
	}
	t.mapper = restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	t.log.Infof("creating the test namespace %s", ns.GetName())
	_, err = t.get(ctx, ns)
	switch {
	case err == nil && !t.cfg.ForceCleanup:
		return false, diagNamespaceAlreadyExists, fmt.Errorf("namespace %s already exists, halting test", ns.GetName())
	case err == nil:
		if err := t.remove(ctx, ns); err != nil {
			return false, diagFailedToQueryOrCreateArtifacts, fmt.Errorf("failed to clean up tester namespace %s "+
				"(--force-cleanup enforced): %w", ns.GetName(), err)
		}
	case !apierrors.IsNotFound(err):
		return false, diagFailedToCreateNamespace, fmt.Errorf("error querying testing namespace %s: %w", ns.GetName(), err)
	}
	if err := t.create(ctx, ns); err != nil {
		return false, diagFailedToCreateNamespace, fmt.Errorf("error creating a namespace for running the tests: %w", err)
	}

	t.log.Info("creating the test Dataplane")
	if _, err := t.makeDataplane(ctx); err != nil {
		return true, diagFailedToQueryOrCreateArtifacts, err
	}
	return true, "", nil
}

// cleanup removes everything the test installed, the namespace last.
func (t *Tester) cleanup(ns *unstructured.Unstructured) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	objs := newICETesterICETesterResources(t.cfg.Namespace, t.cfg.Image, t.cfg.TURNTransports)
	objs = append(objs, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "stunner.l7mp.io/v1", "kind": "Dataplane",
		"metadata": map[string]any{"name": icetesterDataplaneName},
	}}, ns)
	for _, obj := range objs {
		if err := t.delete(ctx, obj); err != nil {
			t.log.Errorf("error deleting %s %s: %s", obj.GetKind(), obj.GetName(), err.Error())
		}
	}
}

// endpoints are the addresses the test reaches the services of the cluster on: the local ends of
// the port-forwards to the backend, the operator's config discovery server and the auth service.
type endpoints struct{ backend, cds, auth string }

// install checks the CRDs, creates the test resources and waits for the backend, then finds the
// backend, the operator and the auth service.
func (t *Tester) install(ctx context.Context) (endpoints, string, error) {
	ep := endpoints{}
	t.log.Info("checking the CRDs")
	for _, crd := range crds {
		if _, err := t.dyn.Resource(crdGVR).Get(ctx, crd, metav1.GetOptions{}); err != nil {
			return ep, diagFailedToQueryOrCreateArtifacts, fmt.Errorf("failed to query CRD %s: %w", crd, err)
		}
	}

	t.log.Info("creating the test resources")
	for _, obj := range newICETesterICETesterResources(t.cfg.Namespace, t.cfg.Image, t.cfg.TURNTransports) {
		if err := t.create(ctx, obj); err != nil {
			return ep, diagFailedToQueryOrCreateArtifacts, fmt.Errorf("failed to create %s %s/%s: %w",
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}

	t.log.Info("waiting for the test backend")
	backend := newICETesterBackendPod(t.cfg.Namespace, t.cfg.Image)
	if err := t.awaitPods(ctx, 30*time.Second, "app=icetester"); err != nil {
		return ep, diagFailedToQueryOrCreateArtifacts, fmt.Errorf("ICE tester backend %s/%s not ready: %w",
			backend.GetNamespace(), backend.GetName(), err)
	}

	pod, err := discovery.DiscoverK8sPod(ctx, t.cfg.K8sConfigFlags, t.cfg.Namespace, "app=icetester",
		stnrv2.DefaultICETesterPort, t.cfg.Logger.NewLogger("backend-fwd"))
	if err != nil {
		return ep, diagICETesterBackendUnavailable, fmt.Errorf("error searching for ICE tester backend pod: %w", err)
	}
	ep.backend = pod.Addr

	t.log.Info("searching for the CDS server")
	pod, err = discovery.DiscoverK8sCDSServer(ctx, t.cfg.K8sConfigFlags, t.cfg.CDSConfigFlags,
		t.cfg.Logger.NewLogger("cds-fwd"))
	if err != nil {
		return ep, diagCDSServerUnavailable, fmt.Errorf("error searching for CDS server: %w", err)
	}
	ep.cds = pod.Addr

	t.log.Info("searching for the auth service")
	pod, err = discovery.DiscoverK8sAuthServer(ctx, t.cfg.K8sConfigFlags, t.cfg.AuthConfigFlags,
		t.cfg.Logger.NewLogger("auth-fwd"))
	if err != nil {
		return ep, diagAuthServiceUnavailable, fmt.Errorf("error searching for auth service: %w", err)
	}
	ep.auth = pod.Addr
	return ep, "", nil
}

// awaitGateway waits until the served config of a test Gateway has an endpoint and a public
// address, on the listener port unless NodePorts are allowed, then until its dataplane pods are
// ready.
func (t *Tester) awaitGateway(ctx context.Context, cdsAddr string, gw *unstructured.Unstructured) (string, error) {
	t.log.Infof("waiting for a public address for Gateway %s/%s", gw.GetNamespace(), gw.GetName())
	cds, err := cdsclient.NewConfigNamespaceNameAPI(cdsAddr, gw.GetNamespace(), gw.GetName(), nil,
		t.cfg.Logger.NewLogger("cds-client"))
	if err != nil {
		return diagCDSServerConnectionFailed, fmt.Errorf("could not connect to the CDS server: %w", err)
	}
	if err := eventually(ctx, 60*time.Second, func(ctx context.Context) (bool, error) {
		confs, err := cds.Get(ctx)
		if err != nil || len(confs) != 1 {
			return false, nil // the operator has not rendered the Gateway yet
		}
		if !slices.ContainsFunc(confs[0].Clusters, func(c stnrv2.ClusterConfig) bool { return len(c.Endpoints) > 0 }) {
			return false, nil
		}
		return slices.ContainsFunc(confs[0].Listeners, func(l stnrv2.ListenerConfig) bool {
			return l.PublicAddr != "" && (l.PublicPort == listenerPort || t.cfg.AllowNodePort && l.PublicPort != 0)
		}), nil
	}); err != nil {
		return diagPublicAddrNotFound, fmt.Errorf("no public address for Gateway %s/%s: %w",
			gw.GetNamespace(), gw.GetName(), err)
	}

	t.log.Infof("waiting for the dataplane of Gateway %s/%s", gw.GetNamespace(), gw.GetName())
	selector := fmt.Sprintf("%s=%s,%s=%s,%s=%s", stnrv2.DefaultAppLabelKey, stnrv2.DefaultAppLabelValue,
		stnrv2.DefaultRelatedGatewayNamespace, gw.GetNamespace(), stnrv2.DefaultRelatedGatewayKey, gw.GetName())
	if err := t.awaitPods(ctx, 60*time.Second, selector); err != nil {
		return diagDataplaneUnavailable, fmt.Errorf("dataplane of Gateway %s/%s not ready: %w",
			gw.GetNamespace(), gw.GetName(), err)
	}
	return "", nil
}

// awaitPods waits until there is a pod with the labels in the test namespace and all of them are
// running and ready.
func (t *Tester) awaitPods(ctx context.Context, timeout time.Duration, selector string) error {
	return eventually(ctx, timeout, func(ctx context.Context) (bool, error) {
		pods, err := t.cs.CoreV1().Pods(t.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, p := range pods.Items {
			if p.Status.Phase != corev1.PodRunning || !slices.ContainsFunc(p.Status.Conditions, func(c corev1.PodCondition) bool {
				return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
			}) {
				return false, nil
			}
		}
		return true, nil
	})
}

// iceConfig obtains the ICE server configuration of a Gateway from the auth service, retrying while
// the auth service has not learned the Gateway yet.
func (t *Tester) iceConfig(ctx context.Context, authAddr, gw string) (webrtc.Configuration, error) {
	t.log.Infof("obtaining ICE server config for Gateway %s/%s", t.cfg.Namespace, gw)
	conf := webrtc.Configuration{}
	err := eventually(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		b, err := discovery.GetAuth(ctx, authAddr, discovery.AuthQuery{Namespace: t.cfg.Namespace, Gateway: gw})
		if err != nil {
			return false, nil
		}
		return true, json.Unmarshal(b, &conf)
	})
	if err != nil {
		return conf, fmt.Errorf("could not obtain ICE server config for Gateway %s/%s: %w", t.cfg.Namespace, gw, err)
	}
	return conf, nil
}

// iceTest connects to the backend over a TURN transport and streams packets to the echo. In a
// symmetric test the backend uses the same Gateway as a TURN server, through its Service.
func (t *Tester) iceTest(ctx context.Context, typ EventType, proto stnrv2.Protocol, backendAddr string, iceConfig webrtc.Configuration) error {
	transport := proto.String()
	t.begin(ctx, typ, transport)
	t.log.Infof("%s over %s", typ.String(), transport)

	listenerConfig := whipconn.Config{}
	if typ == EventSymmetricICETest {
		gw := gwFromProto(proto, t.cfg.Namespace)
		listenerConfig.ICEServers = viaService(iceConfig.ICEServers,
			fmt.Sprintf("%s.%s.svc.cluster.local", gw.GetName(), gw.GetNamespace()))
		listenerConfig.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	if err := eventually(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		return true, setBackendConfig(ctx, backendAddr, listenerConfig)
	}); err != nil {
		return t.end(ctx, typ, transport, fmt.Errorf("could not configure the ICE tester backend: %w", err),
			diagICETesterBackendUnavailable)
	}

	dialerConfig := listenerConfig
	dialerConfig.ICEServers = iceConfig.ICEServers
	dialerConfig.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	dialerConfig.WHIPEndpoint = whipconn.WhipEndpoint
	var conn net.Conn
	if err := eventually(ctx, 90*time.Second, func(ctx context.Context) (bool, error) {
		c, err := whipconn.NewDialer(dialerConfig, t.cfg.Logger).DialContext(ctx, backendAddr)
		if err != nil {
			t.log.Debugf("WHIP dial failed, retrying: %s", err.Error())
			return false, nil
		}
		conn = c
		return true, nil
	}); err != nil {
		return t.end(ctx, typ, transport, fmt.Errorf("could not connect to the ICE tester backend: %w", err),
			diagICETestFailed)
	}

	local, remote, err := candidates(conn)
	if err != nil {
		conn.Close() //nolint:errcheck
		return t.end(ctx, typ, transport, err, diagICETestFailed)
	}

	sctx, cancel := context.WithTimeout(ctx, streamTime)
	defer cancel()
	// the stream closes the conn
	res := mediactl.Stream(sctx, conn, mediactl.Config{Length: streamLength,
		Rate: float64(t.cfg.PacketRate), Echo: true}).Result()
	if res.Received == 0 {
		return t.end(ctx, typ, transport, fmt.Errorf("no echo came back: %s", res), diagICETestFailed)
	}
	t.send(ctx, Event{Type: typ, Transport: transport, Result: &res, Local: local, Remote: remote,
		Timestamp: time.Now()})
	return nil
}

// setBackendConfig sets the config of the backend's WHIP listener and reads it back.
func setBackendConfig(ctx context.Context, addr string, c whipconn.Config) error {
	u := url.URL{Scheme: "http", Host: addr, Path: "/config"}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	for _, req := range []struct {
		method string
		body   io.Reader
	}{{http.MethodPost, bytes.NewReader(b)}, {http.MethodGet, nil}} {
		r, err := http.NewRequestWithContext(ctx, req.method, u.String(), req.body)
		if err != nil {
			return err
		}
		r.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close() //nolint:errcheck
		if err != nil {
			return err
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("%s %s: HTTP error %s", req.method, u.String(), res.Status)
		}
		if req.method == http.MethodGet {
			return json.Unmarshal(body, &c)
		}
	}
	return nil
}

// viaService rewrites the host of every ICE server URI to the address of the Gateway's Service, so
// a pod in the cluster reaches the TURN server.
func viaService(servers []webrtc.ICEServer, host string) []webrtc.ICEServer {
	ret := []webrtc.ICEServer{}
	for _, s := range servers {
		urls := []string{}
		for _, u := range s.URLs {
			uri, err := stnrv2.ParseURI(u)
			if err != nil {
				urls = append(urls, u)
				continue
			}
			uri.Host = host
			urls = append(urls, uri.String())
		}
		ret = append(ret, webrtc.ICEServer{URLs: urls, Username: s.Username, Credential: s.Credential})
	}
	return ret
}

// candidates returns the local and the remote ICE candidates of a WHIP connection, marking the
// candidates of the selected pair.
func candidates(conn net.Conn) ([]Candidate, []Candidate, error) {
	wc, ok := conn.(*whipconn.DialerConn)
	if !ok {
		return nil, nil, errors.New("not a WHIP connection")
	}
	pc := wc.GetPeerConnection()
	pair, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find the selected ICE candidate pair: %w", err)
	}
	if pair == nil {
		return nil, nil, errors.New("no selected ICE candidate pair")
	}

	parse := func(desc *webrtc.SessionDescription, selected string) []Candidate {
		ret := []Candidate{}
		if desc == nil {
			return ret
		}
		sdp, err := desc.Unmarshal()
		if err != nil {
			return ret
		}
		for _, m := range sdp.MediaDescriptions {
			for _, attr := range m.Attributes {
				if !attr.IsICECandidate() {
					continue
				}
				c, err := ice.UnmarshalCandidate(attr.String())
				if err != nil {
					continue
				}
				// a candidate may be listed more than once
				cand := c.String()
				if !slices.ContainsFunc(ret, func(d Candidate) bool { return d.Candidate == cand }) {
					ret = append(ret, Candidate{Candidate: cand, Selected: cand == selected})
				}
			}
		}
		return ret
	}
	return parse(pc.LocalDescription(), pair.Local.String()), parse(pc.RemoteDescription(), pair.Remote.String()), nil
}

// gwFromProto returns the test Gateway of a TURN transport.
func gwFromProto(proto stnrv2.Protocol, ns string) *unstructured.Unstructured {
	if proto == stnrv2.ProtocolTURNTCP {
		return newICETesterTCPGateway(ns)
	}
	return newICETesterUDPGateway(ns)
}

// eventually checks a condition every 250 ms until it holds, returns an error, or the timeout or
// the context ends.
func eventually(ctx context.Context, timeout time.Duration, cond func(ctx context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := cond(ctx)
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// begin reports the start of a step.
func (t *Tester) begin(ctx context.Context, typ EventType, transport string) {
	t.send(ctx, Event{Type: typ, InProgress: true, Transport: transport, Timestamp: time.Now()})
}

// end reports the end of a step and returns its error.
func (t *Tester) end(ctx context.Context, typ EventType, transport string, err error, diag string) error {
	t.send(ctx, Event{Type: typ, Transport: transport, Error: err, Diagnostics: diag, Timestamp: time.Now()})
	return err
}

// send reports an event, unless there is no channel or the context ended.
func (t *Tester) send(ctx context.Context, e Event) {
	if t.cfg.EventChannel == nil {
		return
	}
	select {
	case t.cfg.EventChannel <- e:
	case <-ctx.Done():
	}
}
