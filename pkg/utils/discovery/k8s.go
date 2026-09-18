// Package discovery finds STUNner's components in a Kubernetes cluster, the config discovery
// server of the gateway operator, the stunnerd pods of a Gateway and the auth service, and
// opens port-forwards to them. It is the toolbox behind stunnerctl and the k8s config origin
// of stunnerd.
package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// cdsProbeTimeout bounds one probe of a CDS server pod during discovery.
const cdsProbeTimeout = 2 * time.Second

// CDSConfigFlags composes a set of flags for CDS server discovery.
type CDSConfigFlags struct {
	// Addr is an explicit IP address for the CDS server.
	Addr string
	// Namespace is the namespace of the CDS server pod.
	Namespace string
	// Port is the port of the CDS server pod.
	Port int
}

// NewCDSConfigFlags returns CDS service discovery flags with default values set.
func NewCDSConfigFlags() *CDSConfigFlags {
	port := stnrv1.DefaultConfigDiscoveryPort
	if os.Getenv(stnrv1.DefaultCDSServerPortEnv) != "" {
		p, err := strconv.Atoi(os.Getenv(stnrv1.DefaultCDSServerPortEnv))
		if err != nil {
			port = p
		}
	}
	return &CDSConfigFlags{
		Addr:      os.Getenv(stnrv1.DefaultCDSServerAddrEnv),
		Port:      port,
		Namespace: os.Getenv(stnrv1.DefaultCDSServerNamespaceEnv),
	}
}

// AddFlags binds pod discovery configuration flags to a given flagset.
func (f *CDSConfigFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&f.Addr, "cds-server-address", f.Addr,
		"Config discovery service address (overriders cds-namesapce/name and disables service discovery)")
	flags.StringVar(&f.Namespace, "cds-server-namespace", f.Namespace,
		"Config discovery service namespace (disables service discovery)")
	flags.IntVar(&f.Port, "cds-server-port", f.Port, "Config discovery service port")
}

// PodConfigFlags composes a set of flags for pod discovery.
type PodConfigFlags struct {
	// Addr is an explicit IP address for the pod.
	Addr string
	// Name is the name of the pod.
	Name string
	// Port is the port to use.
	Port int
}

// NewPodConfigFlags returns Stunnerd service discovery flags with default values set.
func NewPodConfigFlags() *PodConfigFlags {
	return &PodConfigFlags{
		Port: stnrv1.DefaultHealthCheckPort,
	}
}

// AddFlags binds pod discovery configuration flags to a given flagset.
func (f *PodConfigFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&f.Addr, "pod-address", f.Addr,
		"Address of the stunnerd instance to connect to (overrides K8s pod discovery)")
	flags.StringVar(&f.Name, "pod-name", f.Name,
		"Name of the specific stunnerd pod to connect to (valid only if both -n and gateway name are specified)")
	flags.IntVar(&f.Port, "pod-port", f.Port, "Port of the stunnerd pod to connect to")
}

// AuthConfigFlags composes a set of flags for authentication service discovery.
type AuthConfigFlags struct {
	// Addr is an explicit IP address for the server.
	Addr string
	// Namespace is the namespace of the server pod.
	Namespace string
	// Port is the port of the server pod.
	Port int
	// Enforce turn credential.
	TurnAuth bool
}

// NewAuthConfigFlags returns auth service discovery flags with default values set.
func NewAuthConfigFlags() *AuthConfigFlags {
	return &AuthConfigFlags{
		Port: stnrv1.DefaultAuthServicePort,
	}
}

// AddFlags binds pod discovery configuration flags to a given flagset.
func (f *AuthConfigFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&f.Addr, "auth-server-address", f.Addr,
		"Auth service address (disables service discovery)")
	flags.StringVar(&f.Namespace, "auth-service-namespace", f.Namespace,
		"Auth service namespace (disables service discovery)")
	flags.IntVar(&f.Port, "auth-service-port", f.Port, "Auth service port")
	flags.BoolVar(&f.TurnAuth, "auth-turn-credential", f.TurnAuth, "Request TURN credentials (default: request ICE server config)")
}

// PodConnector is a helper for discovering and connecting to pods in a Kubernetes cluster.
type PodConnector struct {
	cs       *kubernetes.Clientset
	config   *rest.Config
	k8sFlags *cliopt.ConfigFlags
	log      logging.LeveledLogger
}

// PodInfo allows to return a full pod descriptor to callers.
type PodInfo struct {
	// Name of the pod.
	Name string
	// Namespace is the Kubernetes namespace of the pod.
	Namespace string
	// Addr is the Kubernetes namespace of the pod.
	Addr string
	// Proxy is a boolean telling whether the connection is proxied over a port-forwarder.
	Proxy bool
}

func (p *PodInfo) String() string {
	ret := ""
	if p.Proxy {
		ret += fmt.Sprintf("pod %s/%s at %s", p.Namespace, p.Name, p.Addr)
	} else {
		ret += p.Addr
	}
	return ret
}

// NewK8sDiscoverer returns a new Kubernetes CDS discovery client.
func NewK8sDiscoverer(k8sFlags *cliopt.ConfigFlags, log logging.LeveledLogger) (*PodConnector, error) {
	d := &PodConnector{
		k8sFlags: k8sFlags,
		log:      log,
	}

	d.log.Debug("obtaining kubeconfig")
	config, err := d.k8sFlags.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("error building Kubernetes config: %w", err)
	}
	d.config = config

	d.log.Debug("creating a Kubernetes client")
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating http client: %w", err)
	}
	d.cs = cs

	return d, nil
}

// DiscoverK8sCDSServer discovers the serving CDS server of the gateway operator in a Kubernetes
// cluster and returns an address that a CDS client can use. If necessary, opens a port-forward
// connection to a remote cluster. The operator may run multiple replicas that each carry the
// config discovery label, but only the leader serves config discovery. Policy for looking up the
// the leader pod:
//   - find the operator's Lease and parse the leader pod's name from the holder identity,
//   - when the Lease is unavailable, (no permission, a custom Lease name, or a holder that is
//     not among the pods), probe replicas and use the first one that answers.
func DiscoverK8sCDSServer(ctx context.Context, k8sFlags *cliopt.ConfigFlags, cdsFlags *CDSConfigFlags, log logging.LeveledLogger) (PodInfo, error) {
	// if CDS server address is specified, return it
	if cdsFlags.Addr != "" {
		return PodInfo{
			Addr:  net.JoinHostPort(cdsFlags.Addr, strconv.Itoa(cdsFlags.Port)),
			Proxy: false,
		}, nil
	}

	ns := ""
	nsLog := "<all>"
	if cdsFlags.Namespace != "" {
		ns = cdsFlags.Namespace
		nsLog = ns
	}

	d, err := NewK8sDiscoverer(k8sFlags, log)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to init CDS discovery client: %w", err)
	}

	label := fmt.Sprintf("%s=%s", stnrv1.DefaultCDSServiceLabelKey, stnrv1.DefaultCDSServiceLabelValue)
	d.log.Debugf("querying CDS server pods in namespace %q using label-selector %q", nsLog, label)

	pods, err := d.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: label,
	})
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to query Kubernetes API server: %w", err)
	}

	if len(pods.Items) == 0 {
		return PodInfo{}, fmt.Errorf("no CDS server found")
	}

	if len(pods.Items) == 1 {
		return d.PortFwd(ctx, &pods.Items[0], cdsFlags.Port)
	}

	// several replicas: the Lease names the leader
	for _, podNs := range podNamespaces(pods.Items) {
		lease, err := d.cs.CoordinationV1().Leases(podNs).Get(ctx, stnrv1.DefaultLeaderElectionID,
			metav1.GetOptions{})
		if err != nil {
			d.log.Debugf("cannot read the operator Lease %s/%s: %s", podNs,
				stnrv1.DefaultLeaderElectionID, err.Error())
			continue
		}
		holder := ""
		if lease.Spec.HolderIdentity != nil {
			holder = *lease.Spec.HolderIdentity
		}
		if leader := LeaderPod(holder, pods.Items); leader != nil {
			d.log.Debugf("operator Lease %s/%s names %s/%s as the leader", podNs,
				stnrv1.DefaultLeaderElectionID, leader.GetNamespace(), leader.GetName())
			return d.PortFwd(ctx, leader, cdsFlags.Port)
		}
		d.log.Debugf("operator Lease %s/%s holder %q is not among the CDS server pods",
			podNs, stnrv1.DefaultLeaderElectionID, holder)
	}

	// no usable Lease: probe the replicas, the leader is the one that answers
	d.log.Debugf("probing %d CDS server pods for the serving replica", len(pods.Items))
	for i := range pods.Items {
		probeCtx, cancel := context.WithCancel(ctx)
		p, err := d.PortFwd(probeCtx, &pods.Items[i], cdsFlags.Port)
		if err != nil {
			cancel()
			d.log.Debugf("cannot port-forward to %s/%s: %s", pods.Items[i].GetNamespace(),
				pods.Items[i].GetName(), err.Error())
			continue
		}
		if err := probeCDSServer(probeCtx, p.Addr); err != nil {
			cancel()
			d.log.Debugf("CDS server pod %s/%s does not serve: %s", p.Namespace, p.Name,
				err.Error())
			continue
		}
		// keep this port-forwarder: it stops with the caller's context
		go func() { <-ctx.Done(); cancel() }()
		return p, nil
	}

	return PodInfo{}, fmt.Errorf("none of the %d CDS server pods serves config discovery",
		len(pods.Items))
}

// LeaderPod selects the pod named by a Lease holder identity. The identity written by the
// operator's leader election is the leader pod's name followed by an underscore and a random
// suffix; pod names carry no underscore.
func LeaderPod(holder string, pods []corev1.Pod) *corev1.Pod {
	name := holder
	if i := strings.LastIndex(holder, "_"); i >= 0 {
		name = holder[:i]
	}
	if name == "" {
		return nil
	}
	for i := range pods {
		if pods[i].GetName() == name {
			return &pods[i]
		}
	}
	return nil
}

// podNamespaces lists the distinct namespaces of the pods, in order of first appearance.
func podNamespaces(pods []corev1.Pod) []string {
	seen := map[string]bool{}
	nss := []string{}
	for i := range pods {
		if ns := pods[i].GetNamespace(); !seen[ns] {
			seen[ns] = true
			nss = append(nss, ns)
		}
	}
	return nss
}

// probeCDSServer asks the CDS server at addr for its license status with a short deadline. A
// standby operator does not listen, so the request fails fast.
func probeCDSServer(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, cdsProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s/api/v1/license", addr), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %s", resp.Status)
	}
	return nil
}

// DiscoverK8sStunnerdPods discovers the stunnerd pods in a Kubernetes cluster, opens a
// port-forwarded connection to each, and returns a local address that can be used to connect to
// each pod. If gateway is empty, return all stunnerd pods in a namespace. If no namespace is given
// (using the -n CLI flag), query all stunnerd pods in the cluster.
func DiscoverK8sStunnerdPods(ctx context.Context, k8sFlags *cliopt.ConfigFlags, podFlags *PodConfigFlags, gwNs, gw string, log logging.LeveledLogger) ([]PodInfo, error) {
	var ps []PodInfo

	// direct connection
	if podFlags.Addr != "" {
		return []PodInfo{{
			Addr:  net.JoinHostPort(podFlags.Addr, strconv.Itoa(podFlags.Port)),
			Proxy: false,
		}}, nil
	}

	d, err := NewK8sDiscoverer(k8sFlags, log)
	if err != nil {
		return ps, fmt.Errorf("failed to init CDS discovery client: %w", err)
	}

	selector := labels.NewSelector()
	appLabel, err := labels.NewRequirement(stnrv1.DefaultAppLabelKey,
		selection.Equals, []string{stnrv1.DefaultAppLabelValue})
	if err != nil {
		return ps, fmt.Errorf("failed to create app label selector: %w", err)
	}
	selector = selector.Add(*appLabel)

	if gwNs != "" {
		nsLabel, err := labels.NewRequirement(stnrv1.DefaultRelatedGatewayNamespace,
			selection.Equals, []string{gwNs})
		if err != nil {
			return ps, fmt.Errorf("failed to create namespace label selector: %w", err)
		}
		selector = selector.Add(*nsLabel)

		if gw != "" {
			gwLabel, err := labels.NewRequirement(stnrv1.DefaultRelatedGatewayKey,
				selection.Equals, []string{gw})
			if err != nil {
				return ps, fmt.Errorf("failed to create namespace label selector: %w", err)
			}
			selector = selector.Add(*gwLabel)
		}
	}

	d.log.Debugf("calling GET on api/pods using namespace %q and label selector %q",
		gwNs, selector.String())
	pods, err := d.cs.CoreV1().Pods(gwNs).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return ps, fmt.Errorf("failed to query Kubernetes API server: %w", err)
	}

	// filter by pod name
	if gwNs != "" && gw != "" && podFlags.Name != "" {
		found := false
		for i, p := range pods.Items {
			if p.GetName() == podFlags.Name {
				// keep only the i-th pod
				d.log.Debugf("enforcing pod %s/%s for gateway %s/%s", *k8sFlags.Namespace, gwNs, gw)
				pods.Items = pods.Items[i : i+1]
				found = true
				break
			}
		}
		if !found {
			return ps, fmt.Errorf("pod %q not found for gateway %s/%s",
				podFlags.Name, gwNs, gw)
		}
	}

	// open port-forwarders in parallel
	var wg sync.WaitGroup
	var lock sync.Mutex
	ps = make([]PodInfo, len(pods.Items))
	wg.Add(len(pods.Items))
	for i := range pods.Items {
		go func(j int) {
			defer wg.Done()
			pod := pods.Items[j]

			p, err := d.PortFwd(ctx, &pod, podFlags.Port)
			if err != nil {
				d.log.Errorf("failed to create port-forwarder to stunnerd pod %s/%s: %s",
					pod.GetNamespace(), pod.GetName(), err.Error())
				return
			}

			lock.Lock()
			defer lock.Unlock()
			ps[j] = p
		}(i)
	}

	wg.Wait()

	d.log.Debugf("successfully opened %d port-forward connections", len(pods.Items))

	return ps, nil
}

// DiscoverK8sAuthServer discovers the cluster authentication service.
func DiscoverK8sAuthServer(ctx context.Context, k8sFlags *cliopt.ConfigFlags, authFlags *AuthConfigFlags, log logging.LeveledLogger) (PodInfo, error) {
	if authFlags.Addr != "" {
		return PodInfo{
			Addr:  fmt.Sprintf("%s:%d", authFlags.Addr, authFlags.Port),
			Proxy: false,
		}, nil
	}

	ns := ""
	nsLog := "<all>"
	if authFlags.Namespace != "" {
		ns = authFlags.Namespace
		nsLog = ns
	}

	d, err := NewK8sDiscoverer(k8sFlags, log)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to init CDS discovery client: %w", err)
	}

	label := fmt.Sprintf("%s=%s", stnrv1.DefaultAppLabelKey, stnrv1.DefaultAuthAppLabelValue)
	d.log.Debugf("querying auth service pods in namespace %q using label-selector %q", nsLog, label)

	pods, err := d.cs.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: label,
	})
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to query Kubernetes API server: %w", err)
	}

	if len(pods.Items) == 0 {
		return PodInfo{}, fmt.Errorf("no authentication found")
	}

	if len(pods.Items) > 1 {
		d.log.Infof("mulitple (%d) authentication service instances found, using the first one", len(pods.Items))
	}

	return d.PortFwd(ctx, &pods.Items[0], authFlags.Port)
}

// DiscoverK8sPod discovers an arbitrary pod.
func DiscoverK8sPod(ctx context.Context, k8sFlags *cliopt.ConfigFlags, namespace, labelSelector string, port int, log logging.LeveledLogger) (PodInfo, error) {
	d, err := NewK8sDiscoverer(k8sFlags, log)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to K8s discovery client: %w", err)
	}

	pods, err := d.cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to query Kubernetes API server: %w", err)
	}

	if len(pods.Items) == 0 {
		return PodInfo{}, errors.New("no pod found")
	}

	if len(pods.Items) > 1 {
		d.log.Infof("mulitple (%d) pods found, using the first one", len(pods.Items))
	}

	return d.PortFwd(ctx, &pods.Items[0], port)
}

func (d *PodConnector) PortFwd(ctx context.Context, pod *corev1.Pod, port int) (PodInfo, error) {
	p := PodInfo{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
		Proxy:     true,
	}
	d.log.Debugf("found pod: %s/%s", p.Namespace, p.Name)
	req := d.cs.RESTClient().
		Post().
		Prefix("api/v1").
		Resource("pods").
		Namespace(p.Namespace).
		Name(p.Name).
		SubResource("portforward")

	d.log.Debugf("creating a SPDY stream to API server using URL %q", req.URL().String())
	transport, upgrader, err := spdy.RoundTripperFor(d.config)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to get transport/upgrader from restconfig: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	d.log.Debugf("creating a port-forwarder to pod")
	remoteAddr := fmt.Sprintf("0:%d", port)
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	fw, err := portforward.New(dialer, []string{remoteAddr}, stopChan, readyChan, out, errOut)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to create port-forwarder: %w", err)
	}

	// ForwardPorts blocks for the life of the forwarder; an error before it is ready is a
	// failed setup, an error after is a lost forwarder, which the next request will notice
	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	d.log.Debug("waiting for port-forwarder...")
	select {
	case <-readyChan:
	case err := <-errCh:
		return PodInfo{}, fmt.Errorf("failed to set up port-forwarder to pod %s/%s: %w",
			p.Namespace, p.Name, err)
	case <-ctx.Done():
		close(stopChan)
		return PodInfo{}, ctx.Err()
	}

	localPort, err := fw.GetPorts()
	if err != nil {
		close(stopChan)
		return PodInfo{}, fmt.Errorf("error obtaining local forwarder port: %w", err)
	}

	if len(localPort) != 1 {
		close(stopChan)
		return PodInfo{}, fmt.Errorf("error setting up port-forwarder: required port pairs (1) "+
			"does not match the length of port forwarder port pairs (%d)", len(localPort))
	}

	go func() {
		select {
		case <-ctx.Done():
			close(stopChan)
		case err := <-errCh:
			if err != nil {
				d.log.Errorf("port-forwarder to pod %s/%s lost: %s", p.Namespace, p.Name, err.Error())
			}
		}
	}()

	p.Addr = fmt.Sprintf("127.0.0.1:%d", localPort[0].Local)
	d.log.Debugf("port-forwarder connected to %s", p.String())
	return p, nil
}
