package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/pion/logging"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var DefaultCDSServerPort = 13478

func init() {
	as := strings.Split(stnrv1.DefaultConfigDiscoveryAddress, ":")
	if len(as) == 2 {
		if port, err := strconv.Atoi(as[1]); err != nil {
			DefaultCDSServerPort = port
		}
	}
}

// CDSConfigFlags composes a set of flags for CDS server discovery
type CDSConfigFlags struct {
	// ServerAddr is an explicit IP address for the CDS server.
	ServerAddr string
	// ServerNamespace is the namespace of the CDS server pod.
	ServerNamespace string
	// ServerPort is the port of the CDS server pod.
	ServerPort int
}

// NewCDSConfigFlags returns CDS service discovery flags with default values set.
func NewCDSConfigFlags() *CDSConfigFlags {
	port := DefaultCDSServerPort
	if os.Getenv(stnrv1.DefaultCDSServerPortEnv) != "" {
		p, err := strconv.Atoi(os.Getenv(stnrv1.DefaultCDSServerPortEnv))
		if err != nil {
			port = p
		}
	}
	return &CDSConfigFlags{
		ServerAddr:      os.Getenv(stnrv1.DefaultCDSServerAddrEnv),
		ServerPort:      port,
		ServerNamespace: os.Getenv(stnrv1.DefaultCDSServerNamespaceEnv),
	}
}

// AddFlags binds CDS server discovery configuration flags to a given flagset.
func (f *CDSConfigFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&f.ServerAddr, "cds-server-address", f.ServerAddr,
		"Config discovery service address (overriders cds-namesapce/name and disables CDS service discovery)")
	flags.StringVar(&f.ServerNamespace, "cds-server-namespace", f.ServerNamespace,
		"Config discovery service namespace (disables CDS service discovery)")
	flags.IntVar(&f.ServerPort, "cds-server-port", f.ServerPort,
		"Config discovery service port")
}

// DiscoverK8sCDSServer discovers a CDS Server located in a Kubernetes cluster and returns an
// address that a CDS client can be opened to for reaching that CDS server. If necessary, opens a
// port-forward connection to the remote cluster.
func DiscoverK8sCDSServer(ctx context.Context, k8sFlags *cliopt.ConfigFlags, cdsFlags *CDSConfigFlags, log logging.LeveledLogger) (string, error) {
	// if CDS server address is specified, return it
	if cdsFlags.ServerAddr != "" {
		return cdsFlags.ServerAddr, nil
	}

	ns := ""
	nsLog := "<all>"
	if cdsFlags.ServerNamespace != "" {
		ns = cdsFlags.ServerNamespace
		nsLog = ns
	}

	log.Debug("Obtaining kubeconfig")
	config, err := k8sFlags.ToRESTConfig()
	if err != nil {
		return "", fmt.Errorf("error building Kubernetes config: %w", err)
	}

	log.Debug("Creating a Kubernetes client")
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("error creating http client: %w", err)
	}

	label := fmt.Sprintf("%s=%s", stnrv1.DefaultCDSServiceLabelKey, stnrv1.DefaultCDSServiceLabelValue)
	log.Debugf("Querying CDS server pods in namespace %q using label-selector %q", nsLog, label)
	pods, err := cs.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: label,
	})

	if err != nil {
		return "", err
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no CDS server found")
	}

	if len(pods.Items) > 1 {
		return "", fmt.Errorf("too many CDS servers")
	}

	name := pods.Items[0].GetName()
	namespace := pods.Items[0].GetNamespace()
	log.Debugf("Found CDS server: %s/%s", namespace, name)
	req := cs.RESTClient().
		Post().
		Prefix("api/v1").
		Resource("pods").
		Namespace(namespace).
		Name(name).
		SubResource("portforward")

	log.Debugf("Creating a SPDY stream to API server using URL %q", req.URL().String())
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return "", fmt.Errorf("error getting transport/upgrader from restconfig: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	log.Debug("Creating a port-forwarder to CDS server")
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", cdsFlags.ServerPort)}, stopChan, readyChan, out, errOut)
	if err != nil {
		return "", fmt.Errorf("error creating port-forwarder: %w", err)
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			log.Errorf("error setting up port-forwarder: %s", err.Error())
			os.Exit(1)
		}
	}()

	log.Debug("Waiting for port-forwarder...")
	<-readyChan

	localPort, err := fw.GetPorts()
	if err != nil {
		return "", fmt.Errorf("error obtaining local forwarder port: %w", err)
	}

	if len(localPort) != 1 {
		return "", fmt.Errorf("error setting up port-forwarder: required port pairs (1) "+
			"does not match the length of port forwarder port pairs (%d)", len(localPort))
	}

	go func() {
		<-ctx.Done()
		close(stopChan)
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", localPort[0].Local)
	log.Debugf("CDS server reachable on address %q", addr)
	return addr, nil
}
