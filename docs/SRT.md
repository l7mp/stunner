# SRT gateway

> [!WARNING]
> The SRT gateway is in beta: the API may change in any release and there is no support on any tier.
> Please test and report issues, but in any way, use it at your own risk.

The SRT gateway is a beta feature: its API may change in any release, it comes with no support on
any tier, and the number of SRT Gateways and parallel sessions depends on the tier (see
[Limitations](#limitations)). Within these limits, STUNner ingests
[SRT](https://github.com/Haivision/srt) streams into Kubernetes: encoders, drones and cameras
publish their streams to one public UDP port, decoders and players read them from the same port, and
STUNner routes every connection to the media server pod that holds its stream (e.g.,
[MediaMTX](https://github.com/bluenviron/mediamtx)), with no host networking and no per-pod
Services.

## Table of Contents

1. [Concepts](#concepts)
1. [Streaming with MediaMTX](#streaming-with-mediamtx)
1. [Testing](#testing)
1. [Kernel offload](#kernel-offload)
1. [Limitations](#limitations)

## Concepts

SRT (Secure Reliable Transport) is the UDP transport of live video contribution: an encoder in the
field sends an MPEG-TS stream over a lossy network, SRT retransmits what is lost within a fixed
latency budget and encrypts the stream with a passphrase shared by the two ends. The caller names
the stream in the *stream ID* of the SRT handshake, by convention
`#!::r=<stream>,m=publish` to publish a stream and `#!::r=<stream>,m=request` to read one.

A single SRT stream passes any UDP load balancer. Scaling out is what breaks: with several media
server pods behind one address, the load balancer spreads the connections over the pods, so the
encoder publishing `live/van7` lands on one pod and the decoder reading `live/van7` on another one,
which has no such stream. STUNner reads the stream ID in the handshake and sends every connection
naming the same stream to the same pod:

- the first connection of a stream places it on the backend with the fewest streams, whatever its
  mode, publisher or reader;
- every later connection naming the stream goes to the same backend;
- a stream stays placed for 30 seconds after its last connection closes, so a publisher that
  reconnects after a network outage returns to its readers;
- new backends receive new streams, the placed streams never move.

![STUNner SRT gateway](img/stunner_srt.svg)

An SRT gateway is a Gateway with a listener of protocol `SRT`, a UDP port taking SRT callers. The
UDPRoutes attached to the listener name the media servers, STUNner connects to them at the `port` of
the backend reference. Every listener of an SRT Gateway must be `SRT`, see the [Gateway
reference](GATEWAY.md#srt-gateways).

The encryption is end to end: STUNner never sees the passphrase, the media server checks it. A
stream ID that names no stream (`#!::m=publish`), a stream with no backend, and a stream over the
tier's limit of parallel sessions are rejected in the handshake, so the caller sees a clean
refusal, not a timeout. A stream ID outside the `#!::` syntax names the stream with the whole
string, so use the `#!::` syntax with media servers that take several syntaxes: MediaMTX's own
`publish:live/van7` and `read:live/van7` name two different streams for STUNner.

## Streaming with MediaMTX

The below manifests deploy three MediaMTX replicas taking SRT on UDP port 8890 and accepting any
stream encrypted with the passphrase `my-srt-passphrase` (MediaMTX takes its settings from `MTX_`
environment variables too), an SRT Gateway on the public UDP port 9000, and the UDPRoute between
the two.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mediamtx
  namespace: streaming
spec:
  replicas: 3
  selector:
    matchLabels:
      app: mediamtx
  template:
    metadata:
      labels:
        app: mediamtx
    spec:
      containers:
        - name: mediamtx
          image: bluenviron/mediamtx:latest
          env:
            - name: MTX_PATHDEFAULTS_SRTPUBLISHPASSPHRASE
              value: my-srt-passphrase
            - name: MTX_PATHDEFAULTS_SRTREADPASSPHRASE
              value: my-srt-passphrase
          ports:
            - containerPort: 8890
              protocol: UDP
---
apiVersion: v1
kind: Service
metadata:
  name: mediamtx
  namespace: streaming
spec:
  selector:
    app: mediamtx
  ports:
    - name: srt
      port: 8890
      protocol: UDP
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: srt-gateway
  namespace: streaming
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: srt
      protocol: SRT
      port: 9000
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: mediamtx
  namespace: streaming
spec:
  parentRefs:
    - name: srt-gateway
  rules:
    - backendRefs:
        - name: mediamtx
          port: 8890
```

Once the Gateway is programmed, its public address is in its status:

```console
export SRT_IP=$(kubectl -n streaming get gateway srt-gateway -o jsonpath='{.status.addresses[0].value}')
```

## Testing

Publish a test pattern with [FFmpeg](https://ffmpeg.org), encrypted with the passphrase of the
media servers:

```console
ffmpeg -re -f lavfi -i testsrc=size=1280x720:rate=30 -c:v libx264 -preset veryfast -f mpegts \
    "srt://${SRT_IP}:9000?streamid=#!::r=live/test,m=publish&passphrase=my-srt-passphrase"
```

Watch it from another terminal:

```console
ffplay "srt://${SRT_IP}:9000?streamid=#!::r=live/test,m=request&passphrase=my-srt-passphrase"
```

Both connections reach the same MediaMTX pod, whichever pod the load balancer would have picked.
Publish a second stream under another name and it lands on another pod.

The `mediactl` tool of the STUNner releases has an SRT server for tests, which passes every
published stream to the readers of the same stream on the same server and echoes the connections
that neither publish nor read: run `mediactl srt serve :8890` in the backend pods instead of
MediaMTX and stream packets through the Gateway with `mediactl srt publish
k8s://streaming/srt-gateway live/test` and `mediactl srt read k8s://streaming/srt-gateway live/test`:
`mediactl` finds the Gateway's public address through your kubeconfig, and measures the loss.

## Kernel offload

STUNner can relay the streams of an SRT Gateway in the Linux kernel instead of in user space, up to
the offload limit of your tier (see the [pricing](https://l7mp.io/pricing); the limits may change).
The offload is opt-in: set `offloadEngine` in the Dataplane and grant the dataplane pods the
capabilities it needs; see the [kernel offload](PREMIUM_REFERENCE.md#kernel-offload) reference. The
handshakes and the shutdowns of a stream stay in user space.

## Limitations

- The free tier serves one SRT Gateway with at most 10 parallel sessions (streams), the member
  tier three with at most 100 each, the enterprise tier any number. When the GatewayConfig sets a
  `userQuota`, the lower of the two caps the sessions.
- One dataplane replica per Gateway: the placement of the streams is kept per dataplane pod.
- Callers only: STUNner takes the connections of SRT callers, it does not call SRT listeners
  (pulling an encoder in listener mode) and does not take rendezvous connections.
- The streams are placed by the stream ID: a media server that takes the stream name elsewhere (in
  the passphrase, or after the handshake) gets the streams placed by the whole stream ID.
- Bonded connections (socket groups) are relayed connection by connection, untested.
