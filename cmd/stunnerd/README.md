# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway dataplane.

The daemon supports two basic modes. For quick tests `stunnerd` can be configured as a TURN server
by specifying a TURN network URI on the command line. For more complex scenarios, and especially
for use in a Kubernetes cluster, `stunnerd` can take configuration from a config origin, that can
be any file or a remote server. In addition, `stunnerd` implements a watch-mode, so that it can
actively monitor the config origin for updates and automatically reconcile the TURN server to any
new configuration. This mode is intended for use with the [STUNner Kubernetes gateway
operator](https://github.com/l7mp/stunner-gateway-operator): the operator watches the Kubernetes
[Gateway API](https://gateway-api.sigs.k8s.io) resources, renders the active control plane
configuration per each `stunnerd` pod and dynamically updates the dataplane using STUNner's config
discovery service.

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
* Peer port range filtering.

## Getting Started

### Installation

As easy as with any Go program.
```console
cd stunner
go build -o stunnerd cmd/stunnerd/main.go
```

### Usage

The below command will open a `stunnerd` UDP listener at `127.0.0.1:5000`, set `static` authentication using the username/password pair `user1/passwrd1`, and raise the debug level to the maximum.

```console
./stunnerd --log=all:TRACE turn://user1:passwd1@127.0.0.1:5000
```

Alternatively, run `stunnerd` in verbose mode with the config file taken from `cmd/stunnerd/stunnerd.conf`. Adding the flag `-w` will enable watch mode.

```console
./stunnerd -v -w -c cmd/stunnerd/stunnerd.conf
```

Type `./stunnerd` to see a short description of the most important command line arguments.

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
      protocol: udp
      port: 3478
    - name: stunnerd-tcp
      address: "$STUNNER_ADDR"
      protocol: tcp
      port: 3478
    - name: stunnerd-tls
      address: "$STUNNER_ADDR"
      protocol: tls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.key"
    - name: stunnerd-dtls
      address: "$STUNNER_ADDR"
      protocol: dtls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.key"
```

## Advanced features

### TURN/UDP listener CPU scaling

STUNner can run multiple parallel readloops for TURN/UDP listeners, which allows it to scale to practically any number of CPUs and brings massive performance improvements for UDP workloads. This can be achieved by creating a configurable number of UDP server sockets using the `SO_REUSEPORT` socket option and spawning a separate goroutine to run a parallel readloop per each listener. The kernel will load-balance allocations across the sockets/readloops per the IP 5-tuple, thus the same allocation will always stay at the same CPU. This is important for correct TURN operations.

The feature is exposed via the command line flag `--udp-thread-num=<THREAD_NUMBER>`. The below starts `stunnerd` watching the config file in `/etc/stunnerd/stunnerd.conf` using 32 parallel UDP readloops (the default is 16).

``` sh
./stunnerd -w -c /etc/stunnerd/stunnerd.conf --udp-thread-num=32
```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
