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

## GatewayClass


## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

