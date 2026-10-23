# icetester: Universal UDP echo service using WebRTC/ICE

`icetester` is a test server that can be used to test WebRTC/ICE connectivity. The tester serves a simple
WHIP server that clients can use to create a WebRTC data channel. Whatever is
received by `icetester` on the data channel will be echoed back to the client over the data channel.

While `icetester` can be used as a standalone tool, the intended use is via `stunnerctl icetest`.

## Installation

Install `icetester` using the standard Go toolchain and add it to `$PATH`.

```console
go install github.com/l7mp/stunner/v2/cmd/icetester@latest
```

Building from source is as easy as it usually gets with Go:

```console
cd stunner
go build -o icetester ./cmd/icetester
```

The containerized version is available as `docker.io/l7mp/icetester`.

## Usage

Deploy a STUNner gateway and test it via UDP and TCP through `stunnerctl`:

```console
stunnerctl icetest
```

## License

Copyright 2021-2026 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
