# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway dataplane.

The daemon supports two basic modes. For quick tests `stunnerd` can be configured as a TURN server
by specifying a TURN network URI on the command line. For more complex scenarios, and especially
for use in a Kubernetes cluster, `stunnerd` can take configuration from a config origin, which can
either be a config file or from a remote server reached over WebSocket. In addition, `stunnerd`
implements a watch-mode, so that it can actively monitor the config origin for updates and
automatically reconcile the TURN server to any new configuration. This mode is intended for use
with the [STUNner Kubernetes gateway operator](https://github.com/l7mp/stunner-gateway-operator):
the operator watches the Kubernetes [Gateway API](https://gateway-api.sigs.k8s.io) resources,
renders the active control plane configuration per each `stunnerd` pod and dynamically updates the
dataplane using STUNner's config discovery service.

## Features

* Full Kubernetes integration for quick installation into any hosted or on-prem Kubernetes cluster.
* Dynamic reconciliation by enabling config-file watch mode.
* [RFC 5389](https://tools.ietf.org/html/rfc5389): Session Traversal Utilities for NAT (STUN)
* [RFC 8656](https://tools.ietf.org/html/rfc8656): Traversal Using Relays around NAT (TURN)
* [RFC 6062](https://tools.ietf.org/html/rfc6062): Traversal Using Relays around NAT (TURN)
  Extensions for TCP Allocations
* TURN transport over UDP, TCP, TLS/TCP and DTLS/UDP.
* Plain UDP/TCP listeners that relay raw client flows to a preconfigured peer, and a tunnel
  mode that drives them from the command line (the successor of the retired `turncat` tool).
* TURN/UDP listener CPU scaling.
* Two authentication modes via the long-term STUN/TURN credential mechanism: `static` using a
  static username/password pair, and `ephemeral` with dynamically generated time-scoped
  credentials.
* Peer port range filtering.

## Getting Started

### Installation

As easy as with any Go program.
```console
cd stunner
go build -o stunnerd ./cmd/stunnerd
```

### Usage

The below command will open a `stunnerd` UDP listener at `127.0.0.1:5000`, set `static` authentication using the username/password pair `user1/passwrd1`, and raise the debug level to the maximum and set the logging format to JSON.

```console
./stunnerd --log=all:TRACE --log-format=json turn://user1:passwd1@127.0.0.1:5000
```

Alternatively, run `stunnerd` in verbose mode with the config file taken from `cmd/stunnerd/stunnerd.conf`. Adding the flag `-w` will enable watch mode.

```console
./stunnerd -v -w -c cmd/stunnerd/stunnerd.conf
```

Type `./stunnerd -h` to get a short description of the supported command line arguments.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes and you should be good to go. Or better yet, [install](/docs/INSTALL.md) the STUNner Kubernetes gateway operator that will readily manage the `stunnerd` pods for each Gateway you create.

## Configuration

Using the below configuration, `stunnerd` will open 4 STUNner listeners: two for accepting unencrypted connections at UDP/3478 and TCP/3478, and two for encrypted connections at TLS/TCP/3479 and DTLS/UDP/3479. The daemon will use `ephemeral` authentication, with the shared secret taken from the environment variable `$STUNNER_SHARED_SECRET` during initialization. The relay address will be taken from the `$STUNNER_ADDR` environment variable.

``` yaml
version: v1alpha1
admin:
  name: my-stunnerd
  logLevel: all:DEBUG
  realm: "my-realm.example.com"
static:
  auth:
    type: ephemeral
    credentials:
      secret: $STUNNER_SHARED_SECRET
  listeners:
    - name: stunnerd-udp
      address: "$STUNNER_ADDR"
      protocol: turn-udp
      port: 3478
    - name: stunnerd-tcp
      address: "$STUNNER_ADDR"
      protocol: turn-tcp
      port: 3478
    - name: stunnerd-tls
      address: "$STUNNER_ADDR"
      protocol: turn-tls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.key"
    - name: stunnerd-dtls
      address: "$STUNNER_ADDR"
      protocol: turn-dtls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.key"
```

### Relay addresses

A listener's `address` is the relayed transport address `stunnerd` advertises to clients. A dual-stack deployment needs one per address family: use `addresses` to hand `stunnerd` a list (typically the pod's IPs, one per family, via `$STUNNER_ADDRS`) and it will advertise the relay address matching each client's own address family. Likewise, `public_address`/`public_port` name the single address clients reach the listener at, while `public_addresses` carries one entry per family. IPv6 addresses are written unbracketed in these fields.

``` yaml
    - name: stunnerd-udp
      address: "$STUNNER_ADDR"
      addresses: ["$STUNNER_ADDRS"]
      protocol: turn-udp
      port: 3478
      public_address: "1.2.3.4"
      public_addresses: ["1.2.3.4", "2001:db8::1"]
```

### TURN relay clusters

A cluster with a plain protocol (`udp`, `tcp`) relays a client's traffic directly to the peers admitted by its `endpoints`. A cluster with a TURN protocol (`turn-udp`, `turn-tcp`, `turn-tls`, `turn-dtls`) instead relays the traffic through an upstream TURN server named by its `turnServer` block.

``` yaml
  clusters:
    - name: upstream-turn
      protocol: turn-tcp
      turnServer:
        address: turn.example.com
        port: 3478
        auth:
          type: static
          credentials:
            username: user
            password: pass
```

The optional `auth` block tells `stunnerd` how to authenticate to the upstream server, with the exact same syntax it uses for its own clients: type `static` takes a fixed `username`/`password` pair, type `ephemeral` takes a `secret` from which a time-limited credential is generated per allocation. Omit the block for an upstream server that takes no credentials. For the `turn-tls` and `turn-dtls` transports, `insecure: true` skips the verification of the upstream server's TLS certificate, and `sni` overrides the server name used for certificate verification when it differs from `address` (say, when the server is addressed by IP).

### Plain listeners

A listener with a plain protocol (`UDP`, `TCP`) serves raw client flows instead of TURN sessions and relays every flow to the single static peer named by its `peer_addr` field (`host:port`, the host may be a DNS name, resolved per flow). Raw flows carry no in-band peer address, which is why the peer is pinned in the listener config. The special `STDIN` protocol relays a single flow between the process stdin/stdout pair and the peer, which is what tunnel mode uses under the hood.

``` yaml
  listeners:
    - name: plain-udp
      protocol: UDP
      port: 5000
      peer_addr: "media-server.media.svc:5001"
      flow_timeout: 5m
      routes:
        - media-plane
```

Peer admission is unchanged: a flow is only served if one of the listener's routed clusters admits the resolved peer address, and when the listener routes to a TURN protocol cluster the flow is relayed through the upstream TURN server (tunnel mode). Flows quiet for `flow_timeout` (default 5m) in both directions are torn down; fresh client traffic re-creates them. Note that plain listeners authenticate nobody, so they are never rendered by the Kubernetes gateway operator (the Gateway API cannot express a peer address either); they are available from static config files and the tunnel CLI only. See the [security notes](../SECURITY.md) before exposing one.

### Listener and cluster combinations

A listener's protocol and the protocol of the cluster it routes to together decide how a session is relayed, and whether the [TURN offload](../PREMIUM_REFERENCE.md#turn-offload) can accelerate it. The offload engines accelerate exactly one shape: a leg between a TURN client and a TURN server, where plaintext ChannelData arrives on one side and raw traffic leaves on the other. A leg that carries raw traffic in both directions, ChannelData in both directions, or encrypted ChannelData, stays in user space.

| Listener protocol | Cluster protocol | Relaying | Offload (Premium)|
|---|---|---|---|
| `turn-*` | `udp` | TURN relaying | yes, on a `turn-udp` listener |
| `turn-*` | `tcp` | RFC 6062 relayed TCP connections to the peer | no |
| `turn-*` | `turn-*` | TURN relay chaining | no |
| `udp`, `tcp` | `udp`, `tcp` | direct relaying to the listener's pinned peer | no |
| `udp` | `turn-udp` | datagram tunneling | yes |
| `tcp` | `turn-udp` | stream tunneling | no |
| `udp`, `tcp` | `turn-tcp`, `turn-tls`, `turn-dtls` | stream tunneling | no |
| `stdin` | any | stdin/stdout tunneling | no |

A listener routes either to plain clusters, any number of them, or to a single TURN cluster. Relaying through different transports is not implemented and rejected on reconciliation.

## Tunnel mode

The positional argument count selects `stunnerd`'s mode: no arguments run the dataplane daemon from the config origin (`-c`), a single TURN listener URI runs a standalone TURN server with a default configuration, and three arguments select tunnel mode, which tunnels a local client socket, or the stdin/stdout pair, through a TURN server to a fixed peer. Tunnel mode is the successor of the retired `turncat` utility and keeps its command line shape:

```console
stunnerd [options] <client-addr> <turn-server-addr> <peer-addr>
```

where `client-addr` is `udp://<addr>:<port>`, `tcp://<addr>:<port>`, or `-` for a stdin/stdout tunnel; `turn-server-addr` is either a TURN URI (`turn://<auth>@<addr>:<port>[?transport=udp|tcp|tls|dtls]`, the `<auth>` userinfo being a `username:password` pair for static authentication or a bare shared secret for ephemeral credentials) or the `k8s://<gateway-namespace>/<gateway-name>:<listener>` meta-URI that discovers the running config of a STUNner gateway listener from the cluster (kubeconfig flags apply); and `peer-addr` is `udp://<addr>:<port>`, `tcp://<addr>:<port>`, or the `k8s://<namespace>/<service>:<port-name-or-number>` meta-URI naming a Kubernetes Service port, resolved once at startup into the Service's cluster IP and port with the peer transport taken from the Service port spec.

The below opens a local UDP tunnel endpoint at port 5000 that relays through the `udp-listener` listener of the `udp-gateway` gateway in the `stunner` namespace to a media server service, without ever looking up the service's cluster IP by hand:

```console
stunnerd udp://127.0.0.1:5000 k8s://stunner/udp-gateway:udp-listener k8s://media/media-server:rtp
```

The `--sni` and `--insecure` flags apply to the TLS/DTLS transports (note that `--insecure` has no `-i` shorthand, which belongs to `--id`). A tunnel is quiet by default (`all:WARN`) unless a log level is set. Internally, tunnel mode renders a plain (or `STDIN`) listener pinned to the peer plus a single TURN protocol cluster naming the server, and runs the normal reconcile machinery on the result: there is no separate tunnel datapath. Logs go to stderr, so a stdin/stdout tunnel composes cleanly in shell pipelines; the process exits when the stdin flow ends.

## Performance optimization

STUNner can run multiple parallel readloops for TURN/UDP listeners, which allows it to scale to practically any number of CPUs and brings massive performance improvements for UDP workloads. This can be achieved by creating a configurable number of UDP readloop threads over the same TURN listener. The kernel will load-balance allocations across the readloops per the IP 5-tuple and so the same allocation will always stay at the same CPU, which is important for correct TURN operations.

The feature is exposed via the command line flag `--udp-thread-num=<THREAD_NUMBER>`. The below starts `stunnerd` watching the config file in `/etc/stunnerd/stunnerd.conf` using 32 parallel UDP readloops (the default is 16).

``` sh
./stunnerd -w -c /etc/stunnerd/stunnerd.conf --udp-thread-num=32
```

## License

Copyright 2021-2026 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
