## Deployment models

STUNner supports various deployment models. First, it supports multiple
[architectures](#architectural-models) where it can act as a simple headless STUN/TURN server or it
can implement a fully fledged ingress gateway in front of a Kubernetes-based media server
pool. Second, STUNner can run in one of two[control plane models](control-plane-models), based on
whether the user manually supplies the dataplane configuration or there is a separate control plane
that automatically reconciles the dataplane state based on a high-level [declarative
API](https://gateway-api.sigs.k8s.io). Finally, there are multiple [ICE models](#ice-models), based
on whether only the client connects via STUNner or both the client and the server are configured to
use STUNner.

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
which theoretically means limitless scalability. Furthermore, by creating connection-tracking state
for each client session STUNner supports the dynamic scale-in/scale-out of the media server pool
without dropping client connections. Whether scaling STUNner itself causes client connection drops
is depending on the cloud provider's load-balancer service: if the load-balancer creates conntrack
state for clients' UDP transport streams then STUNner can be scaled freely, otherwise scaling
STUNner may result the [disconnection of a small number of client
connections](https://cilium.io/blog/2020/11/10/cilium-19/#maglev).

## Control plane models

STUNner can run in one of two modes: in the default mode STUNner configuration is controlled by a
*gateway-operator* component based on high-level intent encoded in [Kubernetes Gateway API
resources](https://gateway-api.sigs.k8s.io), while in the *standalone model* the user configures
STUNner manually. The standalone mode provides perfect control over the way STUNner ingests media,
but at the same time it requires users to deal with the subtleties of internal STUNner APIs that
are subject to change between subsequent releases. We are actively working towards feature
completeness for STUNner's operator-ful mode, and we consider the standalone model obsolete at this
point. If still interested, comprehensive documentation can be found [here](/doc/OBSOLETE.md).

## ICE models

ICE server configuration is very simple when using STUNner in the headless architecture: all
clients participating in a call must be configured with STUNner as the TURN server. However, when
using the media-plane architecture there are multiple options, as detailed below.

### Asymmetric ICE mode

The standard mode for configuring clients and media servers with STUNner is the *asymmetric ICE
mode*, whereby the client is configured with STUNner as the TURN server and media servers run with
no STUN or TURN servers whatsoever. 

![STUNner asymmetric ICE mode](/doc/stunner_asymmetric_ice.svg)

In this model, the client is configured with STUNner as the TURN server so at a certain point in
the ICE conversation it opens a TURN transport relay connection via STUNner. The IP address of the
resultant ICE [relay
candidate](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type) is a private pod
IP address, in particular, the IP address of the `stunnerd` pod that happens to receive the client
connection. In contrast, servers run without any STUN/TURN server so they generate [host ICE
candidates](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type) only. Due to
servers being deployed into ordinary Kubernetes pods, the server's host candidate also contains a
private pod IP address. Since in the Kubernetes networking model ["pods can communicate with all
other pods on any other node without
NAT"](https://kubernetes.io/docs/concepts/services-networking), the clients relay candidate and the
servers' host candidate will have direct connectivity in the Kubernetes private container network
and the ICE connectivity check will succeed. See more explanation
[here](/examples/kurento-one2one-call/README.md#what-is-going-on-here).

A word of warning here: when using STUNner refrain from configuring additional public STUN/TURN
servers besides STUNner. The rules to follow in setting the [ICE server
configuration](/README.md#configuring-webrtc-clients) in asymmetric ICE mode are as below:
* on the client, set STUNner as the *only* TURN server and configure *no* STUN servers, whereas
* on the server do *not* configure *any* STUN or TURN servers at all.
   
Note that deviating from the above rules *might* work in certain cases, but may have uncanny and
hard-to-debug side-effects. For instance, configuring clients and servers with public STUN servers
in certain unlucky situations may allow them to connect via server-reflexive ICE candidates,
completely circumventing STUNner. This is on the one hand extremely fragile and, on the other hand,
a security vulnerability; remember, STUNner should be the *only* external access point to your
media plane. It is a good advice to set the `iceTransportPolicy` to `relay` on the clients to avoid
these side-effects: this will prevent clients from generating host and server-reflexive ICE
candidates, leaving STUNner as the only option to obtain an ICE candidate from.

### Symmetric ICE mode

In the symmetric mode both the client and the server obtain an ICE [relay
candidate](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type) from STUNner and
the connection occurs directly via STUNner. 

![STUNner symmetric ICE mode](/doc/stunner_symmetric_ice.svg)

In the symmetric mode the following rules apply for setting up the [ICE server
configuration](/README.md#configuring-webrtc-clients):
* set STUNner as the *only* TURN server and configure *no* STUN servers on both the clients and the
  server, and
* set the `iceTransportPolicy` to `relay` on both sides.

Note that the `iceTransportPolicy: relay` setting is mandatory in this case, otherwise the
connection falls back to the asymmetric mode (this is a consequence of the way [ICE orders
priorities](https://www.ietf.org/rfc/rfc5245.txt) to different connection types).  Furthermore, it
a good practice to configure the STUNner TURN URI in the server-side ICE server configuration with
the *internal* IP address and port used by STUNner (i.e., the ClusterIP of the `stunner` Kubernetes
service and the corresponding port), otherwise the server might connect via the external
LoadBalancer IP causing an unnecessary roundtrip. 

The symmetric mode means more overhead compared to the asymmetric mode, since STUNner now needs to
perform the TURN encapsulation/decapsulation for both sides. However, the symmetric mode comes with
certain operational advantages. Namely, this is the only ICE mode that would allow STUNner to
obscure the internal IP addresses in the ICE candidates from attackers; note that this is not
implemented yet, feel free to create an issue if [exposing internal IP addresses](/doc/SECURITY.md)
is blocking you from adopting STUNner.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

