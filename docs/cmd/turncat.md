# turncat: Swiss-army-knife testing tool for STUNner

`turncat` is a STUN/TURN client to open a connection through a TURN server to an arbitrary remote
address/port. The main use is to open a local tunnel endpoint to any service running inside a
Kubernetes cluster via STUNner.  This is very similar in functionality to `kubectl proxy`, but it
uses STUN/TURN to enter the cluster.

## Getting Started

### Installation

As simple as it gets:

```console
cd stunner
go build -o turncat cmd/turncat/main.go
```

### Usage

Listen to client connections on the UDP listener `127.0.0.1:5000` and tunnel the received packets
through the TURN server located at `192.0.2.1:3478` to the UDP server located at
`192.0.2.2:53`. Use the `static` STUN/TURN credential mechanism to authenticate with the TURN
server and set the user/passwd to `test/test`:

```console
./turncat --log=all:INFO,turncat:DEBUG udp://127.0.0.1:5000 turn://test:test@192.0.2.1:3478 udp://192.0.2.2:53
```

TLS/DTLS should also work fine; note that `--insecure` allows `turncat` to accept self-signed TLS
certificates and `--verbose` is equivalent to setting all `turncat` loggers to DEBUG mode (`-l
all:DEBUG`).

```console
./turncat --verbose --insecure udp://127.0.0.1:5000 turn://test:test@192.0.2.1:3478?transport=tls udp://192.0.2.2:53
```

Alternatively, specify the special TURN server URI `k8s://stunner/stunnerd-config:udp-listener` to
let `turncat` parse the running STUNner configuration from the active Kubernetes cluster. The URI
directs `turncat` to read the STUNner config from the ConfigMap named `stunnerd-config` in the
`stunner` namespace, and connect to the STUNner listener named `udp-listener`. The CLI flag `-`
instructs `turncat` to listen on the standard input: anything you type in the terminal will be sent
via STUNner to the peer `udp://10.0.0.1:9001` (after you press Enter). The CLI flag `-v` will
enable verbose logging.

```console
./turncat -v - k8s://stunner/stunnerd-config:udp-listener udp://10.0.0.1:9001
```
