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

Tunnel the local UDP connection `127.0.0.1:5000` through the TURN server `192.0.2.1:3478` to the
remote DNS server located at `192.0.2.2:53`, and use the long-term STUN/TURN credential with
user/passwdL `test/test`:

``` shell
go run main.go --user test=test --log=all:INFO,turncat=DEBUG udp:127.0.0.1:5000 turn:192.0.2.1:3478 udp:192.0.2.2:53
```

## Help

TODO

## Authors

TODO

## License

MIT License - see [LICENSE](LICENSE) for full text

## Acknowledgments

* Initial code adopted from [pion/stun](https://github.com/pion/turn).
