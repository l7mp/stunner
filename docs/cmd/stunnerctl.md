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

Type `stunnerctl` to get a glimpse of the sub-commands and features provided. 

### Config

The `config` sub-command is used to load or watch running dataplane configs from the STUNner config discovery service (CDS) running in a remote Kubernetes cluster. Usually the CDS server role is fulfilled by the [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) but you can choose any CDS service you want (see the `--cds-server-*` CLI flags in the help). The main use of this command is to check the active dataplane configuration for troubleshooting connectivity problems.

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

- The same, but using the alternative Kubernetes config file `~/my-config.conf` to access the cluster. The rest of the usual `kubectl` flags (`--context`, `--token`, etc.) are also available to select the cluster to connect to.

  ``` console
  stunnerctl --kubeconfig ~/my-config.conf -n stunner config udp-gateway
  ```

- Dump the running config of all gateways in the `stunner` namespace in JSON format (YAML is also available using `-o yaml`):

  ```console
  stunnerctl -n stunner config -o json
  {"version":"v1","admin":{"name":"stunner/tcp-gateway",...}}
  {"version":"v1","admin":{"name":"stunner/udp-gateway",...}}}
  ```

- Watch STUNner configs as they are being refreshed by the operator and dump only the name of the gateway whose config changes:

  ```console
  stunnerctl config --all-namespaces -o jsonpath='{.admin.name}' -w
  stunner/tcp-gateway
  stunner/udp-gateway
  ...
  ```

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

You can also use `kubectl port-forward` to load or watch STUNner configs manually.  Open a port-forwarded connection to the STUNner gateway operator:

``` console
export CDS_SERVER_NAME=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.name}')
export CDS_SERVER_NAMESPACE=$(kubectl get pods -l stunner.l7mp.io/config-discovery-service=enabled --all-namespaces -o jsonpath='{.items[0].metadata.namespace}')
kubectl -n $CDS_SERVER_NAMESPACE port-forward pod/${CDS_SERVER_NAME} 63478:13478 &
```

If all goes well, you can now connect to the STUNner CDS API served by the gateway operator through the port-forwarded tunnel opened by `kubectl` just using `curl`.  The below will load the config of the `udp-gateway` in the `stunner` namespace:

``` console
curl -s http://127.0.0.1:63478/api/v1/configs/stunner/udp-gateway
```

If you happen to have a WebSocket client like the wonderful [`websocat`](https://github.com/vi/websocat) tool installed, you can also watch the configs as they are being rendered by the operator en live.

``` console
websocat ws://127.0.0.1:63478/api/v1/configs/stunner/udp-gateway?watch=true -
```

### Status

The `status` sub-command reports the status of the dataplane pods for a gateway, especially the runtime state of the `stunnerd` daemon.

- Find all dataplane pods for the `udp-gateway` in the `stunner` namespace and dump a status summary:

  ``` console
  stunnerctl -n stunner status udp-gateway
  stunner/udp-gateway-856c9f4dc9-524hc:
  	stunner/udp-gateway:{logLevel="all:INFO",health-check="http://:8086"}
  	static-auth:{realm="stunner.l7mp.io",username="<SECRET>",password="<SECRET>"}
  	listeners:1/clusters:1
  	allocs:3/status=READY
  stunner/udp-gateway-856c9f4dc9-c7wcq:
  	stunner/udp-gateway:{logLevel="all:INFO",health-check="http://:8086"}
  	static-auth:{realm="stunner.l7mp.io",username="<SECRET>",password="<SECRET>"}
  	listeners:1/clusters:1
  	allocs:2/status=READY
  ```

- Same but report only the runtime status of the `stunnerd` pods in the `stunner` namespace:

  ``` console
  stunnerctl -n stunner status -o jsonpath='{.status}'
  READY
  TERMINATING
  ```

### Authentication

The `auth` sub-command can be used to obtain a TURN credential or a full ICE server config for connecting to a specific gateway. The authentication service API is usually served by a separate [STUNner authentication server](https://github.com/l7mp/stunner-auth-service) deployed alongside the gateway operator. The main use of this command is to feed an ICE agent manually with the ICE server config to connect to a specific STUNner gateway.

- Obtain a full ICE server config for `udp-gateway` deployed into the `stunner` namespace:

  ``` console
  stunnerctl -n stunner auth udp-gateway
  {"iceServers":[{"credential":"pass-1","urls":["turn:10.104.19.179:3478?transport=udp"],"username":"user-1"}],"iceTransportPolicy":"all"}
  ```

- Request a plain [TURN credential](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) using the authentication service deployed into the `stunner-system-prod` namespace:

  ``` console
  stunnerctl -n stunner auth udp-gateway --auth-turn-credential --auth-service-namespace=stunner-system-prod
  {"password":"pass-1","ttl":86400,"uris":["turn:10.104.19.179:3478?transport=udp"],"username":"user-1"}
  ```

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
