# SRT gateway

> [!WARNING]
> The SRT gateway is in beta: the API may change in any release and there is no support on any
> tier.  Certain features require premium support: see [here](https://l7mp.io/pricing) for the
> limits and pricing. Please test and report issues, but in any way, use it at your own risk.

STUNner lets you ingest [SRT](https://github.com/Haivision/srt) streams into Kubernetes: encoders,
drones and cameras publish their streams to one public UDP port, decoders and players read them
from the same port, and STUNner routes every connection to the media server pod that holds its
stream (e.g., [MediaMTX](https://github.com/bluenviron/mediamtx)), with no host networking and no
per-pod Services.

The [SRT echo tutorial](examples/srt-echo/README.md) shows the whole path with [`mediactl`](cmd/mediactl.md), the
[MediaMTX tutorial](examples/srt-mediamtx/README.md) with a real media server and FFmpeg.

Note that SRT gateways cannot be scaled beyond a single pod at the moment. STUNner does not support
outbound calls either and does not take rendezvous connections.  File a wishlist issue if you want
support.

## Table of Contents

1. [Concepts](#concepts)
1. [Streaming with MediaMTX](#streaming-with-mediamtx)
1. [Testing](#testing)
1. [Kernel offload](#kernel-offload)

## Concepts

SRT (Secure Reliable Transport) is the UDP transport of live video contribution. an encoder in the
field sends an MPEG-TS stream over a lossy network. SRT guarantees retransmits within a fixed
latency budget and encrypts the stream with a passphrase shared by the two ends. The caller names
the stream in the *stream ID* of the SRT handshake, by convention `#!::r=<stream>,m=publish` to
publish a stream and `#!::r=<stream>,m=request` to read one. 

Critically, STUNner makes sure to route every streams to the same session to the same SRT media
server. This makes sure media servers can be freely scaled to as many server pods as required
behind one address: STUNner spreads the connections over the pods but it makes sure that the
encoder publishing `live/van7` and the decoder reading `live/van7` land on one pod. STUNner reads
the stream ID in the handshake and sends every connection naming the same stream to the same pod:

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
stream ID that names no stream (`#!::m=publish`), a reader with no backend, and a stream over the
tier's limit of parallel sessions are rejected in the handshake. A stream ID outside the `#!::`
syntax names the stream with the whole string, so use the `#!::` syntax with media servers that
take several syntaxes: MediaMTX's own `publish:live/van7` and `read:live/van7` name two different
streams for STUNner.

## Streaming with MediaMTX

The below manifests deploy three MediaMTX replicas taking SRT on UDP port 8890 and accepting any
stream encrypted with the passphrase `my-srt-passphrase` (MediaMTX takes its settings from `MTX_`
environment variables), an SRT Gateway on the public UDP port 9000, and the UDPRoute between the
two.

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

STUNner can relay the streams of an SRT Gateway in the Linux kernel instead of in user space: once
a caller's handshake has completed, its flow to the media server is relayed in the kernel, one
offloaded flow per caller. The handshakes and the shutdowns stay in user space, so a repeated
conclusion is still answered and a shutdown still ends the flow. See the [kernel
offload](PREMIUM_REFERENCE.md#kernel-offload) reference for the offload limit of your tier, the
settings and the monitoring.
