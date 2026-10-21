# mediactl: A media source and sink for testing media gateways

`mediactl` sends packets and measures what comes back, or answers and echoes, over the protocols
STUNner relays: SIP calls with RTP media, plain UDP and TCP streams (RTSP interleaved over TCP is a
TCP stream), streams relayed through a TURN server, and SRT streams. The payload is padding: the
tool tests the path, not the codecs. It runs outside the cluster and measures through it.

## Installation

Download `mediactl` from the [STUNner releases](https://github.com/l7mp/stunner/releases), or
install it with the Go toolchain:

```console
go install github.com/l7mp/stunner/v2/cmd/mediactl@latest
```

The container image is `docker.io/l7mp/mediactl`; by default it answers SIP calls on UDP port 5060.

## Usage

| Command | What it does |
|:--|:--|
| `mediactl sip call <sip-uri>` | Dial a call, stream packets over its RTP media and measure the echo. `--username` and `--password` answer a digest challenge. |
| `mediactl sip answer [<ip>][:<port>]` | Answer every call and echo its media (default port 5060). `--registrar` takes REGISTERs too. |
| `mediactl udp send <host>:<port>` | Send packets to a UDP peer and measure the echo. |
| `mediactl udp echo [<ip>]:<port>` | Echo every UDP client, report every flow. |
| `mediactl tcp send <host>:<port>` | Stream to a TCP peer and measure the echo. |
| `mediactl tcp echo [<ip>]:<port>` | Echo every TCP connection, report every flow. |
| `mediactl turn send <turn-uri> <host>:<port>` | Send packets to the peer through a TURN relay and measure the echo. `--username` and `--password` authenticate, `--tcp` relays over an RFC 6062 TCP connection, `--insecure` skips the certificate check over TLS and DTLS. |
| `mediactl srt publish <host>:<port> <stream>` | Publish a stream (stream ID `#!::r=<stream>,m=publish`). |
| `mediactl srt read <host>:<port> <stream>` | Read a stream and measure what arrives (stream ID `#!::r=<stream>,m=request`). |
| `mediactl srt send <host>:<port> <stream>` | Send packets on a stream and measure the echo (stream ID `#!::r=<stream>`). |
| `mediactl srt serve [<ip>]:<port>` | Pass every published stream to the readers of the same stream, echo the other callers. |

The SRT commands take `--passphrase` for the stream encryption.

### Measuring

A sender sends `--length` (160) bytes `-k/--send-rate` (50) times a second until `-t/--timeout`
(default: until Ctrl-C); the defaults are the 20 ms frames of a G.711 call. Over UDP, TURN, SRT
and the RTP of a SIP call every packet starts with a sequence number and a timestamp, so the
receiving side measures the loss and an echoing sender the round trip; over TCP the send rate is
the rate of the writes and only the bytes count. A sender logs the rates of every second and a
summary, but every side reports only what it measures itself: an echoing sender what
it sent, what came back, the loss and the round trip, a publisher what it sent, a reader what it
received, and the echo servers every flow, once it ends.

| Option | Effect |
|:--|:--|
| `-P/--parallel <n>` | Run `n` streams at once; every stream and the total is reported. |
| `-o/--output json` | Print the results of every stream and the total on the standard output as JSON. |
| `--max-loss <percent>` | Fail when more than this percent of the packets is lost. |
| `--max-rtt <duration>` | Fail when the average round trip is longer. |
| `--ts <file>` | Send the bytes of a file, an MPEG-TS file say, instead of measured packets, from the start again at its end; only the bitrate is measured. |

A sender fails (exits non-zero) when nothing came back that should have, or a threshold is
crossed. `-l/--log` sets the log level as in `stunnerd` (`all:INFO` by default), `-v/--verbose`
is `-l all:DEBUG`. Every flag can also be set in a config file (`--config`, YAML, JSON or TOML) or
in an environment variable: `MEDIACTL_TIMEOUT=10s` sets `--timeout`.

### Addressing Gateways in Kubernetes

Instead of an address, a sender takes `k8s://<namespace>/<gateway>[/<listener>]`: the public address
of a listener of a STUNner Gateway, read from the dataplane config the operator serves, through
your kubeconfig. The listener may be left out when the Gateway has one. A TURN listener brings its
TURN URI and the credentials of its auth policy, ephemeral ones included. A TURN peer may be
`k8s://<namespace>/<service>:<port>`, the first ready pod of a Service at the target port of the
Service port of that number or name: TURN relays to pod addresses. An empty namespace
(`k8s:///...`) is the namespace of the kubeconfig context.
The standard kubeconfig flags (`--kubeconfig`, `--context`, `-n`, ...) and the config discovery
flags of `stunnerctl` apply.

Ten seconds of TURN through the UDP listener of a Gateway to an echo pod, no credentials typed:

```console
mediactl turn send -t 10s k8s://stunner/udp-gateway/udp-listener k8s://stunner/udp-echo:9001
```

A SIP call through a SIP Gateway (the user part of the URI is `--to`, `echo` by default; see the
[SIP echo tutorial](../examples/sip-echo/README.md)):

```console
mediactl sip call -t 10s k8s://stunner/sip-gateway/sip
```

Publish and read a stream through an SRT Gateway with one listener:

```console
mediactl srt publish k8s://streaming/srt-gateway live/test &
mediactl srt read -t 10s k8s://streaming/srt-gateway live/test
```

A load test of the stream cap: six SRT echo streams at 2 Mbit/s each, JSON for the CI, failing
above 1% loss (the sixth stream is over the cap, so this one fails by design):

```console
mediactl srt send -t 30s -P 6 --length 1316 -k 190 -o json --max-loss 1 k8s://streaming/srt-gateway live/load
```

Real media through the Gateway to MediaMTX, the test pattern of a file:

```console
mediactl srt publish -t 60s --ts testsrc.ts --length 1316 -k 400 k8s://streaming/srt-gateway live/test
```

An RTSP or any other TCP service behind a plain TCP listener, measured at 1 Mbit/s:

```console
mediactl tcp send -t 10s --length 1250 -k 100 k8s://cameras/rtsp-gateway
```

Without Kubernetes, the same commands take plain addresses:

```console
mediactl turn send -t 10s --username user-1 --password pass-1 "turn:${GATEWAY_IP}:3478?transport=udp" ${ECHO_POD_IP}:9001
mediactl sip call -t 10s sip:echo@${SIP_IP}:5060
```

## The library

Every sender and answerer is also a Go library for writing tests,
[`pkg/mediactl`](https://pkg.go.dev/github.com/l7mp/stunner/v2/pkg/mediactl). Its model: every
protocol is a dialer of the `net.Conn`s of a sender and a `net.Listener` of the `net.Conn`s of an
answerer, and one stream engine runs over any of them. `mediactl.Stream(ctx, conn, config)` sends
and measures, `mediactl.Echo` and `mediactl.Serve` answer, and the protocols come from the standard
library and the subpackages:

| Protocol | Dialer | Listener |
|:--|:--|:--|
| UDP, TCP | `net.Dialer` | `pkg/udp`, `net.Listen` |
| SIP and RTP | `sip.Phone` (`DialContext` calls a SIP URI) | `sip.Phone` (`Accept` returns the next answered call) |
| SRT | `srt.Dialer` (a stream and a mode) | `srt.Listen` (publishers to readers inside, the rest echoed) |
| TURN | `turn.Dialer` (the TURN server; network `tcp` for RFC 6062) | |

A SIP call is a `net.Conn` of its media: `rtp.Conn` frames its writes as RTP packets and returns
the payloads of the RTP packets it reads, with an RTCP sender report every second, so the same
stream engine measures a call. `k8s.Resolver` resolves the `k8s://` addresses.

## License

Copyright 2021-2026 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
