## Deployment models

STUNner supports various deployment models. In particular, it can be deployed in the headless model
where there is no media server behind it, or it can work in the media-plane model to provide
ingress services for an entire WebRTC workload. Orthogonally, there are two control-plane
deployment models: in the standalone mode users manually supply the dataplane configuration, while
in the full (default) deployment model there is a separate control plane responsible for exposing
the dataplane configuration to the users in a [declarative API](https://gateway-api.sigs.k8s.io).

## Architectural models

STUNner supports two architectural models, depending on whether it is used as a mere STUN/TURN
server or it is functioning as an actual ingress gateway service to feed traffic into a media
server pool deployed *behind* STUNner.

### Headless model

In the *headless deployment model* STUNner acts as a simple scalable STUN/TURN server that WebRTC
clients can use as a NAT traversal facility for establishing media connections between
themselves. This is not that much different from a standard public STUN/TURN server setup, but in
this case the STUN/TURN servers are deployed into Kubernetes.

![STUNner headless deployment architecture](/doc/stunner_standalone_arch.svg)

Note that for STUNner to be able to connect two or more WebRTC clients in the headless model *all*
the clients *must* use STUNner as the TURN server. This is because STUNner opens the transport
relay connections *inside* the cluster, on a private IP address, and this address is reachable only
to STUNner itself, but not for external clients or STUN/TURN servers.

### Media-plane model

In the fully fledged *media-plane deployment model*, STUNner implements a STUN/TURN ingress gateway
service that WebRTC clients can use to open a transport relay connection to the media servers
running *inside* the Kubernetes cluster. This makes it possible to deploy WebRTC application
servers and media servers into ordinary Kubernetes pods, taking advantage of Kubernetes's excellent
tooling to manage, scale, monitor and troubleshoot the WebRTC infrastructure like any other
cloud-bound workload.

![STUNner media-plane deployment architecture](/doc/stunner_arch.svg)

There is no limitation as to how many gateway and media server pods can be opened in this model,
which theoretically means limitless scalability. By creating connection-tracking state for each
client session STUNner also supports the dynamic scale-in/scale-out of the media server pool
without dropping client connections. Whether scaling STUNner itself causes client connection drops
is depending on the cloud provider's load-balancer service: if the load-balancer creates conntrack
state for clients' UDP transport streams then STUNner can be scaled freely, otherwise scaling
STUNner may result the [disconnection of a small number of client
connections](https://cilium.io/blog/2020/11/10/cilium-19/#maglev).

## Control plane models

STUNner can run in one of two modes: in the default mode STUNner configuration is controlled by a
*gateway-operator* component based on high-level intent encoded in the [Kubernetes Gateway API
resources](https://gateway-api.sigs.k8s.io), while in the *standalone model* the user configures
STUNner manually. The standalone mode provides perfect control over the way STUNner ingests media,
but at the same time it requires users to deal with the subtleties of internal STUNner APIs that
are subject to change between subsequent releases. We are actively working towards feature
completeness for STUNner's operator-ful mode, and we consider the standalone model obsolete. If
still interested, comprehensive documentation can be found [here](/doc/OBSOLETE.md).

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

