# Stunner: A cloud-native STUN/TURN server for WebRTC

A simple STUN/TURN client to tunnel a local UDP connection through a TURN server.

## Description

TODO

## Getting Started

### Dependencies

TODO

### Installing

TODO

### Executing program

Tunnel the local UDP client connection `127.0.0.1:5000` through the TURN server `34.116.207.28:3478` to
the peer `10.76.128.7:53`:

``` shell
go run main.go --client=127.0.0.1:5000 --server=34.116.207.28:3478 --peer=10.76.128.7:53 --user test=test --verbose
```

## Help

TODO

## Authors

TODO

## License

MIT License - see [LICENSE](LICENSE) for full text

## Acknowledgments

* Initial code adopted from [pion/stun](https://github.com/pion/turn).
