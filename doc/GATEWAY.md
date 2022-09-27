# Gateway API reference

The [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) exposes the
control plane configuration using the standard [Kubernetes Gateway
API](https://gateway-api.sigs.k8s.io). This allows to configure STUNner in the familiar
YAML-engineering style via Kubernetes manifests. The below reference gives a quick overview of the
Gateway API resources, with some notes on the specifics of STUNner's gateway operator
implementation.  See the [official docs](/doc/GATEWAY.md) for a full reference.

## Overview

The main unit of the control plane configuration is the *Gateway hierarchy*. Here, a Gateway
hierarchy is a collection of [Kubernetes Custom
Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources)
that together describe the way media traffic should enter the cluster via STUNner, including public
IP addresses and ports clients can use to reach STUNner, TURN credentials, routing rules, etc. The
anchor of the Gateway hierarchy is the GatewayClass object, and the rest of the resources form a
complete hierarchy underneath it: the GatewayConfig describes general STUNner configuration,
Gateways define a public IP address, port and transport protocol for STUNner to open TURN servers,
and UDPRoutes point to the backend services client traffic should be forwarded to. 

![Gateway hierarchy](/doc/gateway_api.svg)

In general the boundary of the Gateway hierarchy is the namespace of the Gateway API resources
specified in the namespace, except the GatewayClass, which is
[cluster-scoped](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions). You
can run multiple Gateway hierarchies side-by-side, by creating multiple namespaces and installing a
separate set of Gateway API resources into each, and installing a separate dataplane into each
namespace.

## GatewayClass

The GatewayClass resource provides the root of the Gateway hierarchy. GatewayClass resources are
cluster-scoped, so they can be attached to from any namespace, but in STUNner we usually assume
that each Gateway hierarchy will be restricted to a single namespace and specify a separate global
GatewayClass as the anchor of the hierarchy.

Below is a sample GatewayClass resource. Each GatewayClass must specify a controller that will
manage the Gateway objects created under the hierarchy. In our case, this must be set to
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

Below is a quick reference of the most important GatewayClass fields.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `spec.controllerName` | `string` | Name of the name of the controller that is managing the Gateways of this class. The value of this field MUST be specified as `stunner.l7mp.io/gateway-operator`. | Yes |
| `spec.parametersRef` | `object` | Reference to a GatewayConfig resource, identified by the `name` and `namespace`, which describes general STUNner configuration. The settings `group: "stunner.l7mp.io"` and `kind: GatewayConfig` are default and can be omitted, specifying any other group or kind is an error. | Yes |
| `spec.description` | `string` | Description helps describe a GatewayClass with more details. | No |

## GatewayConfig

The GatewayConfig resource provides general configuration for STUNner, most importantly the
STUN/TURN authentication [credentials](/doc/AUTH.md) clients can use to authenticate with
STUNner. GatewayClass resources attach a STUNner configuration to the hierarchy by specifying a
particular GatewayConfig in the `parametersRef`.  GatewayConfig resources are namespaced, and every
hierarchy can contain at most one GatewayConfig. Failing to specify a GatewayConfig is an error
because the authentication credentials cannot learned by the dataplane in such cases, and the
STUNner gateway operator will refuse to generate a dataplane running config until the user attaches
a valid GatewayConfig to the hierarchy.

The following example sets the [`plaintext` authentication](/doc/AUTH.md) mechanism for STUNner,
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
EOF
```

Below is a quick reference of the most important GatewayConfig fields.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `spec.stunnerConfig` | `string` | The name of the ConfigMap into which the operator renders the `stunnerd` running configuration. Default: `stunnerd-config`. | No |
| `spec.realm` | `string` | The STUN/TURN authentication realm to be used for clients to authenticate with STUNner. The realm must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character. Default: `stunner.l7mp.io`. | No |
| `spec.authType` | `string` | Type of the STUN/TURN authentication mechanism used by STUNner. Default: `plaintext`. | No |
| `spec.username` | `string` | The `username` credential for [`plaintext` authentication](/doc/AUTH.md) | No |
| `spec.password` | `string` | The `password` credential for [`plaintext` authentication](/doc/AUTH.md) | No |
| `spec.sharedSecret` | `string` | The shared secret for [`longterm` authentication](/doc/AUTH.md). | No |
| `spec.authLifetime` | `int` | The lifetime of [`longterm` authentication](/doc/AUTH.md) credentials in seconds. Not used by STUNner.| No |
| `spec.loadBalancerServiceAnnotations` | `map[string]string` | A list of annotations that will go into the LoadBalancer services created automatically by STUNner per Gateway to provide a public IP addresses. See more details [here](https://github.com/l7mp/stunner/issues/32). | No |

Note that at least a valid username/password pair *must* be supplied for `plaintext`
authentication, or a `sharedSecret` for the `longterm` mode. Missing both is an error.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

