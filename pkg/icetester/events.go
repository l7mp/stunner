package icetester

import (
	"time"

	"github.com/l7mp/stunner/v2/pkg/mediactl"
)

// EventType is a step of the ICE test. The steps run in this order, the ICE tests once per TURN
// transport.
type EventType int

const (
	// EventInit creates the test namespace and the test Dataplane.
	EventInit EventType = iota
	// EventInstallationComplete checks the CRDs, installs the test Gateways and the backend, and
	// finds the operator and the auth service.
	EventInstallationComplete
	// EventGatewayAvailable waits for every test Gateway to get a public address and a ready
	// dataplane pod.
	EventGatewayAvailable
	// EventICEConfigAvailable obtains the ICE server configuration of every test Gateway.
	EventICEConfigAvailable
	// EventAsymmetricICETest connects to the backend over TURN, the backend on its own address.
	EventAsymmetricICETest
	// EventSymmetricICETest connects to the backend over TURN, the backend over TURN too.
	EventSymmetricICETest
)

func (t EventType) String() string {
	switch t {
	case EventInit:
		return "Initializing"
	case EventInstallationComplete:
		return "Checking installation"
	case EventGatewayAvailable:
		return "Checking Gateway"
	case EventICEConfigAvailable:
		return "Obtaining ICE server configuration"
	case EventAsymmetricICETest:
		return "Running asymmetric ICE test"
	case EventSymmetricICETest:
		return "Running symmetric ICE test"
	default:
		return "N/A"
	}
}

// Event reports a step: its start (InProgress), then its end, with the error and a diagnostic
// hint if it failed. The events of an ICE test name the TURN transport and, on success, carry the
// result of the stream and the ICE candidates of the connection.
type Event struct {
	Type        EventType
	InProgress  bool
	Transport   string
	Error       error
	Diagnostics string
	Result      *mediactl.Result
	Local       []Candidate
	Remote      []Candidate
	Timestamp   time.Time
}

// Candidate is an ICE candidate of the test connection; Selected marks the one of the selected
// candidate pair.
type Candidate struct {
	Candidate string
	Selected  bool
}

const (
	// init
	diagK8sConfigUnavailable = "The Kubernetes configuration is unavailable. Please check whether kubectl works first."

	diagK8sClientError = "Kubernetes client is dysfunctional or the Kubernetes API server is unreachable. Does kubectl work? Is the Kubernetes context set for the right cluster?"

	diagNamespaceAlreadyExists = "The Kubernetes namespace to be used for the test already exists, most probably due to an earlier unclean exit. The tester refuses to run in such cases in order to avoid interference with existing workload. If you are sure about that the namespace is unused use '--force-cleanup' to remove it before running the test."

	diagFailedToCreateNamespace = "A namespace for running the test could not be created. This either means that the namespace already exists (e.g., if the icetester could not exit cleanly) or you specified an existing namespace (the tester refuses to run in an existing namespace in order to avoid interfering with the resources existing there), or the current Kubernetes user does not have enough rights to create a namespace. Does 'kubectl create namespace my-namespace' work?"

	// install
	diagFailedToQueryOrCreateArtifacts = "Some Kubernetes resources needed for running the tests could not be queried or created. Typically, this occurs because the Gateway API custom resources or STUNner's own custom resources have not been installed, or the current Kubernetes user does not have enough rights to get or create the resource, or some other error occurred."

	diagCDSServerUnavailable = "The STUNner gateway operator is not installed or the installation is incomplete. Is the gateway operator pod running? It is usually called 'stunner-gateway-operator-controller-manager-XXX' in the 'stunner-system', or in the namespace you installed STUNner. What is the pod status?"

	diagAuthServiceUnavailable = "The STUNner auth service is not installed or the installation is incomplete. Is the STUNner authentication server pod running? Search for the service called 'stunner-auth' in the 'stunner-system', or in the namespace you installed STUNner. What is the pod status of the pod called 'stunner-auth-XXX' in the same namespace?"

	diagICETesterBackendUnavailable = "The ICE tester backend is not inserted into the Kubernetes cluster or it is dysfunctional. This is usually a bug in the 'stunnerctl', please file an issue."

	diagCDSServerConnectionFailed = "The STUNner gateway operator is installed but it is dysfunctional. This often occurs because the Gateway API CRDs are missing or are of the wrong version and thus the operator fails to start, or the operator does not have enough RBAC permissions to access the Kubernetes resources it works on."

	diagAuthServiceConnectionFailed = "The STUNner auth service is installed but it does not hand out the ICE server configuration of the test Gateways. Check the logs of the 'stunner-auth-XXX' pod."

	diagPublicAddrNotFound = "At least one Gateway could not be exposed on a public IP/port. This is the most typical problem you will see with STUNner: it usually means that the load-balancer integration in your Kubernetes cluster does not work, or, if you are on NodePorts, none of the Kubernetes nodes have a publicly available external IP (look for ExternalIP in your node status)."

	diagDataplaneUnavailable = "The dataplane pod of a test Gateway is not running or not ready. Check the status and the events of the stunnerd pods in the test namespace."

	// test
	diagICETestFailed = "The ICE test has failed. Check the reported error!"
)
