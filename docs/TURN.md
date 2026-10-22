# TURN gateway

The TURN gateway is the WebRTC face of STUNner: browsers and WebRTC clients open a TURN allocation
on the public address of a STUNner Gateway, and STUNner relays their media to the WebRTC media
servers running in ordinary Kubernetes pods (e.g., [LiveKit](https://livekit.io),
[Janus](https://janus.conf.meetecho.com), [mediasoup](https://mediasoup.org)), or between the
clients themselves. This is the original STUNner use case, and the one every tutorial starts from.

## Table of Contents

1. [Concepts](#concepts)
1. [A TURN Gateway](#a-turn-gateway)
1. [Client configuration](#client-configuration)
1. [Testing](#testing)
1. [Kernel offload](#kernel-offload)

## Concepts

A TURN Gateway is a plain Gateway with `TURN-UDP`, `TURN-TCP`, `TURN-TLS` or `TURN-DTLS` listeners.
Every listener is a TURN server over the respective transport, and the UDPRoutes attached to the
listener name the peers its clients may reach: the TURN server grants a client a permission only for
the pods of the backends of the routes (or the address ranges of a StaticService), every other peer
is refused.

STUNner supports two [deployment models](DEPLOYMENT.md#architectural-models): in the *headless*
model it is a plain TURN server the clients use to reach each other, in the *media-plane* model it
is the gateway that ingests the media of the clients into a media server pool deployed behind it.
In the media-plane model the media servers need no public address: they run in the private pod
network, and the [ICE mode](DEPLOYMENT.md#webrtc-ice-models) decides whether they use STUNner as
their TURN server too.

## A TURN Gateway

The below Gateway exposes a TURN server over UDP port 3478 on a LoadBalancer Service, and the
UDPRoute lets its clients reach the pods of the `media-server` Service in the `media-plane`
namespace. The GatewayClass and the GatewayConfig of the [installation](INSTALL.md) set the
credentials the clients authenticate with (see [authentication](AUTH.md)).

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: udp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: TURN-UDP
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: media-plane
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server
          namespace: media-plane
```

Add a `TURN-TLS` listener on port 443 for clients behind firewalls that pass only TLS: see
[listener configuration](GATEWAY.md#listener-configuration) for the certificate, and the [Gateway
reference](GATEWAY.md) and the [route reference](ROUTE.md) for every field. A
[TURNRoute](ROUTE.md#turnroute) relays the allocations through an upstream TURN server instead.

## Client configuration

The clients need the ICE server configuration of the Gateway: the TURN URIs of its listeners and
valid credentials. The STUNner [authentication service](AUTH.md) returns it in the format the
`RTCPeerConnection` takes, always in sync with the running configuration:

```console
curl "http://stunner-auth.stunner-system:8088/ice?service=turn"
```

Your application server queries the service and hands the result to the clients, which pass it to
the `RTCPeerConnection` unchanged. No other client-side change is needed.

## Testing

`stunnerctl icetest` deploys a WebRTC echo server, exposes it through a UDP and a TCP TURN Gateway
of its own, connects to it and reports the loss and the latency, the quickest test of a fresh
installation. The [UDP echo tutorial](examples/udp-echo/README.md) walks through a TURN Gateway
step by step, and `mediactl` streams packets through a TURN allocation to a UDP echo peer and
measures the loss and the round trip:

```console
mediactl turn send -t 10s k8s://stunner/udp-gateway/udp-listener k8s://stunner/udp-echo:9001
```

`mediactl` finds the public address and the credentials of the listener and a pod of the echo
Service through your kubeconfig; see the [mediactl manual](cmd/mediactl.md) for plain addresses and
more.

## Kernel offload

STUNner can relay the UDP channels of a TURN/UDP listener in the Linux kernel instead of in user
space: once a client binds a channel to a peer of a UDPRoute backend, the ChannelData from the
client and the datagrams from the peer are relayed in the kernel, one offloaded flow per channel.
The allocations, the permissions and the channel bindings stay in user space, and so does the data
of a client that sends it in Send indications instead of a channel. TURN over TCP, TLS or DTLS, and
relaying through an upstream TURN server of a [TURNRoute](ROUTE.md#turnroute), are not offloaded.
See the [kernel offload](PREMIUM_REFERENCE.md#kernel-offload) reference for the offload limit of
your tier, the settings and the monitoring.
