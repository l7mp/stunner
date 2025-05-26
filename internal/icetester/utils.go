package icetester

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/pion/webrtc/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	v1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var turnURIAddrRegexp = regexp.MustCompile(`:(\d+.\d+.\d+.\d+):`)

func getGVR(config *rest.Config, obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	gvk := gv.WithKind(obj.GetKind())
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	return mapping.Resource, nil
}

func gwFromProto(proto v1.ListenerProtocol, ns string) *unstructured.Unstructured {
	switch proto {
	case v1.ListenerProtocolTURNUDP, v1.ListenerProtocolUDP:
		return newICETesterUDPGateway(ns)
	case v1.ListenerProtocolTCP, v1.ListenerProtocolTURNTCP:
		return newICETesterTCPGateway(ns)
	default:
		return nil
	}
}

// updateICEServerAddr modifies the cluster-side ICE server config for symmetric ICE tests so that
// the backend will be configured with the gateway as a TURN server
func (t *iceTester) updateICEServerAddr(ss []webrtc.ICEServer, proto v1.ListenerProtocol) []webrtc.ICEServer {
	// use service as the TURN server address
	gw := gwFromProto(proto, t.namespace)
	ret := []webrtc.ICEServer{}
	for _, s := range ss {
		urls := []string{}
		for _, u := range s.URLs {
			urls = append(urls, turnURIAddrRegexp.ReplaceAllString(u,
				fmt.Sprintf(":%s.%s.svc.cluster.local:", gw.GetName(), gw.GetNamespace())))
		}
		ret = append(ret, webrtc.ICEServer{
			URLs:       urls,
			Username:   s.Username,
			Credential: s.Credential,
		})
	}
	return ret
}

func makeSelector(matcher map[string]string) string {
	labelSelector := metav1.LabelSelector{MatchLabels: matcher}
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		panic(err)
	}
	return selector.String()
}

// checkers
type checkerFunc func(ctx context.Context) (bool, error)

func eventually(ctx context.Context, condition checkerFunc, timeout, interval time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ok, err := condition(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		select {
		case <-timeoutCtx.Done():
			return timeoutCtx.Err()
		case <-ticker.C:
			continue
		}
	}
}

func (t *iceTester) podStatusChecker(obj *unstructured.Unstructured, opts metav1.GetOptions) checkerFunc {
	return func(ctx context.Context) (bool, error) {
		pod, err := t.get(ctx, obj, opts)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			}
			return false, nil //retry
		}

		phase, found, err := unstructured.NestedString(pod.Object, "status", "phase")
		if err != nil || !found {
			return false, nil // retry
		}

		if phase != "Running" {
			return false, nil // retry
		}

		conditions, found, err := unstructured.NestedSlice(pod.Object, "status", "conditions")
		if err != nil || !found {
			return false, nil // retry
		}

		ready := false
		for _, c := range conditions {
			condition, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if condition["type"] == "Ready" && condition["status"] == "True" {
				ready = true
				break
			}
		}

		return ready, nil
	}
}

// check status of all pods in the podlist, best used for querying pods by a label-selector
func (t *iceTester) podListStatusChecker(opts metav1.ListOptions) checkerFunc {
	return func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "/v1",
				"kind":       "Pod",
			},
		}

		pods, err := t.list(ctx, obj, opts)
		if err != nil {
			return false, err
		}

		for _, p := range pods {
			chk := t.podStatusChecker(p, metav1.GetOptions{})
			ok, err := chk(ctx)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}

		return true, nil
	}
}
