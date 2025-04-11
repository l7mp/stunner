package icetester

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/l7mp/stunner/pkg/apis/v1"
)

const (
	icetesterDataplaneName = "icetester-dataplane"
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
						"port":     int64(3478),
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
						"port":     int64(3478),
						"protocol": "TURN-TCP",
					},
				},
			},
		},
	}
}

func newICETesterUDPRoute(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "stunner.l7mp.io/v1",
			"kind":       "UDPRoute",
			"metadata": map[string]any{
				"name":      "icetest-route",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"parentRefs": []any{
					map[string]any{
						"name": "icetest-udp-gateway",
					},
					map[string]any{
						"name": "icetest-tcp-gateway",
					},
				},
				"rules": []any{
					map[string]any{
						"backendRefs": []any{
							map[string]any{
								"name": "icetest-backend",
							},
							map[string]any{
								"name": "icetest-udp-gateway",
							},
							map[string]any{
								"name": "icetest-tcp-gateway",
							},
						},
					},
				},
			},
		},
	}
}

func newICETesterBackendPod(namespace, iceTesterImage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "/v1",
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
						"port":     int64(v1.DefaultICETesterPort),
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

func newICETesterICETesterResources(ns, iceTesterImage string) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		newICETesterGatewayClass(ns),
		newICETesterGatewayConfig(ns),
		newICETesterUDPGateway(ns),
		newICETesterTCPGateway(ns),
		newICETesterUDPRoute(ns),
		newICETesterBackendPod(ns, iceTesterImage),
		newICETesterBackendService(ns),
	}
}

func (t *iceTester) makeDataplane(ctx context.Context) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "stunner.l7mp.io/v1",
			"kind":       "Dataplane",
			"metadata": map[string]any{
				"name": "default",
			},
		},
	}
	d, err := t.get(ctx, obj, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("Failed to query default Dataplane: %w", err)
	}

	// customize
	d.SetName(icetesterDataplaneName)
	d.SetResourceVersion("")
	for _, s := range []struct {
		path  []string
		value any
	}{
		{path: []string{"spec", "terminationGracePeriodSeconds"}, value: int64(0)},
		{path: []string{"spec", "resources", "requests", "cpu"}, value: "250m"},
		{path: []string{"spec", "resources", "requests", "memory"}, value: "256Mi"},
		{path: []string{"spec", "resources", "limits", "cpu"}, value: "250m"},
		{path: []string{"spec", "resources", "limits", "memory"}, value: "256Mi"},
		{path: []string{"spec", "offloadEngine"}, value: t.offloadEngine.String()},
	} {
		if err := unstructured.SetNestedField(d.Object, s.value, s.path...); err != nil {
			return nil, fmt.Errorf("Failed to set field <%s> to %s: %w",
				strings.Join(s.path, "."), s.value, err)
		}
	}

	// offload customizations
	if t.offloadEngine != v1.OffloadEngineNone {
		if err := unstructured.SetNestedSlice(d.Object, []any{"NET_ADMIN", "SYS_ADMIN", "SYS_MODULE"},
			"spec", "containerSecurityContext", "capabilities", "add"); err != nil {
			return nil, fmt.Errorf("Failed to enable NET_ADMIN capabilities: %w", err)
		}
	}

	if err := t.createOrUpdate(ctx, d, metav1.GetOptions{}, metav1.CreateOptions{}, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("Failed to create/update tester Dataplane: %w", err)
	}

	return d, nil
}

// k8s client funcs
func (t *iceTester) create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) error {
	gvr, err := getGVR(t.k8sConfig, obj)
	if err != nil {
		return err
	}

	if obj.GetNamespace() != "" {
		// For namespaced resources
		_, err = t.Resource(gvr).Namespace(obj.GetNamespace()).Create(ctx, obj, opts)
	} else {
		// For cluster-scoped resources
		_, err = t.Resource(gvr).Create(ctx, obj, opts)
	}

	return err
}

func (t *iceTester) get(ctx context.Context, obj *unstructured.Unstructured, opts metav1.GetOptions) (*unstructured.Unstructured, error) {
	gvr, err := getGVR(t.k8sConfig, obj)
	if err != nil {
		return nil, err
	}

	var getObj *unstructured.Unstructured
	if obj.GetNamespace() != "" {
		// For namespaced resources
		getObj, err = t.Resource(gvr).Namespace(obj.GetNamespace()).Get(ctx, obj.GetName(), opts)
	} else {
		// For cluster-scoped resources
		getObj, err = t.Resource(gvr).Get(ctx, obj.GetName(), opts)
	}

	return getObj, err
}

func (t *iceTester) getCRD(ctx context.Context, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error) {
	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	return t.Resource(gvr).Get(ctx, name, opts)
}

func (t *iceTester) list(ctx context.Context, obj *unstructured.Unstructured, opts metav1.ListOptions) ([]*unstructured.Unstructured, error) {
	gvr, err := getGVR(t.k8sConfig, obj)
	if err != nil {
		return nil, err
	}

	var listObj *unstructured.UnstructuredList
	if obj.GetNamespace() != "" {
		// For namespaced resources
		listObj, err = t.Resource(gvr).Namespace(obj.GetNamespace()).List(ctx, opts)
	} else {
		// For cluster-scoped resources
		listObj, err = t.Resource(gvr).List(ctx, opts)
	}
	if err != nil {
		return nil, err
	}

	var list = make([]*unstructured.Unstructured, len(listObj.Items))
	for i, o := range listObj.Items {
		list[i] = &o
	}

	return list, err
}

func (t *iceTester) delete(ctx context.Context, obj *unstructured.Unstructured, opts metav1.DeleteOptions) error {
	gvr, err := getGVR(t.k8sConfig, obj)
	if err != nil {
		return err
	}

	if obj.GetNamespace() != "" {
		// For namespaced resources
		return t.Resource(gvr).Namespace(obj.GetNamespace()).Delete(ctx, obj.GetName(), opts)
	} else {
		// For cluster-scoped resources
		return t.Resource(gvr).
			Delete(ctx, obj.GetName(), opts)
	}
}

func (t *iceTester) update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	gvr, err := getGVR(t.k8sConfig, obj)
	if err != nil {
		return nil, err
	}

	current, err := t.get(ctx, obj, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Copy the ResourceVersion to ensure proper update
	obj.SetResourceVersion(current.GetResourceVersion())

	// Perform the update
	if obj.GetNamespace() != "" {
		return t.Resource(gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, opts)
	}

	return t.Resource(gvr).Update(ctx, obj, opts)
}

func (t *iceTester) createOrUpdate(ctx context.Context, obj *unstructured.Unstructured, gopts metav1.GetOptions, copts metav1.CreateOptions, uopts metav1.UpdateOptions) error {
	if _, err := t.get(ctx, obj, gopts); err != nil {
		if apierrors.IsNotFound(err) {
			return t.create(ctx, obj, copts)
		} else {
			_, err := t.update(ctx, obj, uopts)
			return err
		}
	} else {
		return err
	}
}

func (t *iceTester) safelyRemove(ctx context.Context, obj *unstructured.Unstructured, gopts metav1.GetOptions, dopts metav1.DeleteOptions) error {
	_ = t.delete(ctx, obj, dopts) // nolint:errcheck
	return eventually(ctx, func(ctx context.Context) (bool, error) {
		if _, err := t.get(ctx, obj, gopts); err != nil && apierrors.IsNotFound(err) {
			return true, nil
		} else {
			return false, err
		}
	}, 30*time.Second, 250*time.Millisecond)
}
