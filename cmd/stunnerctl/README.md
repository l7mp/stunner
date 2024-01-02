# stunnerctl: Command line toolbox for STUNner

A CLI tool to simplify the interaction with STUNner.

## Usage

Dump the running config of a STUNner gateway in human-readable format.

The below will select the Gateway called `tcp-gateway` in the `stunner` namespace:

```console
cmd/stunnerctl/stunnerctl running-config stunner/stunner-gateway
STUN/TURN authentication type:  static
STUN/TURN username:             user-1
STUN/TURN password:             pass-1
Listener 1
        Name:   stunner/tcp-gateway/tcp-listener
        Listener:       stunner/tcp-gateway/tcp-listener
        Protocol:       TURN-TCP
        Public address: 35.187.97.94
        Public port:    3478
```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
