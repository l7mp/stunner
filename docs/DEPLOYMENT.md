# Deployment models

STUNner can be deployed in many different ways, supporting a wide range of operational
requirements. STUNner supports multiple [architectural models](#architectural-models) where it can
act either as a simple headless STUN/TURN server or a fully fledged ingress gateway in front of an
entire Kubernetes-based media server pool. In addition, if STUNner is configured as an ingress
gateway then it can run in one of two [ICE models](#ice-models), based on whether only the
client connects via STUNner or both clients and media servers use STUNner to set up the media-plane
connection. 

<!-- Third, STUNner can run in one of several [data plane models](#data-plane-models),
based --> <!-- on whether the dataplane is automatically provisioned or the user has to manually
supply the --> <!-- dataplane pods for STUNner. -->

## Architectural models

STUNner supports two architectural models, depending on whether it is used as a simple STUN/TURN
service or it is functioning as an actual ingress gateway service to feed traffic into a media
server pool deployed *behind* STUNner.

### Headless deployment model

In the *headless deployment model* STUNner acts as a simple scalable STUN/TURN server that WebRTC
clients and servers can use as a NAT traversal facility for establishing media connections between
themselves. This is not that much different from a standard public STUN/TURN server setup, but in
this case the STUN/TURN servers are deployed into Kubernetes.

![STUNner headless deployment architecture](img/stunner_standalone_arch.svg)

<!-- > **Warning**   -->
<!-- For STUNner to be able to connect WebRTC clients and servers in the headless model *all* the -->
<!-- clients and servers *must* use STUNner as the TURN server. This is because STUNner opens the -->
<!-- transport relay connections *inside* the cluster, on a private IP address, and this address is -->
<!-- reachable only to STUNner itself, but not for external STUN/TURN servers. -->

### Media-plane deployment model

In the fully fledged *media-plane deployment model*, STUNner implements a STUN/TURN ingress gateway
service that WebRTC clients can use to open a transport relay connection to the media servers
running *inside* the Kubernetes cluster. This makes it possible to deploy WebRTC application
servers and media servers into ordinary Kubernetes pods, taking advantage of Kubernetes's excellent
tooling to manage, scale, monitor and troubleshoot the WebRTC infrastructure like any other
cloud-bound workload.

![STUNner media-plane deployment architecture](img/stunner_arch.svg)

There is no limitation as to how many gateway and media server pods can be opened in this model,
which theoretically means limitless scalability. Furthermore, by creating connection-tracking state
for each client session STUNner supports the dynamic scale-out of the media server pool without
dropping active client connections. Whether scaling STUNner itself causes client connection drops
depend on the cloud provider's load-balancer service: if the load-balancer creates conntrack state
for clients' UDP transport streams then STUNner can be scaled freely, otherwise scaling STUNner may
result the [disconnection of a small number of client
connections](https://cilium.io/blog/2020/11/10/cilium-19/#maglev).

## ICE models

The peers willing to create a connection via STUNner (e.g., two clients as per the headless model,
or a client and a media server in the media-plane deployment model) need to decide how to create
ICE candidates.

### Asymmetric ICE mode

In *asymmetric ICE mode*, one peer is configured with STUNner as the TURN server and the other peer
runs with no STUN or TURN servers whatsoever. The first peer will create a TURN transport relay
connection via STUNner to which the other peer can directly join. Asymmetric ICE mode is the
recommended way for the media-plane deployment model.

![STUNner asymmetric ICE mode](img/stunner_asymmetric_ice.svg)

In this model, the client is configured with STUNner as the TURN server so at a certain point in
the ICE conversation it opens a TURN transport relay connection via STUNner. The IP address of the
resultant ICE [relay
candidate](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type) is a private pod
IP address, namely the IP address of the `stunnerd` pod that happens to receive the client
connection. In contrast, servers run without any STUN/TURN server whatsoever, so they generate
[host ICE candidates](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type)
only. Due to servers being deployed into ordinary Kubernetes pods, the server's host candidate will
likewise contain a private pod IP address. Then, since in the Kubernetes networking model ["pods
can communicate with all other pods on any other node without a
NAT"](https://kubernetes.io/docs/concepts/services-networking), the client's relay candidate and
the server's host candidate will have direct connectivity in the Kubernetes private container
network and the ICE connectivity check will succeed. See more explanation
[here](examples/kurento-one2one-call/README.md#what-is-going-on-here).

Refrain from configuring additional public STUN/TURN servers apart from STUNner itself. The rules
to follow for setting the [ICE server
configuration](https://github.com/l7mp/stunner#configuring-webrtc-clients) in asymmetric ICE mode
are as below:
- on the client, set STUNner as the *only* TURN server and configure *no* STUN servers, and
- on the server do *not* configure *any* STUN or TURN server whatsoever.

Deviating from these rules *might* work in certain cases, but may have uncanny and hard-to-debug
side-effects. For instance, configuring clients and servers with public STUN servers in certain
unlucky situations may allow them to connect via server-reflexive ICE candidates, completely
circumventing STUNner. This is on the one hand extremely fragile and, on the other hand, a security
vulnerability; remember, STUNner should be the *only* external access point to your media plane. It
is a good advice to set the `iceTransportPolicy` to `relay` on the clients to avoid side-effects:
this will prevent clients from generating host and server-reflexive ICE candidates, leaving STUNner
as the only option to obtain an ICE candidate from.

### Symmetric ICE mode

In the symmetric ICE mode both the client and the server obtain an ICE [relay
candidate](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate/type) from STUNner and
the connection occurs directly via STUNner. This is the simplest mode for the headless deployment
model, but symmetric mode can also be used for the media-plane model as well to connect clients to
media servers.

![STUNner symmetric ICE mode](img/stunner_symmetric_ice.svg)

In the symmetric mode the following rules apply for setting the [ICE server
configuration](https://github.com/l7mp/stunner#configuring-webrtc-clients):

- on both the clients and the server set STUNner as the *only* TURN server and configure *no* STUN
  servers, and
- set the `iceTransportPolicy` to `relay` on both sides.

The `iceTransportPolicy: relay` setting is mandatory in this case, otherwise the connection falls
back to the asymmetric mode (this is a consequence of the way [ICE assigns
priorities](https://www.ietf.org/rfc/rfc5245.txt) to different connection types).  Furthermore, it
is a good practice to configure the STUNner TURN URI in the server-side ICE server configuration
with the *internal* IP address and port used by STUNner (i.e., the ClusterIP of the `stunner`
Kubernetes service and the corresponding port), otherwise the server might connect via the external
LoadBalancer IP causing an unnecessary roundtrip (hairpinning).

The symmetric mode means more overhead compared to the asymmetric mode, since STUNner now performs
TURN encapsulation/decapsulation for both sides. However, the symmetric mode comes with certain
operational advantages. Namely, this is the only ICE mode that would allow STUNner to obscure the
internal IP addresses in the ICE candidates from attackers; note that this is not implemented yet,
but feel free to open an issue if [exposing internal IP addresses](SECURITY.md) is blocking
you from adopting STUNner.

<!-- ## Data plane models -->

<!-- STUNner supports two dataplane provisioning modes. In the default *managed* mode, the dataplane -->
<!-- pods (i.e., the `stunnerd` pods) are provisioned automatically per each Gateway existing in the -->
<!-- cluster. In the *legacy* mode, the dataplane is supposed to be deployed by the user manually by -->
<!-- installing the `stunner/stunner` Helm chart into the target namespaces. Legacy mode is considered -->
<!-- obsolete at this point and it will be removed in a later release. -->
