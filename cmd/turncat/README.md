# turncat: Tunnel a local connection through a TURN server

The `turncat` tool implements a simple STUN/TURN client to open a connection through a TURN server
to an arbitrary remote address/port. The main use is to open a connection to any service running
inside a Kubernetes cluster via STUNner.  This is very similar in functionality to `kubectl proxy`,
but it uses STUN/TURN to enter the cluster.

## Getting Started

### Installation

As simple as it gets:

```console
cd stunner
go build -o turncat cmd/turncat/main.go
```

### Usage

Tunnel the local UDP connection at `127.0.0.1:5000` through the TURN server `192.0.2.1:3478` to the
remote DNS server located at `192.0.2.2:53`, and use the long-term STUN/TURN credential with
user/passwd `test/test`:

```console
./turncat --log=all:INFO,turncat:DEBUG udp://127.0.0.1:5000 \
      turn://test:test@192.0.2.1:3478 udp://192.0.2.2:53
```

Alternatively, specify the special STUNner URI `k8s://stunner/stunnerd-config:udp-listener` to let
`turncat` parse the running STUNner configuration from the active Kubernetes cluster context,
identifying the `stunner` namespace and the `stunnerd-config` ConfigMap to read the STUNner config
from plus the listener called `udp-listener` to connect to. The CLI flag `-v` will enable verbose
logging.

```console
./turncat -v - k8s://stunner/stunnerd-config:udp-listener udp://${PEER_IP}:9001
```

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
