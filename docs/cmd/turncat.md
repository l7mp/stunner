# turncat: Swiss-army-knife testing tool for STUNner

`turncat` is a STUN/TURN client to open a connection through a TURN server to an arbitrary remote address/port. 
The main use is to open a local tunnel endpoint to any service running inside a Kubernetes cluster via STUNner.
This is very similar in functionality to `kubectl port-forward`, but it uses STUN/TURN to enter the cluster. 
This  is much faster than the TCP connection used by `kubectl`.

## Installation

On Linux and macOS, use [this script](/cmd/getstunner/getstunner.sh) to download the latest version of the `turncat` binary:

```console
curl -sL https://raw.githubusercontent.com/l7mp/stunner/main/cmd/getstunner/getstunner.sh | sh -
export PATH=$HOME/.l7mp/bin:$PATH
```
> [!NOTE]
> The script installs `stunnerctl` too.

Install the `turncat` binary using the standard Go toolchain and add it to `$PATH`.

```console
go install github.com/l7mp/stunner/cmd/turncat@latest
```

You can also enforce a specific OS, CPU architecture, and STUNner version like below:

```console
GOOS=windows GOARCH=amd64 go install github.com/l7mp/stunner/cmd/turncat@v0.17.5
```

Building from source is as easy as it usually gets with Go:

```console
cd stunner
go build -o turncat cmd/turncat/main.go
```

## Usage

Listen to client connections on the UDP listener `127.0.0.1:5000` and tunnel the received packets through the TURN server located at `192.0.2.1:3478` to the UDP listener located at `192.0.2.2:53`. 
Use the [`static` STUN/TURN credential mechanism](/docs/AUTH.md) to authenticate with the TURN server and set the user/passwd to `test/test`:

```console
./turncat --log=all:INFO,turncat:DEBUG udp://127.0.0.1:5000 turn://test:test@192.0.2.1:3478 \
    udp://192.0.2.2:53
```

TLS/DTLS should also work. 
Below `--insecure` allows `turncat` to accept self-signed TLS certificates and `--verbose` is equivalent to setting all loggers to DEBUG mode (`-l all:DEBUG`).

```console
./turncat --verbose --insecure udp://127.0.0.1:5000 \
    turn://test:test@192.0.2.1:3478?transport=tls udp://192.0.2.2:53
```

Alternatively, you can specify the special TURN server meta-URI `k8s://stunner/udp-gateway:udp-listener` to let `turncat` parse the running STUNner configuration from the active Kubernetes cluster. 
The URI directs `turncat` to read the config of the STUNner Gateway called `udp-gateway` in the `stunner` namespace and connect to the TURN listener named `udp-listener`. 
The CLI flag `-` instructs `turncat` to listen on the standard input: anything you type in the terminal will be sent via STUNner to the peer `udp://10.0.0.1:9001` (after you press Enter). 
The CLI flag `-v` will enable verbose logging.

```console
./turncat -v - k8s://stunner/udp-gateway:udp-listener udp://10.0.0.1:9001
```

Note that the standard `kubectl` command line flags are available. 
For instance, the below will use the context `prod-europe` from the kubeconfig file `kube-prod.conf`:

```console
./turncat --kubeconfig=kube-prod.conf --context prod-europe -v - k8s://... udp://...
```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and [pion/turn](https://github.com/pion/turn).
