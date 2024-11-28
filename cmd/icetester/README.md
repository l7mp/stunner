# icetester: Universal UDP echo service using WebRTC/ICE

`icetester` is test server that can be used WebRTC/ICE connectivity. The tester serves a simple
WebSocket/JSON API server that clients can use to create a WebRTC data channel. whatever is
received by `icetester` on the data channel will be echoed back to the client over the data channel.

While `icetester` can be used as a standalone too, the intended use is via `stunnerctl icetest`.

## Installation

Install `icetest` using the standard Go toolchain and add it to `$PATH`. 

```console
go install github.com/l7mp/stunner/cmd/icetest@latest
```

Building from source is as easy as it usually gets with Go:

```console
cd stunner
go build -o turncat cmd/turncat/main.go
```

The containerized version is available as `docker.io/l7mp/icester`.

## Usage

Deploy a STUNner gateway and test is via UDP and TCP through `stunnerctl`: 

```console
stunnerctl icetest
```

## License

Copyright 2021-2024 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
