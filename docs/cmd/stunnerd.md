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
* TURN/UDP listener CPU scaling.
* Two authentication modes via the long-term STUN/TURN credential mechanism: `static` using a
  static username/password pair, and `ephemeral` with dynamically generated time-scoped
  credentials.

## Getting Started

### Installation

As easy as with any Go program.

```console
cd stunner
go build -o stunnerd cmd/stunnerd/main.go
```

### Usage

The below command will open a `stunnerd` UDP listener at `127.0.0.1:5000`, set `static` authentication using the username/password pair `user1/passwrd1`, and raise the debug level to the maximum and set the logging format to JSON.

```console
./stunnerd --log=all:TRACE --log-format=json turn://user1:passwd1@127.0.0.1:5000
```

Alternatively, run `stunnerd` in verbose mode with the config file taken from `cmd/stunnerd/stunnerd.conf`. Adding the flag `-w` will enable watch mode.

```console
./stunnerd -v -w -c file://cmd/stunnerd/stunnerd.conf
```

A few flags override the config, whatever its origin: `--license` takes the license config as the single-line JSON `licensegen -o json` writes (`{"license_config":{"key":"...","hmac":"..."}}`) or just the inner `{"key":"...","hmac":"..."}`, and `--offload` sets the offload engine (`none`, `xdp`, `tc` or `auto`) on every interface. On SIGTERM, `stunnerd` fails its readiness check and waits for the live sessions to end; `--no-graceful-shutdown` makes it close them and exit at once.

```console
./stunnerd -c file://cmd/stunnerd/stunnerd.conf --license '{"key":"...","hmac":"..."}' --offload tc
```

Type `./stunnerd -h` to get a short description of the supported command line arguments.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes and you should be good to go. Or better yet, [install](/docs/INSTALL.md) the STUNner Kubernetes gateway operator that will readily manage the `stunnerd` pods for each Gateway you create.

## Configuration

A `stunnerd` configuration (API version `v2`) is built from three kinds of objects: a **listener** accepts client connections on a transport (`UDP`, `TCP`, `TLS`, `DTLS`, or `STDIN`) and hands every accepted connection down the chain of servers, a **server** processes a client connection (`turn`, `l4`) and hands it to one of a set of clusters, and a **cluster** is a named set of peer `endpoints`, the `routing_policy` over them, and the transport that reaches them. The routing policy is `FILTER` (the default) for a cluster behind a `turn` server, which admits the peers the endpoints contain, and a load-balancing policy, `ROUND_ROBIN`, for a cluster behind an `l4` server, which dials the single-port endpoints in turn; a server skips a cluster of the other kind.

Using the below configuration, `stunnerd` opens 4 listeners: two for unencrypted connections at UDP/3478 and TCP/3478 and two for encrypted connections at TLS/3479 and DTLS/3479, all served by the same TURN server. The daemon uses `ephemeral` authentication, with the shared secret taken from the environment variable `$STUNNER_SHARED_SECRET` during initialization, and it relays to any peer, over UDP and TCP (for [RFC 6062](https://tools.ietf.org/html/rfc6062) TCP allocations). The listeners bind every interface, and the relay address the clusters advertise is taken from the `$STUNNER_ADDR` environment variable.

``` yaml
version: v2
admin:
  name: my-stunnerd
  loglevel: all:DEBUG
auth:
  type: ephemeral
  realm: "my-realm.example.com"
  credentials:
    secret: $STUNNER_SHARED_SECRET
listeners:
  - name: stunnerd-udp
    protocol: UDP
    port: 3478
    servers: [turn-server]
  - name: stunnerd-tcp
    protocol: TCP
    port: 3478
    servers: [turn-server]
  - name: stunnerd-tls
    protocol: TLS
    port: 3479
    cert: "<base64 PEM certificate>"
    key: "<base64 PEM key>"
    servers: [turn-server]
  - name: stunnerd-dtls
    protocol: DTLS
    port: 3479
    cert: "<base64 PEM certificate>"
    key: "<base64 PEM key>"
    servers: [turn-server]
servers:
  - name: turn-server
    type: turn
    clusters:
      - open-udp
      - open-tcp
clusters:
  - name: open-udp
    protocol: UDP
    endpoints:
      - "0.0.0.0/0"
      - "::/0"
    addresses: ["$STUNNER_ADDR"]
  - name: open-tcp
    protocol: TCP
    endpoints:
      - "0.0.0.0/0"
      - "::/0"
    addresses: ["$STUNNER_ADDR"]
```

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

The `--sni` and `--insecure` flags apply to the TLS/DTLS transports (note that `--insecure` has no `-i` shorthand, which belongs to `--id`). A tunnel is quiet by default (`all:WARN`) unless a log level is set. Internally, tunnel mode renders a `UDP`, `TCP` or `STDIN` listener feeding an `l4` server, a single cluster holding the peer and tunnelled through the TURN server, and runs the normal reconcile machinery on the result: there is no separate tunnel datapath. Logs go to stderr, so a stdin/stdout tunnel composes cleanly in shell pipelines; the process exits when the stdin flow ends.

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
