# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway dataplane.

The daemon supports two basic modes. For quick tests `stunnerd` can be configured as a TURN server by specifying a TURN network URI on the command line. For more complex scenarios, and especially for use in a Kubernetes cluster, `stunnerd` can take configuration from a config origin, which can either be a config file or from a remote Config Discovery Service (CDS) server reached over WebSocket. In addition, `stunnerd` implements a watch-mode, so that it can actively monitor the config origin for updates and automatically reconcile the TURN server to any new configuration. This mode is intended for use with the [STUNner Kubernetes gateway operator](https://github.com/l7mp/stunner-gateway-operator): the operator watches the Kubernetes [Gateway API](https://gateway-api.sigs.k8s.io) resources, renders the active control plane configuration per each `stunnerd` pod and dynamically updates the dataplane using STUNner's config discovery service.

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

Installation is As easy as with any Go program.

```console
cd stunner
go build -o stunnerd cmd/stunnerd/main.go
```

## Usage

To quickly fire up a TURN server or a TURN proxy, use `stunnerd` via the command line configuration interface.  The number of positional arguments selects the operation mode: a single TURN URI runs a standalone TURN server with the default configuration and three arguments enable the tunnel mode. 

In the TURN server mode, provide on single TURN URI:

```console
stunnerd [options] <turn-URI>
```

The below command will open a TURN server listener at `127.0.0.1:5000` over UDP, set `static` authentication using the username/password pair `user1/passwrd1`, and set the maximum debug level JSON logging format.

```console
./stunnerd --log=all:TRACE --log-format=json turn://user1:passwd1@127.0.0.1:5000
```

The tunnel mode tunnels a local client socket through a TURN server to a remote peer. Tunnel mode is the successor of the retired `turncat` utility. The general CLI shape for the tunnel mode invocation is as below:

```console
stunnerd [options] <client-addr> <turn-URI> <peer-addr>
```

Here, `client-addr` configures the client socket (e.g., `udp://<addr>:<port>`, `tcp://<addr>:<port>`, or `-` for a stdin/stdout tunnel), `turn-URI` is a TURN server URI (`turn://<auth>@<addr>:<port>[?transport=udp|tcp|tls|dtls]`, where the `<auth>` is a `username:password` pair for static authentication or a bare shared secret for ephemeral credentials); and `peer-addr` is the remote peer to connect to via the TURN tunnel (e.g., `udp://<addr>:<port>` or `tcp://<addr>:<port>`).

The below opens a local UDP tunnel endpoint at port 5000 that relays through the TURN server at `1.2.3.4:3478` to a media server at `10.0.0.1:9000`:

```console
stunnerd udp://127.0.0.1:5000 turn://user:pass@1.2.3.4:3478 udp://10.0.0.1:9000
```

The `--sni` and `--insecure` flags apply to the TLS/DTLS transports (note that `--insecure` has no `-i` shorthand, which belongs to `--id`). A tunnel is quiet by default (`all:WARN`) unless a log level is set. Internally, tunnel mode renders a `UDP`, `TCP` or `STDIN` listener feeding an `L4` server, a single cluster holding the peer and tunnelled through the TURN server, presenting the credentials of an authentication policy named `upstream`, and runs the normal reconcile machinery on the result: there is no separate tunnel datapath. Logs go to stderr, so a stdin/stdout tunnel composes cleanly in shell pipelines; the process exits when the stdin flow ends.

Type `./stunnerd -h` to get a short description of the supported command line arguments.

## Configuration

Alternatively, configure `stunnerd` with a config file. For instance, the below reads the config file from `cmd/stunnerd/stunnerd.conf` and selects verbose mode, while adding the flag `-w` will enable watch mode.

```console
./stunnerd -v -w -c file://cmd/stunnerd/stunnerd.conf
```

Configuration files for `stunnerd`, now at API version `v2`, are built from five config objects:

- Listeners accept client connections (`UDP`, `TCP`, `TLS`, `DTLS`, `STDIN` or `FIFO`) and hand them to a chain of servers for processing.
- Servers (`TURN`, `L4`, `SIP` or `RTP`) serve connections through a set of clusters.
- Clusters define the endpoints reachable via the cluster (addresses or domains, with an optional port) plus a routing policy and the transport protocol to reach them.
- Filters are ordered list of allow/deny rules on addresses and ports. Filters can be invoked at various points in the processing pipeline: listener filters are applied at the ingress, cluster filters at the egress, etc. The first matching rule decides and unmatched traffic passes; a rule without an `ip_block` matches every address, so end an allow-list with a bare `- action: DENY`.
- Auth configs are named authentication policies (`static`, `ephemeral` or `none`).

A few flags override the config, whatever its origin: `--license` takes the license config as the single-line JSON (e.g., `{"key":"...","hmac":"..."}`); `--offload` sets the offload engine (`none`, `xdp`, `tc` or `auto`) on every interface; and `--no-graceful-shutdown` disables graceful shutdowns: normally on being closed `stunnerd` waits for live sessions to end, which minimizes disruptions to clients but may take minutes or hours, which this flag disables and lets `stunnerd` to close immediately.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes and you should be good to go. Or better yet, [install](/docs/INSTALL.md) the STUNner Kubernetes gateway operator that will readily manage the `stunnerd` pods for each Gateway you create.

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
