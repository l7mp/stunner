# Gateway API reference

The [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) exposes the
control plane configuration using the standard [Kubernetes Gateway
API](https://gateway-api.sigs.k8s.io). This allows to configure STUNner in the familiar
YAML-engineering style via Kubernetes manifests. The below reference gives a quick overview of the
Gateway API. Note that STUNner implements only a subset of the full [spec](/doc/GATEWAY.md), see
[here](https://github.com/l7mp/stunner-gateway-operator#caveats) for a list of the most important
simplifications.

## Overview

The main unit of the control plane configuration is the *gateway hierarchy*. Here, a Gateway
hierarchy is a collection of [Kubernetes Custom
Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources)
that together describe the way media traffic should enter the cluster via STUNner, including public
IP addresses and ports clients can use to reach STUNner, TURN credentials, routing rules, etc. The
anchor of the gateway hierarchy is the GatewayClass object, and the rest of the resources form a
complete hierarchy underneath it.

![Gateway hierarchy](/doc/gateway_api.svg)

In general, the scope of a gateway hierarchy is a single namespace, but this is not strictly
enforced: e.g., the GatewayClass is
[cluster-scoped](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions)
so it is outside the namespace, GatewayClasses can refer to GatewayConfigs across namespaces,
etc. Still, it is a good practice to keep all control plane configuration, plus the actual
dataplane pods, in a single namespace as much as possible.

## GatewayClass

The GatewayClass resource provides the root of the gateway hierarchy. GatewayClass resources are
cluster-scoped, so they can be attached to from any namespace, and we usually assume that each
namespaced gateway hierarchy will have a separate global GatewayClass as the anchor.

Below is a sample GatewayClass resource. Each GatewayClass must specify a controller that will
manage the Gateway objects created under the hierarchy; this must be set to
`stunner.l7mp.io/gateway-operator` for the STUNner gateway operator to pick up the GatewayClass. In
addition, a GatewayClass can refer to further implementation-specific configuration via a
`parametersRef`; in our case, this will be a GatewayConfig object (see [below](#gatewayconfig)).

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GatewayClass
metadata:
  name: stunner-gatewayclass
spec:
  controllerName: "stunner.l7mp.io/gateway-operator"
  parametersRef:
    group: "stunner.l7mp.io"
    kind: GatewayConfig
    name: stunner-gatewayconfig
    namespace: stunner
  description: "STUNner is a WebRTC ingress gateway for Kubernetes"
```

Below is a quick reference of the most important fields of the GatewayClass
[`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects)

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `controllerName` | `string` | Reference to the controller that is managing the Gateways of this class. The value of this field MUST be specified as `stunner.l7mp.io/gateway-operator`. | Yes |
| `parametersRef` | `object` | Reference to a GatewayConfig resource, identified by the `name` and `namespace`, which describes general STUNner configuration. The settings `group: "stunner.l7mp.io"` and `kind: GatewayConfig` are default and can be omitted.  Specifying any other group or kind is an error. | Yes |
| `description` | `string` | Description helps describe a GatewayClass with more details. | No |

## GatewayConfig

The GatewayConfig resource provides general configuration for STUNner, most importantly the
STUN/TURN authentication [credentials](/doc/AUTH.md) clients can use to connect to
STUNner. GatewayClass resources attach a STUNner configuration to the hierarchy by specifying a
particular GatewayConfig in the `parametersRef`.  GatewayConfig resources are namespaced, and every
hierarchy can contain at most one GatewayConfig. Failing to specify a GatewayConfig is an error
because the authentication credentials cannot be learned by the dataplane otherwise. The STUNner
gateway operator will refuse to generate a dataplane running config until the user attaches a valid
GatewayConfig to the hierarchy.

The following example sets the [`plaintext` authentication](/doc/AUTH.md) mechanism for STUNner
using the username/password pair `user-1/pass-1`, and the authentication realm `stunner.l7mp.io`.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  realm: stunner.l7mp.io
  authType: plaintext
  userName: "user-1"
  password: "pass-1"
  metricsEndpoint: "http://0.0.0.0:8080/metrics"
```

Below is a quick reference of the most important fields of the GatewayConfig
[`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects)

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `stunnerConfig` | `string` | The name of the ConfigMap into which the operator renders the `stunnerd` running configuration. Default: `stunnerd-config`. | No |
| `realm` | `string` | The STUN/TURN authentication realm to be used for clients to authenticate with STUNner. The realm must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character. Default: `stunner.l7mp.io`. | No |
| `authType` | `string` | Type of the STUN/TURN authentication mechanism. Default: `plaintext`. | No |
| `username` | `string` | The `username` for [`plaintext` authentication](/doc/AUTH.md). | No |
| `password` | `string` | The credential for [`plaintext` authentication](/doc/AUTH.md). | No |
| `metricsEndpoint` | `string` | The metrics server (Prometheus) endpoint URL for the `stiunnerd` pods.| No |
| `healthCheckEndpoint` | `string` | HTTP health-check endpoint exposed by `stiunnerd`. Liveness check will be available on path `/live` and readiness check on path `/ready`. Default is to enable health-checking on `http://0.0.0.0:8086`.| No |
| `sharedSecret` | `string` | The shared secret for [`longterm` authentication](/doc/AUTH.md). | No |
| `authLifetime` | `int` | The lifetime of [`longterm` authentication](/doc/AUTH.md) credentials in seconds. Not used by STUNner.| No |
| `loadBalancerServiceAnnotations` | `map[string]string` | A list of annotations that will go into the LoadBalancer services created automatically by STUNner to obtain a public IP addresses. See more detail [here](https://github.com/l7mp/stunner/issues/32). | No |

Note that at least a valid username/password pair *must* be supplied for `plaintext`
authentication, or a `sharedSecret` for the `longterm` mode. Missing both is an error.

Except the TURN authentication realm, all GatewayConfig resources are safe under modification. That
is, the `stunnerd` daemons know how to reconcile a change in the GatewayConfig without restarting
the TURN server. Changing the realm, however, induces a full TURN server restart.

## Gateway

Gateways describe the STUN/TURN server listeners exposed to clients.

In the below example, we open a STUN/TURN listener on the UDP port 3478.  STUNner will
automatically expose this listener on a public IP address and port by creating a [LoadBalancer
service](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) for each
Gateway. Then, it awaits clients to connect and, once authenticated, forward client connections to
an arbitrary service backend.

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: udp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: UDP
```

Below is a quick reference of the most important fields of the Gateway
[`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects).

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `gatewayClassName` | `string` | The name of the GatewayClass that provides the root of the hierarchy the Gateway is attached to. | Yes |
| `listeners[0].name` | `string` | Name of the TURN listener. | No |
| `listeners[0].port` | `int` | Network port for the TURN listener. | Yes |
| `listeners[0].protocol` | `string` | Transport protocol for the TURN listener. Either UDP or TCP; support for TLS and DTLS will be added in the next release. | Yes |

Note that STUNner assumes that there is only a *single* listener specified in each Gateway (that is
why we fixed the listener list index above at 0). Multiple listeners are accepted and STUNner will
install the corresponding TURN server listeners for each, but it will create only a *single*
LoadBalancer service to expose the entire Gateway with all the listeners. Thus, the resultant
public address/port provides access only to the first listener in the list. You can always create
the missing LoadBalancer services manually, but this is not encouraged., Create *two* separately
Gateways if you want multiple listeners, each with a single listener only.

Gateway resources are *not* safe for modification. For various reasons rooted in the limitations of
the [pion/turn](https://github.com/pion/turn) library that provides the TURN services to STUNner,
`stunnerd` daemons cannot add/remove TURN server listeners without restarting the whole TURN
server. This will then terminate all active client sessions. We plan to address this restriction in
a later release; until then, it is best to refrain from modifying the Gateways in production
deployments. The suggested workaround is to create *another* STUNner gateway hierarchy, make the
necessary changes there, direct clients to the new STUNner gateway service by providing them a new
ICE server configuration, and finally delete the old hierarchy.

## UDPRoute

UDPRoute resources can be attached to Gateways to specify the backend services permitted to be
reached via the Gateway. Multiple UDPRoutes can attach to a Gateway, and each UDPRoute can specify
multiple backend services; in this case access to *all* backends in *each* of the attached
UDPRoutes is allowed. An UDPRoute can be attached only to a Gateway *in the same namespace*, by
setting the `parentRef` to the Gateway's name. Attaching Gateways and UDPRoutes across a namespace
boundary is prohibited.

The below UDPRoute will configure STUNner to route client connections received on the Gateway
called `udp-gateway` to the media server pool identified by the Kubernetes service
`media-server-pool` in the `media-plane` namespace.

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
```

Below is a quick reference of the most important fields of the UDPRoute
[`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects).

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `parentRefs` | `[]string` | A list of Gateways in the same namepace to attach the route to. Attaching UDPRoutes across namespaces is prohibited. | Yes |
| `rules.backendRefs` | `list` | A list of `name`/`namespace` pairs specifying the backend services reachable through the UDPRoute. It is allowed to specify a service from a namespace other than the UDPRoute's own namespace. | No |

UDPRoute resources are safe for modification: `stunnerd` knows how to reconcile modified routes
without restarting the TURN server. 

## Status

Kubernetes has a very useful feature: most resources contain a `status` subresource that describes
the current state of the object, supplied and updated by the Kubernetes system and its
components. The Kubernetes control plane continually and actively manages every object's actual
state to match the desired state you supplied and updates the status field to indicate whether any
error was encountered during the reconciliation process.

For instance, below is the status from a successfully reconciled Gateway, with one UDPRoute
successfully attached to the Gateway:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
...
spec:
...
status:
  conditions:
  - lastTransitionTime: ...
    type: Scheduled
    status: "True"
    reason: Scheduled
    message: gateway under processing by controller "stunner.l7mp.io/gateway-operator"
  - lastTransitionTime: ...
    type: Ready
    status: "True"
    reason: Ready
    message: gateway successfully processed by controller "stunner.l7mp.io/gateway-operator"
  listeners:
  - attachedRoutes: 1
    conditions:
    - lastTransitionTime: ...
      type: Detached
      status: "False"
      reason: Attached
      message: listener accepted
    - lastTransitionTime: ...
      type: ResolvedRefs
      status: "True"
      reason: ResolvedRefs
      message: listener object references sucessfully resolved
    - lastTransitionTime: ...
      type: Ready
      status: "True"
      reason: Ready
      message: public address found for gateway
...
```

If you are not sure about whether the STUNner gateway operator successfully picked up your Gateways
or UDPRoutes, it is worth checking the status to see what went wrong.

```console
kubectl get <reosurce> -n <namespace> <name> -o jsonpath='{.status}'
```

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

