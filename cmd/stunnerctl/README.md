# stunnerctl: Command line toolbox for STUNner

A CLI tool to simplify the interaction with STUNner. 
The prominent use of `stunnerctl` is to load or watch STUNner dataplane configurations from a Kubernetes cluster for debugging and troubleshooting, or just for checking whether everything is configured the way it should be.

## Installation

Install the `stunnerctl` binary using the standard Go toolchain and add it to `$PATH`. 

```console
go install github.com/l7mp/stunner/cmd/stunnerctl@latest
```

You can also enforce a specific OS, CPU architecture, and STUNner version:

```console
GOOS=windows GOARCH=amd64 go install github.com/l7mp/stunner/cmd/stunnerctl@v0.17.5
```

Building from source is as easy as it usually gets with Go:

```console
cd stunner
go build -o stunnerctl cmd/stunnerctl/main.go
```

## Usage

Type `stunnerctl` to get a glimpse of the features provided. Below are some common usage examples.

- Dump a summary of the running config of the STUNner gateway called `udp-gateway` deployed into the `stunner` namespace:

  ```console
  stunnerctl -n stunner config udp-gateway
  Gateway: stunner/udp-gateway (loglevel: "all:INFO")
  Authentication type: static, username/password: user-1/pass-1
  Listeners:
    - Name: stunner/udp-gateway/udp-listener
      Protocol: TURN-UDP
      Public address:port: 34.118.88.91:9001
      Routes: [stunner/iperf-server]
      Endpoints: [10.76.1.3, 10.80.7.104]
  ```

- Dump a the running config of all gateways in the `stunner` namespace in JSON format (YAML is also available using `-o yaml`):

  ```console
  stunnerctl -n stunner config -o json
  {"version":"v1","admin":{"name":"stunner/tcp-gateway",...}}
  {"version":"v1","admin":{"name":"stunner/udp-gateway",...}}}
  ```

- Watch all STUNner configs as they are being refreshed and dump only the name of the STUNner gateway whose config changes:

  ```console
  stunnerctl config --all-namespaces -o jsonpath='{.admin.name}' -w
  stunner/tcp-gateway
  stunner/udp-gateway
  ...
  ```

## Fallback

For those who don't have the Go toolchain available to run `go install`, STUNner provides a minimalistic `stunnerctl` replacement called `stunnerctl.sh`.
This script requires nothing else than `bash`, `kubectl`, `curl` and `jq` to work. 

The below will dump the running config of `tcp-gateway` deployed into the `stunner` namespace:

```console
cd stunner
cmd/stunnerctl/stunnerctl.sh running-config stunner/tcp-gateway
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

## Last resort

You can also use `kubectl port-forward` to load or watch STUNner configs manually.
Open a port-forwarded connection to the STUNner gateway operator:

``` console
export CDS_SERVER_NAME=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.name}')
export CDS_SERVER_NAMESPACE=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.namespace}')
kubectl -n $CDS_SERVER_NAMESPACE port-forward pod/${CDS_SERVER_NAME} 63478:13478 &
```

If all goes well, you can now connect to the STUNner config discovery API served by the gateway operator directly, just using `curl`. 
The below will load the config of the `udp-gateway` in the `stunner` namespace:

``` console
curl -s http://127.0.0.1:63478/api/v1/configs/stunner/udp-gateway
```

If you happen to have a WebSocket client like the wonderful [`websocat`](https://github.com/vi/websocat) tool installed, you can also watch the configs as they are being rendered by the operator en live.

``` console
websocat ws://127.0.0.1:63478/api/v1/configs/stunner/udp-gateway?watch=true -
```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
