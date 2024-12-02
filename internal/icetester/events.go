package icetester

import "time"

// ICE test steps:
// 1. Check install: CDS server and API service available, GW resources and icetest-backend created
// 2. Wait until a GW public IP becomes available: emit one event for all configured TURN protocols
// 3. Run ICE test: emit one event per each configured TURN protocol
// 4. Delete K8s resources: Un-apply static manifests

type EventType int

const (
	EventInit EventType = iota
	EventInstallationComplete
	EventGatewayAvailable
	EventICEConfigAvailable
	EventAsymmetricICETest
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

type Event struct {
	Type        EventType
	Error       error
	Diagnostics string
	Args        map[string]any
	Timestamp   time.Time
	InProgress  bool
}

const (
	// init
	diagK8sConfigUnavailable = "The Kubernetes configuration is unavailable. Please check whether kubectl works first."

	diagK8sClientError = "Kubernetes client is dysfunctional or the Kubernetes API server is unreachable. Does kubectl work? Is the Kubernetes context set for the right cluster?"

	diagNamespaceAlreadyExists = "The Kubernetes namespace to be used for the test already exists, most probably due to an earlier unclean exit. The tester refuses to run in such cases in order to avoid interference with existing workload. If you are sure about that the namespace is unused use '--force-cleanup' to remove it before running the test."

	diagFailedToCreateNamespace = "A namespace for running the test could not be created. This either means that the namespace already exists (e.g., if the icetester could not exit cleanly) or you spefified an existing namespace (the tester refuses to run in an existing namespace in order to avoid interfering with the resources existing there), or the current Kubernetes user does not have enough rigts to create a namespace. Does 'kubectl create namespace my-namespace' work?"

	// install
	diagFailedToQueryOrCreateArtifacts = "Some Kubernetes resources needed for running the tests could not be queried or created. Typically, this occurs because the Gateway API custom resources or STUNNer's own custom resources have not been installed, or the current Kubernetes user does not have enough rigts to get or create the resource, or some other error occurred."

	diagCDSServerUnavailable = "The STUNner gateway operator is not installed or the installation is incomplete. Is the gateway operator pod running? It is usually called 'stunner-gateway-operator-controller-manager-XXX' in the 'stunner-system', or in the namespace you installed STUNner. What is the pod status?"

	diagAuthServiceUnavailable = "The STUNner auth service is not installed or the installation is incomplete. Is the STUNner authentication server pod running? Search for the service called 'stunner-auth' in the 'stunner-system', or in the namespace you installed STUNner. What is the pod status of the pod called 'stunner-auth-XXX' in the same namespace?"

	diagICETesterBackendUnavailable = "The ICE tester backend is not inserted into the Kubernetes cluster or it is dysfunctional. This is usually a bug in the 'stunnerctl', please file an issue."

	diagCDSServerConnectionFailed = "The STUNner gateway operator is installed but it is dysfunctional. This often occurs because the Gateway API CRDs are missing or are of the wrong version and thus the operator fails to start, or the operator does not have enough RBAC permissions to access the Kubernetes resources it works on."

	diagPublicAddrNotFound = "At least one Gateway could not be exposed on a public IP/port. This is the most typical problem you will see with STUNner: it usually means that the load-balancer integration in your Kubernetes cluster does not work, or, if you are on NodePorts, none of the Kubernetes nodes have a publicly avaalble external IP (look for ExternalIP in your node status)."

	// test
	diagICETestFailed = "The ICE test has failed. Check the reported error!"
)
