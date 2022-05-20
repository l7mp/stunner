# stunnerd: The STUNner gateway daemon

The `stunnerd` daemon implements the STUNner gateway service.

## Getting Started

Installation is as easy as it gets:

```console
$ cd stunner
$ go build stunnerd cmd/stunnerd/main.go
```

## Features

* Full Kubernetes integration for quick installation into any hosted or on-prem Kubernetes cluster
  and easy Day-2 operations.
* [RFC 5389](https://tools.ietf.org/html/rfc5389): Session Traversal Utilities for NAT (STUN)
* [RFC 8656](https://tools.ietf.org/html/rfc8656): Traversal Using Relays around NAT (TURN)
* [RFC 6062](https://tools.ietf.org/html/rfc6062): Traversal Using Relays around NAT (TURN)
  Extensions for TCP Allocations
* TURN transport over UDP, TCP, TLS/TCP and DTLS/UDP.
* Two authentication modes via the long-term STUN/TURN credential mechanism: `plaintext` using a
  static username/password pair, and `longterm` with dynamically generated time-scoped credentials.

## Usage

The below command will open a `stunnerd` UDP listener service at `127.0.0.1:5000` with `plaintext`
authentication using the username/password pair `user1/passwrd1`, and set the maximum debug level.

```console
$ ./stunnerd --log=all:TRACE turn://user1:passwd1@127.0.0.1:5000
```

Alternatively, run `stunnerd` in verbose mode with the configuration file taken from
`cmd/stunnerd/stunnerd.conf`.

```console
$ ./stunnerd -v -c cmd/stunnerd/stunnerd.conf
```

Type `./stunnerd` to see a short description of the command line arguments supported by `stunnerd`.

In practice, you'll rarely need to run `stunnerd` directly: just fire up the [prebuilt container
image](https://hub.docker.com/repository/docker/l7mp/stunnerd) in Kubernetes using one of the
[installation modes](/doc/INSTALL.md) and you should be good to go.

## Configuration

Using the below configuration, `stunnerd` will open 4 STUNner listeners: two for accepting
unencrypted connections at UDP/3478 and TCP/3478, and two for encrypted connections at TLS/TCP/3479
and DTLS/UDP/3479. For easier debugging, the port for the transport relay connections opened by
`stunnerd` will be taken from [10000:19999] for the UDP listener, [20000:29999] for the TCP
listener, etc.  The daemon will use `longterm` authentication, with the shared secret read from the
environment variable `$STUNNER_SHARED_SECRET` during initialization. The relay address is set to
`$STUNNER_ADDR`.

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
