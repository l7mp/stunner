# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway dataplane.

The daemon supports two basic modes. For quick tests `stunnerd` can be configured as a TURN server
by specifying a TURN network URI on the command line. For more complex scenarios, and especially
for use in a Kubernetes cluster, `stunnerd` can take configuration from a config file. In addition,
`stunnerd` implements a watch-mode, so that it can actively monitor the config file for updates
and, once the config file has changed, automatically reconcile the TURN server to the new
configuration. This mode is intended for use with the [STUNner Kubernetes gateway
operator](https://github.com/l7mp/stunner-gateway-operator): the operator watches the Kubernetes
[Gateway API](https://gateway-api.sigs.k8s.io) resources and renders the active control plane
configuration into a ConfigMap, which is then mapped into the `stunnerd` pod's filesystem so that
the daemon can pick up the latest configuration using the watch mode.

## Features

* Full Kubernetes integration for quick installation into any hosted or on-prem Kubernetes cluster.
* Dynamic reconciliation by enabling config-file watch mode.
* [RFC 5389](https://tools.ietf.org/html/rfc5389): Session Traversal Utilities for NAT (STUN)
* [RFC 8656](https://tools.ietf.org/html/rfc8656): Traversal Using Relays around NAT (TURN)
* [RFC 6062](https://tools.ietf.org/html/rfc6062): Traversal Using Relays around NAT (TURN)
  Extensions for TCP Allocations
* TURN transport over UDP, TCP, TLS/TCP and DTLS/UDP.
* Two authentication modes via the long-term STUN/TURN credential mechanism: `plaintext` using a
  static username/password pair, and `longterm` with dynamically generated time-scoped credentials.

## Getting Started

### Installation

As easy as with any Go program.
```console
cd stunner
go build -o stunnerd cmd/stunnerd/main.go
```

### Usage

The below command will open a `stunnerd` UDP listener at `127.0.0.1:5000`, set `plaintext`
authentication using the username/password pair `user1/passwrd1`, and raises the debug level to the
maximum.

```console
./stunnerd --log=all:TRACE turn://user1:passwd1@127.0.0.1:5000
```

Alternatively, run `stunnerd` in verbose mode with the config file taken from
`cmd/stunnerd/stunnerd.conf`. Adding the flag `-w` will enable watch mode.

```console
$ ./stunnerd -v -w -c cmd/stunnerd/stunnerd.conf
```

Type `./stunnerd` to see a short description of the command line arguments supported by `stunnerd`.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container
image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes and you should be good
to go.

## Configuration

Using the below configuration, `stunnerd` will open 4 STUNner listeners: two for accepting
unencrypted connections at UDP/3478 and TCP/3478, and two for encrypted connections at TLS/TCP/3479
and DTLS/UDP/3479. For easier debugging, the port for the transport relay connections opened by
`stunnerd` will be taken from [10000:19999] for the UDP listener, [20000:29999] for the TCP
listener, etc.  The daemon will use `longterm` authentication, with the shared secret read from the
environment variable `$STUNNER_SHARED_SECRET` during initialization. The relay address is taken
from the `$STUNNER_ADDR` environment variable.

``` yaml
version: v1alpha1
admin:
  name: my-stunnerd
  logLevel: all:DEBUG
  realm: "my-realm.example.com"
static:
  auth:
    type: longterm
    credentials:
      secret: $STUNNER_SHARED_SECRET
  listeners:
    - name: stunnerd-udp
      address: "$STUNNER_ADDR"
      protocol: udp
      port: 3478
      minPort: 10000
      maxPort: 19999
    - name: stunnerd-tcp
      address: "$STUNNER_ADDR"
      protocol: tcp
      port: 3478
      minPort: 20000
      maxPort: 29999
    - name: stunnerd-tls
      protocol: tls
      port: 3479
      minPort: 30000
      maxPort: 39999
      cert: "my-cert.cert"
      key: "my-key.key"
    - name: stunnerd-dtls
      protocol: dtls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.key"
      minPort: 40000
      maxPort: 49999
```

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
