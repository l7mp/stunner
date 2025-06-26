package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"

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

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

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

	d.log.Debug("Obtaining kubeconfig")
	config, err := d.k8sFlags.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("error building Kubernetes config: %w", err)
	}
	d.config = config

	d.log.Debug("Creating a Kubernetes client")
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating http client: %w", err)
	}
	d.cs = cs

	return d, nil
}

// DiscoverK8sCDSServer discovers a CDS Server located in a Kubernetes cluster and returns an
// address that a CDS client can be opened to for reaching that CDS server. If necessary, opens a
// port-forward connection to the remote cluster.
func DiscoverK8sCDSServer(ctx context.Context, k8sFlags *cliopt.ConfigFlags, cdsFlags *CDSConfigFlags, log logging.LeveledLogger) (PodInfo, error) {
	// if CDS server address is specified, return it
	if cdsFlags.Addr != "" {
		return PodInfo{
			Addr:  fmt.Sprintf("%s:%d", cdsFlags.Addr, cdsFlags.Port),
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
	d.log.Debugf("Querying CDS server pods in namespace %q using label-selector %q", nsLog, label)

	pods, err := d.cs.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: label,
	})
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to query Kubernetes API server: %w", err)
	}

	if len(pods.Items) == 0 {
		return PodInfo{}, fmt.Errorf("no CDS server found")
	}

	if len(pods.Items) > 1 {
		return PodInfo{}, fmt.Errorf("too many CDS servers")
	}

	return d.PortFwd(ctx, &pods.Items[0], cdsFlags.Port)
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
			Addr:  fmt.Sprintf("%s:%d", podFlags.Addr, podFlags.Port),
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

	d.log.Debugf("Calling GET on api/pods using namespace %q and label selector %q",
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
				d.log.Debugf("Enforcing pod %s/%s for gateway %s/%s", *k8sFlags.Namespace, gwNs, gw)
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
				d.log.Errorf("Failed to create port-forwarder to stunnerd pod %s/%s: %s",
					pod.GetNamespace(), pod.GetName(), err.Error())
				return
			}

			lock.Lock()
			defer lock.Unlock()
			ps[j] = p
		}(i)
	}

	wg.Wait()

	d.log.Debugf("Successfully opened %d port-forward connections", len(pods.Items))

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
	d.log.Debugf("Querying auth service pods in namespace %q using label-selector %q", nsLog, label)

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
		d.log.Infof("Mulitple (%d) authentication service instances found, using the first one", len(pods.Items))
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
		d.log.Infof("Mulitple (%d) pods found, using the first one", len(pods.Items))
	}

	return d.PortFwd(ctx, &pods.Items[0], port)
}

func (d *PodConnector) PortFwd(ctx context.Context, pod *corev1.Pod, port int) (PodInfo, error) {
	p := PodInfo{
		Name:      pod.GetName(),
		Namespace: pod.GetNamespace(),
		Proxy:     true,
	}
	d.log.Debugf("Found pod: %s/%s", p.Namespace, p.Name)
	req := d.cs.RESTClient().
		Post().
		Prefix("api/v1").
		Resource("pods").
		Namespace(p.Namespace).
		Name(p.Name).
		SubResource("portforward")

	d.log.Debugf("Creating a SPDY stream to API server using URL %q", req.URL().String())
	transport, upgrader, err := spdy.RoundTripperFor(d.config)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to get transport/upgrader from restconfig: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	d.log.Debugf("Creating a port-forwarder to pod")
	remoteAddr := fmt.Sprintf("0:%d", port)
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	fw, err := portforward.New(dialer, []string{remoteAddr}, stopChan, readyChan, out, errOut)
	if err != nil {
		return PodInfo{}, fmt.Errorf("failed to create port-forwarder: %w", err)
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			d.log.Errorf("failed to set up port-forwarder: %s", err.Error())
			os.Exit(1)
		}
	}()

	d.log.Debug("Waiting for port-forwarder...")
	<-readyChan

	localPort, err := fw.GetPorts()
	if err != nil {
		return PodInfo{}, fmt.Errorf("error obtaining local forwarder port: %w", err)
	}

	if len(localPort) != 1 {
		return PodInfo{}, fmt.Errorf("error setting up port-forwarder: required port pairs (1) "+
			"does not match the length of port forwarder port pairs (%d)", len(localPort))
	}

	go func() {
		<-ctx.Done()
		close(stopChan)
	}()

	p.Addr = fmt.Sprintf("127.0.0.1:%d", localPort[0].Local)
	d.log.Debugf("Port-forwarder connected to %s", p.String())
	return p, nil
}
