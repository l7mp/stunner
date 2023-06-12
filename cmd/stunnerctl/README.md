# stunnerctl: Command line toolbox for STUNner

A CLI tool to simplify the interaction with STUNner.

## Usage

Dump the running config from a live STUNner deployment in human-readable format.

```console
cmd/stunnerctl/stunnerctl running-config stunner/stunnerd-config
STUN/TURN authentication type:	plaintext
STUN/TURN username:		user-1
STUN/TURN password:		pass-1
Listener:	udp-listener
Protocol:	UDP
Public address:	34.118.36.108
Public port:	3478
```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
