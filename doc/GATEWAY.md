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
| `controllerName` | `string` | STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc). lkjrlekkjkrje jkjkjrkej kjtkejtkje kjtke jtkje kejt kjek jtkejtkjtke jtkjek jkej kejt kjekj tkjetkjkejktjekj | Yes |

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

