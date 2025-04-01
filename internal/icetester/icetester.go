package icetester

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	v1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
	"github.com/l7mp/stunner/pkg/whipconn"
)

const (
	DefaultICETesterImage                    = "docker.io/l7mp/icetester:latest"
	DefaultICETesterTimeout                  = 5 * time.Minute
	DefaultICETesterPacketRate time.Duration = 0

	floodTestPacketSize = 100
	floodTestTimeout    = 20 * time.Second
)

var (
	crdCheckList = []string{
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
)

type ICETestType int

const (
	ICETestAsymmetric ICETestType = iota
	ICETestSymmetric
)

func (t ICETestType) String() string {
	switch t {
	case ICETestAsymmetric:
		return "Asymmetric"
	case ICETestSymmetric:
		return "Symmetric"
	default:
		return "N/A"
	}
}

type Config struct {
	EventChannel chan Event

	K8sConfigFlags  *cliopt.ConfigFlags
	CDSConfigFlags  *cdsclient.CDSConfigFlags
	AuthConfigFlags *cdsclient.AuthConfigFlags

	Namespace      string
	TURNTransports []v1.ListenerProtocol
	ICETesterImage string
	ForceCleanup   bool
	PacketRate     int

	Logger logger.LoggerFactory
}

type iceTester struct {
	k8sConfig *rest.Config
	*dynamic.DynamicClient

	eventCh chan Event

	k8sConfigFlags  *cliopt.ConfigFlags
	cdsConfigFlags  *cdsclient.CDSConfigFlags
	authConfigFlags *cdsclient.AuthConfigFlags

	namespace             string
	transports            []v1.ListenerProtocol
	iceTesterImage        string
	forceCleanup          bool
	floodTestSendInterval time.Duration

	logger logger.LoggerFactory
	log    logging.LeveledLogger
}

func NewICETester(config Config) (*iceTester, error) {
	logr := config.Logger
	if logr == nil {
		logr = logger.NewLoggerFactory("all:INFO")
	}

	image := DefaultICETesterImage
	if config.ICETesterImage != "" {
		image = config.ICETesterImage
	}

	var sendInterval time.Duration
	if config.PacketRate == 0 {
		sendInterval = 0
	} else {
		sendInterval = time.Duration(int64(float64(time.Second) / float64(config.PacketRate)))
	}

	tester := &iceTester{
		eventCh: config.EventChannel,

		k8sConfigFlags:  config.K8sConfigFlags,
		cdsConfigFlags:  config.CDSConfigFlags,
		authConfigFlags: config.AuthConfigFlags,

		namespace:             config.Namespace,
		transports:            config.TURNTransports,
		iceTesterImage:        image,
		forceCleanup:          config.ForceCleanup,
		floodTestSendInterval: sendInterval,

		logger: logr,
		log:    logr.NewLogger("icetester"),
	}

	return tester, nil
}

func (t *iceTester) Start(ctx context.Context) error {
	log := t.log

	///////// EventInit in-progress
	t.sendEventInit(EventInit, nil) //nolint:errcheck

	log.Infof("Creating a Kubernetes client")
	k8sConfig, err := t.k8sConfigFlags.ToRESTConfig()
	if err != nil {
		return t.sendEventComplete(EventInit,
			fmt.Errorf("Error building a Kubernetes config: %w", err),
			diagK8sConfigUnavailable,
			nil,
		)
	}
	t.k8sConfig = k8sConfig

	cs, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		return t.sendEventComplete(EventInit,
			fmt.Errorf("Error creating a Kubernetes client: %w", err),
			diagK8sClientError,
			nil,
		)
	}
	t.DynamicClient = cs

	// log.Infof("Checking basic connectivity")
	// apiGroupList := &unstructured.Unstructured{}
	// apiGroupList.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "APIGroup"})
	// if _, err := t.get(ctx, apiGroupList, metav1.GetOptions{}); err != nil {
	// 	return t.sendEventComplete(EventInit,
	// 		fmt.Errorf("Failed to query API server: %w", err),
	// 		diagK8sClientError,
	// 		nil,
	// 	)
	// }

	log.Infof("Creating a namespace for testing")
	ns := newICETesterNamespace(t.namespace)
	if _, err := t.get(ctx, ns, metav1.GetOptions{}); err == nil {
		if t.forceCleanup {
			if err := t.safelyRemove(ctx, ns, metav1.GetOptions{}, metav1.DeleteOptions{}); err != nil {
				return t.sendEventComplete(EventInit,
					fmt.Errorf("Failed to clean up tester namespace %s (--force-cleanup enforced): %w",
						ns.GetName(), err),
					diagFailedToQueryOrCreateArtifacts,
					nil,
				)
			}
		} else {
			return t.sendEventComplete(EventInit,
				fmt.Errorf("Namespace %s already exists, halting test", ns.GetName()),
				diagNamespaceAlreadyExists,
				nil,
			)
		}
	} else if !apierrors.IsNotFound(err) {
		return t.sendEventComplete(EventInit,
			fmt.Errorf("Error querying testing namespace %s: %w", ns.GetName(), err),
			diagFailedToCreateNamespace,
			nil,
		)
	}

	if err := t.create(ctx, ns, metav1.CreateOptions{}); err != nil {
		return t.sendEventComplete(EventInit,
			fmt.Errorf("Error creating a namespace for running the tests: %w", err),
			diagFailedToCreateNamespace,
			nil,
		)
	}
	defer func() {
		// do not use ctx: it might have timed out
		if err := t.delete(context.TODO(), ns, metav1.DeleteOptions{}); err != nil {
			log.Errorf("Error deleting namespace: %s", err.Error())
		}
	}()

	log.Infof("Creating custom Dataplane")
	d, err := t.makeDataplane(ctx)
	if err != nil {
		return t.sendEventComplete(EventInit,
			fmt.Errorf("Failed to create custom Dataplane: %w", err),
			diagFailedToQueryOrCreateArtifacts,
			nil,
		)
	}
	defer func() {
		// do not use ctx: it might have timed out
		if err := t.delete(context.TODO(), d, metav1.DeleteOptions{}); err != nil {
			log.Errorf("Error deleting custom Dataplane: %s", err.Error())
		}
	}()

	///////// EventInit ready
	t.sendEventComplete(EventInit, nil, "", nil) //nolint:errcheck

	///////// EventInstallationComplete in-progress
	t.sendEventInit(EventInstallationComplete, nil) //nolint:errcheck

	log.Infof("Querying CRDs")
	for _, crd := range crdCheckList {
		if _, err := t.getCRD(ctx, crd, metav1.GetOptions{}); err != nil {
			return t.sendEventComplete(EventInstallationComplete,
				fmt.Errorf("Failed to query CRD %s: %w", crd, err),
				diagFailedToQueryOrCreateArtifacts,
				nil,
			)
		}
	}

	log.Infof("Inserting tester artifacts")
	for _, obj := range newICETesterICETesterResources(t.namespace, t.iceTesterImage) {
		if err := t.safelyRemove(ctx, obj, metav1.GetOptions{}, metav1.DeleteOptions{}); err != nil {
			return t.sendEventComplete(EventInstallationComplete,
				fmt.Errorf("Failed to clean up resource %s/%s of kind %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), err),
				diagFailedToQueryOrCreateArtifacts,
				nil,
			)
		}

		if err := t.create(ctx, obj, metav1.CreateOptions{}); err != nil {
			return t.sendEventComplete(EventInstallationComplete,
				fmt.Errorf("Failed to create resource %s/%s of kind %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), err),
				diagFailedToQueryOrCreateArtifacts,
				nil,
			)
		}

		log.Debugf("Created resource %s/%s of kind %s", obj.GetNamespace(),
			obj.GetName(), obj.GetKind())
	}
	defer func() {
		for _, obj := range newICETesterICETesterResources(t.namespace, t.iceTesterImage) {
			// do not use ctx: it might have timed out
			if err := t.delete(context.TODO(), obj, metav1.DeleteOptions{}); err != nil {
				log.Errorf("Error deleting resource: %s", err.Error())
			}
		}
	}()

	log.Infof("Checking tester backend")
	iceTesterPod := newICETesterBackendPod(t.namespace, t.iceTesterImage)
	if err := eventually(ctx, t.podStatusChecker(iceTesterPod, metav1.GetOptions{}), 30*time.Second, 250*time.Millisecond); err != nil {
		return t.sendEventComplete(EventInstallationComplete,
			fmt.Errorf("ICE tester backend %s/%s not running or ready: %w",
				iceTesterPod.GetNamespace(), iceTesterPod.GetName(), err),
			diagFailedToQueryOrCreateArtifacts,
			nil,
		)
	}

	whipEndpoint, err := cdsclient.DiscoverK8sPod(ctx, t.k8sConfigFlags, t.namespace, "app=icetester", v1.DefaultICETesterPort,
		t.logger.NewLogger("auth-fwd"))
	if err != nil {
		return t.sendEventComplete(EventInstallationComplete,
			fmt.Errorf("Error searching for ICE tester backend pod: %w", err),
			diagFailedToQueryOrCreateArtifacts,
			nil,
		)
	}

	log.Info("Searching for CDS server")
	cdsPod, err := cdsclient.DiscoverK8sCDSServer(ctx, t.k8sConfigFlags, t.cdsConfigFlags,
		t.logger.NewLogger("cds-fwd"))
	if err != nil {
		return t.sendEventComplete(EventInstallationComplete,
			fmt.Errorf("Error searching for CDS server: %w", err),
			diagCDSServerUnavailable,
			nil,
		)
	}

	log.Info("Searching for authentication service")
	authPod, err := cdsclient.DiscoverK8sAuthServer(ctx, t.k8sConfigFlags, t.authConfigFlags,
		t.logger.NewLogger("auth-fwd"))
	if err != nil {
		return t.sendEventComplete(EventInstallationComplete,
			fmt.Errorf("Error searching for auth service: %w", err),
			diagAuthServiceUnavailable,
			nil,
		)
	}

	///////// EventInstallationComplete ready
	t.sendEventComplete(EventInstallationComplete, nil, "", nil) //nolint:errcheck

	///////// EventGatewayAvailable in-progress
	t.sendEventInit(EventGatewayAvailable, nil) //nolint:errcheck

	log.Info("Checking dataplane")
	for _, proto := range t.transports {
		gw := gwFromProto(proto, t.namespace)

		log.Infof("Checing public address for Gateway %s", gw.GetName())
		cds, err := cdsclient.NewConfigNamespaceNameAPI(cdsPod.Addr, t.namespace, gw.GetName(), "",
			t.logger.NewLogger("cds-client"))
		if err != nil {
			return t.sendEventComplete(EventGatewayAvailable,
				fmt.Errorf("Could not connect to CDS server for obtaning the configuration of Gateway %s: %w",
					gw.GetName(), err),
				diagCDSServerConnectionFailed,
				nil,
			)
		}

		if err := eventually(ctx, func(ctx context.Context) (bool, error) {
			confs, err := cds.Get(ctx)
			if err != nil {
				// log.Tracef("Could not get dataplane config: %w", err)
				return false, nil
			}

			if len(confs) != 1 {
				return false, errors.New("Expected exactly one dataplane config") // this should never rhappen
			}

			found := false
			for _, c := range confs[0].Clusters {
				if len(c.Endpoints) != 0 {
					found = true
					break
				}
			}
			if !found {
				// errors.New("No clusters found")
				return false, nil // operator not ready yet: retry

			}

			for _, l := range confs[0].Listeners {
				if l.PublicAddr == "" || l.PublicPort == 0 {
					return false, nil // no public address yet: retry
				}
			}

			return true, nil
		}, 60*time.Second, 250*time.Millisecond); err != nil {
			return t.sendEventComplete(EventGatewayAvailable,
				fmt.Errorf("Failed to find public address for Gateway %s/%s: %w", t.namespace, gw.GetName(), err),
				diagPublicAddrNotFound,
				nil,
			)
		}

		log.Infof("Checking dataplane pod for Gateway %s/%s", gw.GetNamespace(), gw.GetName())
		// name is unstable, choose by label
		opts := metav1.ListOptions{LabelSelector: makeSelector(map[string]string{"app": "my-app", "some-label": "some-value"})}
		if err := eventually(ctx, t.podListStatusChecker(opts), 60*time.Second, 250*time.Millisecond); err != nil {
			return t.sendEventComplete(EventGatewayAvailable,
				fmt.Errorf("Dataplane pod for Gateway %s/%s not running or ready: %w",
					iceTesterPod.GetNamespace(), iceTesterPod.GetName(), err),
				diagFailedToQueryOrCreateArtifacts,
				nil,
			)
		}
	}

	///////// EventGatewayAvailable ready
	t.sendEventComplete(EventGatewayAvailable, nil, "", nil) //nolint:errcheck

	///////// EventICEConfigAvailable in-progress
	t.sendEventInit(EventICEConfigAvailable, nil) //nolint:errcheck

	iceServers := map[v1.ListenerProtocol]webrtc.Configuration{}
	for _, proto := range t.transports {
		log.Infof("Testing ICE connection over TURN transport %s", proto.String())

		gw := gwFromProto(proto, t.namespace)

		log.Infof("Obtaining ICE server config for Gateway %s/%s", t.namespace, gw.GetName())
		if err := eventually(ctx, func(ctx context.Context) (bool, error) {
			u := url.URL{
				Scheme: "http",
				Host:   authPod.Addr,
				Path:   "/ice",
			}
			q := u.Query()
			q.Set("service", "turn")
			q.Set("namespace", t.namespace)
			q.Set("gateway", gw.GetName())
			u.RawQuery = q.Encode()

			req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
			if err != nil {
				return false, fmt.Errorf("Error preparing auth service request: %w", err)
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return false, fmt.Errorf("Failed to query auth service: %w", err)
			}

			if res.StatusCode != http.StatusOK {
				return false, nil
				// return false, fmt.Errorf("Wrong HTTP status querying auth service: %d", res.StatusCode) // this may return false for a while
			}

			var iceconf struct {
				ICEServers         []webrtc.ICEServer
				ICETransportPolicy webrtc.ICETransportPolicy
			}
			if err := json.NewDecoder(res.Body).Decode(&iceconf); err != nil { // Handle errors
				return false, fmt.Errorf("Failed to parse the ICE server config obtained from the auth service: %w", err)
			}

			iceServers[proto] = webrtc.Configuration{
				ICEServers:         iceconf.ICEServers,
				ICETransportPolicy: iceconf.ICETransportPolicy,
			}

			return true, nil
		}, 10*time.Second, 250*time.Millisecond); err != nil {
			return t.sendEventComplete(EventInstallationComplete,
				fmt.Errorf("Could not obtain ICE server config for Gateway %s/%s: %w", t.namespace, gw.GetName(), err),
				diagCDSServerConnectionFailed,
				nil,
			)
		}
	}

	///////// EventICEServerConfigAvailable ready
	t.sendEventComplete(EventICEConfigAvailable, nil, "", nil) //nolint:errcheck

	for _, iceTestType := range []ICETestType{ICETestAsymmetric, ICETestSymmetric} {
		var eventType EventType
		switch iceTestType {
		case ICETestAsymmetric:
			eventType = EventAsymmetricICETest
		case ICETestSymmetric:
			eventType = EventSymmetricICETest
		default:
		}
		for _, proto := range t.transports {
			iceConfig := iceServers[proto]

			///////// EventICETestComplete in-progress
			t.sendEventInit(eventType, map[string]any{"ICETransport": proto.String()}) //nolint:errcheck

			log.Infof("Performing %s ICE test for ICE transport %s", iceTestType.String(), proto.String())

			listenerConfig := whipconn.Config{}
			// in symmetric tests the listener uses the same ICE servers as the dialer
			if iceTestType == ICETestSymmetric {
				listenerConfig.ICEServers = t.updateICEServerAddr(iceConfig.ICEServers, proto)
				listenerConfig.ICETransportPolicy = webrtc.ICETransportPolicyRelay
			}
			log.Debugf("Setting ICE tester listener config: %#v", listenerConfig)
			if err := eventually(ctx, func(ctx context.Context) (bool, error) {
				uri := url.URL{
					Scheme: "http",
					Host:   whipEndpoint.Addr,
					Path:   "/config",
				}

				b, err := json.Marshal(listenerConfig)
				if err != nil {
					return false, fmt.Errorf("Error preparing config: %w", err) // should never happen
				}

				_, err = http.Post(uri.String(), "application/json", bytes.NewReader(b))
				if err != nil {
					return false, fmt.Errorf("Failed to POST config: %w", err) // should never happen
				}

				// query back
				req, err := http.NewRequest(http.MethodGet, uri.String(), nil)
				if err != nil {
					return false, fmt.Errorf("Error preparing GET request for querying the config: %w", err)
				}
				req.Header.Add("Content-Type", "application/json")

				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return false, fmt.Errorf("Failed to GET config: %w", err)
				}

				err = json.NewDecoder(res.Body).Decode(&listenerConfig)
				if err != nil {
					return false, fmt.Errorf("Failed to decode config: %w", err)
				}

				return true, nil
			}, 10*time.Second, 250*time.Millisecond); err != nil {
				return t.sendEventComplete(eventType,
					fmt.Errorf("Could not update config on ICE tester backend: %w", err),
					diagICETesterBackendUnavailable,
					nil,
				)
			}

			dialerConfig := listenerConfig
			dialerConfig.ICEServers = iceConfig.ICEServers
			dialerConfig.ICETransportPolicy = webrtc.ICETransportPolicyRelay
			dialerConfig.WHIPEndpoint = whipconn.WhipEndpoint
			log.Debugf("Using ICE tester dialer config: %#v", dialerConfig)

			log.Debug("Dialing")
			var clientConn net.Conn
			if err := eventually(ctx, func(ctx context.Context) (bool, error) {
				conn, err := whipconn.NewDialer(dialerConfig, t.logger).DialContext(ctx, whipEndpoint.Addr)
				if err != nil {
					return false, nil // not fatal, should be retried
				}
				clientConn = conn
				return true, nil
			}, 90*time.Second, 1000*time.Millisecond); err != nil {
				return t.sendEventComplete(EventAsymmetricICETest,
					fmt.Errorf("Could not send WHIP request to ICE tester backend: %w", err),
					diagICETestFailed,
					map[string]any{"ICETransport": proto.String()})
			}

			localSelected, remoteSelected, err := t.getSelectedICECandidates(clientConn)
			if err != nil {
				return t.sendEventComplete(eventType,
					fmt.Errorf("Failed to find selected ICE candidate pair: %w", err),
					diagICETestFailed,
					map[string]any{"ICETransport": proto.String()})
			}
			localCandidates, remoteCandidates, err := t.getICECandidates(clientConn, localSelected, remoteSelected)
			if err != nil {
				return t.sendEventComplete(eventType,
					fmt.Errorf("Failed to find ICE candidates: %w", err),
					diagICETestFailed,
					map[string]any{"ICETransport": proto.String()})
			}

			timeout, stop := context.WithTimeout(ctx, floodTestTimeout)
			defer stop() // useless
			stats, err := FloodTest(timeout, clientConn, t.floodTestSendInterval, floodTestPacketSize)
			if err != nil {
				return t.sendEventComplete(eventType,
					fmt.Errorf("Flood test failed: %w", err),
					diagICETestFailed,
					map[string]any{"ICETransport": proto.String()})
			}

			// floodtest closes the connection on normal exit, but not on error
			clientConn.Close()

			///////// EventICETestComplete ready
			//nolint:errcheck
			t.sendEventComplete(eventType, nil, "", map[string]any{
				"ICETransport":        proto.String(),
				"Stats":               stats,
				"LocalICECandidates":  localCandidates,
				"RemoteICECandidates": remoteCandidates,
			})
		}
	}

	return nil
}

func (t *iceTester) sendEventInit(typ EventType, args map[string]any) {
	t.eventCh <- Event{Type: typ, InProgress: true, Timestamp: time.Now(), Args: args}
}

func (t *iceTester) sendEventComplete(typ EventType, err error, diag string, args map[string]any) error {
	t.eventCh <- Event{Type: typ, Error: err, Timestamp: time.Now(), Diagnostics: diag, Args: args}
	return err
}

func (t *iceTester) getSelectedICECandidates(conn net.Conn) (string, string, error) {
	whipconn, ok := conn.(*whipconn.DialerConn)
	if !ok {
		return "", "", errors.New("failed to cast net.Conn to whipconn")
	}

	peerConn := whipconn.GetPeerConnection()
	transport := peerConn.SCTP().Transport().ICETransport()
	selectedPair, err := transport.GetSelectedCandidatePair()
	if err != nil {
		return "", "", err
	}

	return selectedPair.Local.String(), selectedPair.Remote.String(), nil
}

// parse from the sdps
type CandidateDesc struct {
	Candidate string
	Selected  bool
}

func (t *iceTester) getICECandidates(conn net.Conn, localSelected, remoteSelected string) ([]CandidateDesc, []CandidateDesc, error) {
	local, remote := []CandidateDesc{}, []CandidateDesc{}

	whipconn, ok := conn.(*whipconn.DialerConn)
	if !ok {
		return local, remote, errors.New("failed to cast net.Conn to whipconn")
	}

	peerConn := whipconn.GetPeerConnection()

	getcands := func(desc *webrtc.SessionDescription, selected string) []CandidateDesc {
		ret := []CandidateDesc{}
		sdp, err := desc.Unmarshal()
		if err != nil {
			return ret
		}

		for _, m := range sdp.MediaDescriptions {
			for _, attr := range m.Attributes {
				if attr.IsICECandidate() {
					ice, err := ice.UnmarshalCandidate(attr.String())
					if err == nil {
						cand := ice.String()
						if !ContainsDesc(ret, cand) { // candidates are often dumplicated for some reason
							mark := false
							if cand == selected {
								mark = true
							}
							ret = append(ret, CandidateDesc{Candidate: cand, Selected: mark})
						}
					}
				}
			}
		}

		return ret
	}

	local = getcands(peerConn.LocalDescription(), localSelected)
	remote = getcands(peerConn.RemoteDescription(), remoteSelected)

	return local, remote, nil
}

func ContainsDesc(cs []CandidateDesc, c string) bool {
	ss := make([]string, len(cs))
	for i, c := range cs {
		ss[i] = c.Candidate
	}
	return slices.Contains(ss, c)
}
