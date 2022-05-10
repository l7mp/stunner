# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway service.

## Getting Started

Installation is as easy as it gets:

```console
$ cd stunner
$ go run utils/stunnerd/main.go --log=all:TRACE turn://user1:passwd1@127.0.0.1:3478
```

## Features

* Full Kubernetes integration for quick installation into any hosted or on-prem Kubernetes cluster
  and easy Day-2 operations.
* [RFC 5389](https://tools.ietf.org/html/rfc5389): Session Traversal Utilities for NAT (STUN)
* [RFC 8656](https://tools.ietf.org/html/rfc8656): Traversal Using Relays around NAT (TURN)
* [RFC 6062](https://tools.ietf.org/html/rfc6062): Traversal Using Relays around NAT (TURN)
  Extensions for TCP Allocations
* TURN transport over UDP, TCP, TLS/TCP and DTLS/UDP.
* Two authentication modes via long-term STUN/TURN credential mechanism: `plaintext` using a static
  username/password pair, and `longterm` with dynamically generated time-scoped credentials.

## Usage

Open a `stunnerd` UDP listener service at `127.0.0.1:5000` with the username/password pair
`user1/passwrd1` for `plaintext` authentication using the long-term STUN/TURN credential mechanism
and set the maximum debug level.

```console
$ ./stunnerd --log=all:TRACE turn://user1:passwd1@127.0.0.1:3478
```

Alternatively, run `stunnerd` in verbose mode with the configuration file taken from
`utils/stunnerd/stunnerd.conf`.

```console
$ ./stunnerd -v -c utils/stunnerd/stunnerd.conf
```

Type `./stunnerd` to see a short description of the command line arguments supported by `stunnerd`.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container
image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes using one of the
[installation modes](/doc/INSTALL.md) and you should be good to go.

## Configuration

The below configuration will open a 4 STUNner listeners, two for unencrypted connections at
UDP/3478 and TCP/3478, and two encrypted connections at TLS/TCP/3479 and DTLS/UDP/3479. The daemon
will use `longterm` authentication, using a shared secret read from the environment variable
`$STUNNER_SHARED_SECRET`. The relay address 

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
      key: "my-key.cert"
    - name: stunnerd-dtls
      protocol: dtls
      port: 3479
      cert: "my-cert.cert"
      key: "my-key.cert"
      minPort: 40000
      maxPort: 49999
```

Note that changing any configuration parameter (e.g., updating the shared secret) currently
requires restarting `stunnerd`. We aim to implement a basic reconciliation loop in a later release.
 
## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
