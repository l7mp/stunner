# SIP gateway

> [!WARNING]
> The SIP gateway is experimental: the API may change, there is no support, and the official
> STUNner images serve only one SIP Gateway with at most 5 parallel calls. Please test and report
> issues, but in any way use it at your own risk.

The SIP gateway is an experimental feature: its API may change in any release, it comes with no
support, and the official STUNner images serve a single SIP Gateway with at most 5 parallel calls on
every tier. Within these limits, STUNner ingests SIP trunks into Kubernetes: carriers send SIP and
RTP to one public address, STUNner proxies the SIP signaling to an in-cluster SIP backend (e.g.,
[LiveKit SIP](https://docs.livekit.io/sip/)) and relays the media of every call between the carrier
and the in-cluster media peers, with no host networking and no per-pod Services.

## Table of Contents

1. [Concepts](#concepts)
1. [IP-authenticated trunks](#ip-authenticated-trunks)
1. [Registration-based trunks](#registration-based-trunks)
1. [Separate SIP and media servers](#separate-sip-and-media-servers)
1. [Limitations](#limitations)

## Concepts

A SIP gateway is a plain Gateway whose `spec.infrastructure.parametersRef` names a `SIPTrunk` in
the namespace of the Gateway. The SIPTrunk names the listener of the Gateway that carries the media
and tells how the carriers connect. STUNner supports the two common trunk models:

- *IP-authenticated trunks*: the carriers send SIP to the Gateway from known address ranges, as
  Twilio Elastic SIP Trunking, Telnyx IP connections or DIDWW do;
- *registration-based trunks*: STUNner dials the carrier's registrar, registers a phone number and
  takes the inbound calls over the same connection, as the SIP accounts of most SMB and ISP
  carriers work.

The [deployment models](DEPLOYMENT.md#sip-deployment-models) show both, the
[reference](REFERENCE.md#siptrunk) lists every SIPTrunk field, and the [SIP echo
tutorial](examples/sip-echo/README.md) tries the whole path with a softphone.

Every listener of the Gateway other than the media listener is a SIP listener. The listener
protocols are plain transports: `UDP`, `TCP` or `TLS` for SIP, `UDP` for the media. The media
listener is a single UDP port carrying the RTP and RTCP of every call.

UDPRoutes attach to the listeners of a SIP Gateway as to any Gateway: a UDPRoute on a SIP listener
is the SIP next hop (STUNner proxies the calls to the backend pods at the backend port, round robin),
a UDPRoute on the media listener lists the media peers (the pods STUNner relays the media to). A
UDPRoute with no `sectionName` attaches to every listener, so a single route serves both roles when
the SIP backend also terminates the media, as LiveKit SIP does. A SIP server with a media server of
its own, like Kamailio with rtpengine, takes a route on each (see the [deployment
models](DEPLOYMENT.md#separate-sip-and-media-servers)); a backend with a `port` and an `endPort`
restricts the media peers to that port range.

Carriers match the media of a call by the exact source address of the media they receive, so the
Gateway's load balancer must preserve the client source address: the
`stunner.l7mp.io/external-traffic-policy: local` annotation sets the Service's
`externalTrafficPolicy` to `Local` (see [load balancer configuration](REFERENCE.md#load-balancer-configuration)).
A load balancer that rewrites the source address (e.g., a full proxy) breaks the media of every
call.

A Gateway whose `parametersRef` names a missing or malformed SIPTrunk, or a kind the operator does
not support, is not accepted and gets no dataplane. Of several SIP Gateways the oldest one by
creation time is served, the others are not accepted, with the reason in their status.

## IP-authenticated trunks

The carrier sends its calls to the public address of a SIP listener, so configure the carrier's
trunk with the address in the Gateway's status (or a DNS name pointing to it), and list the
carrier's signaling address ranges in `allowedSources`: the SIP listeners admit calls only from
there.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: sip-gateway
  namespace: voice
  annotations:
    stunner.l7mp.io/external-traffic-policy: local
spec:
  gatewayClassName: stunner-gatewayclass
  infrastructure:
    parametersRef:
      group: stunner.l7mp.io
      kind: SIPTrunk
      name: carriers
  listeners:
    - name: sip
      protocol: UDP
      port: 5060
    - name: media
      protocol: UDP
      port: 30000
---
apiVersion: stunner.l7mp.io/v1alpha1
kind: SIPTrunk
metadata:
  name: carriers
  namespace: voice
spec:
  mediaListener: media
  allowedSources:
    - 203.0.113.0/24
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: livekit-sip
  namespace: voice
spec:
  parentRefs:
    - name: sip-gateway
  rules:
    - backendRefs:
        - name: livekit-sip
          port: 5060
```

For SIP over TLS add a `TLS` listener with `tls.certificateRefs`: cert-manager's Gateway API support
issues its certificate without further setup. Carriers verify the certificate against the address
they reach STUNner at, so for a certificate of a DNS name set `spec.addresses: [{type: Hostname,
value: <name>}]` on the Gateway.

## Registration-based trunks

STUNner dials the carrier's registrar, registers the address of record and answers the registrar's
digest challenge with the credentials of a Secret. The carrier then delivers the inbound calls over
the registered connection, so the carrier needs no route to the Gateway's SIP listeners and the
egress address of the cluster does not matter for signaling. The media still arrives at the
Gateway's media listener.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: carrier-digest
  namespace: voice
stringData:
  username: "+3612345678"
  password: "<password>"
---
apiVersion: stunner.l7mp.io/v1alpha1
kind: SIPTrunk
metadata:
  name: carriers
  namespace: voice
spec:
  mediaListener: media
  registrations:
    - name: carrier
      registrar: "sips:sip.carrier.example"
      aor: "sip:+3612345678@sip.carrier.example"
      authRef:
        name: carrier-digest
```

The Gateway and the UDPRoute are those of the inbound case; the Gateway needs no SIP listener of
its own, only the media listener. The calls of a registration go to the next hops of the routes of
the SIP listeners. A TLS registration verifies the registrar's certificate against the system roots
and the host of `registrar`. An inbound trunk and registrations may share a SIPTrunk.

## Separate SIP and media servers

Some SIP servers use a dedicated media server pool to handle real-time traffic, like Kamailio with
rtpengine. In such cases the SIP next hop and the media peers are different Services. STUNner
allows to enforce this distinction via attaching a separate UDPRoute for the SIP backends and
another one for the media server backends: the UDPRoute pointing to the SIP servers attaches to the
SIP listeners by the `sectionName`, while the UDPRoute for media attaches to the media listener,
again by `sectionName`. Other SIP servers terminate the media themselves (LiveKit SIP): a single
UDPRoute with no `sectionName` serves both roles. In any case, STUNner relays the media to the
address in the SDP the SIP server returns, which must be an address of the media server's pods.

LiveKit SIP runs with a single shared UDPRoute for both SIP signaling and media. Configure the
STUNner UDPRoute with no `sectionName`, and LiveKit SIP with its pod address in the SDP it sends:
no external IP, no host networking, no public RTP port range. Since LiveKit SIP will see the calls
to arrive from the STUNner pods instead of from the carriers, its inbound trunk's
`allowed_addresses` cannot filter the carriers; STUNner instead filters carrier traffic by
configuring `allowedSources` in the SIPTrunk. An inbound trunk that lists its `numbers` may leave
`allowed_addresses` empty, a trunk with no `numbers` can set it to the pod address range of the
cluster. 

When SIP servers and media servers are separate, STUNner must be configured with two UDPRoutes, one
attaching to the SIP listener by the `sectionName` and pointing to the cluster's SIP server pool
(e.g., Kamailio), while the other attaching to the RTP media listener and pointing to the
in-cluster media server pool (e.g., rtpengine), again by `sectionName`:

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: kamailio
spec:
  parentRefs:
    - name: sip-gateway
      sectionName: sip
  rules:
    - backendRefs:
        - name: kamailio
          port: 5060
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: rtpengine
spec:
  parentRefs:
    - name: sip-gateway
      sectionName: media
  rules:
    - backendRefs:
        - name: rtpengine
          port: 30000
          endPort: 40000
```

The `port` of a backend is the SIP port STUNner dials; a `port` with an `endPort` restricts the
media peers to that range, here rtpengine's media port range.

## Limitations

- One SIP Gateway with at most 5 parallel calls, one dataplane replica.
- A lost registration connection ends the calls over it; STUNner dials and registers again.
- No outbound calls.
- The media of a carrier is matched by the address in its SDP: carriers behind a NAT that rewrites
  their media source are not supported.
