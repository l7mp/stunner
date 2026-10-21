package icetester

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

const (
	icetesterDataplaneName = "icetester-dataplane"
	// listenerPort is the port of the TURN listener of every test Gateway
	listenerPort = 3478
)

func newICETesterNamespace(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]any{
				"name": name,
			},
		},
	}
}

func newICETesterGatewayClass(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "GatewayClass",
			"metadata": map[string]any{
				"name": "icetest-gatewayclass",
			},
			"spec": map[string]any{
				"controllerName": "stunner.l7mp.io/gateway-operator",
				"parametersRef": map[string]any{
					"group":     "stunner.l7mp.io",
					"kind":      "GatewayConfig",
					"name":      "icetest-gatewayconfig",
					"namespace": namespace,
				},
				"description": "STUNner is a WebRTC ingress gateway for Kubernetes",
			},
		},
	}
}

func newICETesterGatewayConfig(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "stunner.l7mp.io/v1",
			"kind":       "GatewayConfig",
			"metadata": map[string]any{
				"name":      "icetest-gatewayconfig",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"realm":        "icetest.l7mp.io",
				"authType":     "ephemeral",
				"sharedSecret": "icetest-secret",
				"dataplane":    icetesterDataplaneName,
				"logLevel":     "all:INFO",
			},
		},
	}
}

func newICETesterUDPGateway(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				"name":      "icetest-udp-gateway",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"gatewayClassName": "icetest-gatewayclass",
				"listeners": []any{
					map[string]any{
						"name":     "icetest-udp-listener",
						"port":     int64(listenerPort),
						"protocol": "TURN-UDP",
					},
				},
			},
		},
	}
}

func newICETesterTCPGateway(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				"name":      "icetest-tcp-gateway",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"gatewayClassName": "icetest-gatewayclass",
				"listeners": []any{
					map[string]any{
						"name":     "icetest-tcp-listener",
						"port":     int64(listenerPort),
						"protocol": "TURN-TCP",
					},
				},
			},
		},
	}
}

func newICETesterUDPRoute(namespace string, protos []stnrv2.Protocol) *unstructured.Unstructured {
	r := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "stunner.l7mp.io/v1",
			"kind":       "UDPRoute",
			"metadata": map[string]any{
				"name":      "icetest-route",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"parentRefs": []any{},
				"rules": []any{
					map[string]any{
						"backendRefs": []any{
							map[string]any{
								"name": "icetest-backend",
							},
						},
					},
				},
			},
		},
	}

	if slices.Contains(protos, stnrv2.ProtocolTURNUDP) {
		r = addGWtoUDPRoute("icetest-udp-gateway", r)

	}
	if slices.Contains(protos, stnrv2.ProtocolTURNTCP) {
		r = addGWtoUDPRoute("icetest-tcp-gateway", r)
	}

	return r
}

func addGWtoUDPRoute(gw string, u *unstructured.Unstructured) *unstructured.Unstructured {
	if u == nil {
		return u
	}

	parentRefs, _, _ := unstructured.NestedSlice(u.Object, "spec", "parentRefs")
	parentRefs = append(parentRefs, map[string]any{"name": gw})
	unstructured.SetNestedSlice(u.Object, parentRefs, "spec", "parentRefs") // nolint:errcheck

	rules, _, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	rule := rules[0].(map[string]any)
	backendRefs, _, _ := unstructured.NestedSlice(rule, "backendRefs")
	backendRefs = append(backendRefs, map[string]any{"name": gw})
	unstructured.SetNestedSlice(rule, backendRefs, "backendRefs") // nolint:errcheck
	unstructured.SetNestedSlice(u.Object, rules, "spec", "rules") // nolint:errcheck

	return u
}

func newICETesterBackendPod(namespace, iceTesterImage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "icetest-backend",
				"namespace": namespace,
				"labels": map[string]any{
					"app": "icetester",
				},
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":    "icetester",
						"image":   iceTesterImage,
						"command": []any{"icetester"},
						"args":    []any{"-l", "all:DEBUG"},
					},
				},
			},
		},
	}
}

func newICETesterBackendService(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]any{
				"name":      "icetest-backend",
				"namespace": namespace,
				"labels": map[string]any{
					"app": "icetester",
				},
			},
			"spec": map[string]any{
				"ports": []any{
					map[string]any{
						"name":     "whip-port",
						"port":     int64(stnrv2.DefaultICETesterPort),
						"protocol": "TCP",
					},
				},
				"selector": map[string]any{
					"app": "icetester",
				},
			},
		},
	}
}

func newICETesterICETesterResources(ns, iceTesterImage string, protos []stnrv2.Protocol) []*unstructured.Unstructured {
	l := []*unstructured.Unstructured{
		newICETesterGatewayClass(ns),
		newICETesterGatewayConfig(ns),
	}
	if slices.Contains(protos, stnrv2.ProtocolTURNUDP) {
		l = append(l, newICETesterUDPGateway(ns))
	}
	if slices.Contains(protos, stnrv2.ProtocolTURNTCP) {
		l = append(l, newICETesterTCPGateway(ns))
	}

	l = append(l, []*unstructured.Unstructured{
		newICETesterUDPRoute(ns, protos),
		newICETesterBackendPod(ns, iceTesterImage),
		newICETesterBackendService(ns),
	}...)

	return l
}

// makeDataplane creates the test Dataplane, a copy of the default one with the resources, the
// grace period and the offload engine of the test.
func (t *Tester) makeDataplane(ctx context.Context) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "stunner.l7mp.io/v1",
		"kind":       "Dataplane",
		"metadata":   map[string]any{"name": "default"},
	}}
	d, err := t.get(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to query default Dataplane: %w", err)
	}

	d.SetName(icetesterDataplaneName)
	d.SetResourceVersion("")
	d.SetUID("")
	for _, s := range []struct {
		path  []string
		value any
	}{
		{path: []string{"spec", "terminationGracePeriodSeconds"}, value: int64(0)},
		{path: []string{"spec", "resources", "requests", "cpu"}, value: "250m"},
		{path: []string{"spec", "resources", "requests", "memory"}, value: "256Mi"},
		{path: []string{"spec", "resources", "limits", "cpu"}, value: "250m"},
		{path: []string{"spec", "resources", "limits", "memory"}, value: "256Mi"},
		{path: []string{"spec", "offloadEngine"}, value: t.cfg.OffloadEngine.String()},
	} {
		if err := unstructured.SetNestedField(d.Object, s.value, s.path...); err != nil {
			return nil, fmt.Errorf("failed to set field <%s> to %s: %w",
				strings.Join(s.path, "."), s.value, err)
		}
	}
	if t.cfg.OffloadEngine != stnrv2.OffloadEngineNone {
		if err := unstructured.SetNestedSlice(d.Object, []any{"NET_ADMIN", "SYS_ADMIN", "SYS_MODULE"},
			"spec", "containerSecurityContext", "capabilities", "add"); err != nil {
			return nil, fmt.Errorf("failed to enable NET_ADMIN capabilities: %w", err)
		}
	}

	// the test Dataplane of an earlier run is replaced
	cur, err := t.get(ctx, d)
	switch {
	case apierrors.IsNotFound(err):
		err = t.create(ctx, d)
	case err == nil:
		d.SetResourceVersion(cur.GetResourceVersion())
		var r dynamic.ResourceInterface
		if r, err = t.resource(d); err == nil {
			_, err = r.Update(ctx, d, metav1.UpdateOptions{})
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create or update the test Dataplane: %w", err)
	}
	return d, nil
}

// resource returns the dynamic client of the kind of an object, in its namespace if any.
func (t *Tester) resource(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := t.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	if obj.GetNamespace() != "" {
		return t.dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace()), nil
	}
	return t.dyn.Resource(mapping.Resource), nil
}

func (t *Tester) create(ctx context.Context, obj *unstructured.Unstructured) error {
	r, err := t.resource(obj)
	if err != nil {
		return err
	}
	_, err = r.Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func (t *Tester) get(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	r, err := t.resource(obj)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, obj.GetName(), metav1.GetOptions{})
}

// delete deletes an object, an absent one included.
func (t *Tester) delete(ctx context.Context, obj *unstructured.Unstructured) error {
	r, err := t.resource(obj)
	if err != nil {
		return err
	}
	if err := r.Delete(ctx, obj.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// remove deletes an object and waits until it is gone.
func (t *Tester) remove(ctx context.Context, obj *unstructured.Unstructured) error {
	if err := t.delete(ctx, obj); err != nil {
		return err
	}
	return eventually(ctx, 30*time.Second, func(ctx context.Context) (bool, error) {
		_, err := t.get(ctx, obj)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}
