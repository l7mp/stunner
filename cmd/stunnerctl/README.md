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

### License status

STUNner requires a valid license to unlock premium features. The below will report STUNner's license status:

```console
stunnerctl license
License status:
    Subscription type: member
    Enabled features: DaemonSet, STUNServer, ...
    Last updated: ...
```
   
This command will connect to your STUNner gateway operator and validate your license. It will also report any errors STUNner may have encountered while validating your license.

### Authentication

The `auth` sub-command can be used to obtain a TURN credential or a full ICE server config for connecting to a specific gateway. The authentication service API is usually served by a separate [STUNner authentication server](https://github.com/l7mp/stunner-auth-service) deployed alongside the gateway operator. The main use of this command is to feed an ICE agent manually with the ICE server config to connect to a specific STUNner gateway.

- Obtain a full ICE server config for `udp-gateway` deployed into the `stunner` namespace:

  ``` console
  stunnerctl -n stunner auth udp-gateway
  {"iceServers":[{"credential":"pass-1","urls":["turn:1A.B.C.D:3478?transport=udp"],"username":"user-1"}],"iceTransportPolicy":"all"}
  ```

- Request a plain [TURN credential](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) using the authentication service deployed into the `stunner-system-prod` namespace:

  ``` console
  stunnerctl -n stunner auth udp-gateway --auth-turn-credential --auth-service-namespace=stunner-system-prod
  {"password":"pass-1","ttl":86400,"uris":["turn:A.B.C.D:3478?transport=udp"],"username":"user-1"}
```

- Obtain an ICE config for the Gateway `stunner/udp-gateway` and setting the user-id to `my-user`
  (useful for ephemeral authentication):

  ``` console
  stunnerctl  -n stunner auth udp-gateway --username my-user
  {"iceServers":[{"credential":"...=","urls":["turn:A.B.C.D:3478?transport=udp"],"username":"1740150333:my-user"}],"iceTransportPolicy":"all"}
  ```

### ICE test

The `icetest` sub-command can be used to run a full-blown ICE test. This command is intended for
users to check a STUNner installation and pinpoint installation errors.

The tester will fire up a WHIP server in the cluster, configure a UDP and a TCP gateway to expose
it, makes a PeerConnection to the WHIP server via the gateways, and performs a quick test by
sending a set of packets via a data channel created over the PeerConnection and measures loss and
latency using the packets echoed back by the WHIP server. If successful, the tester will output the
measured statistics, otherwise it reports the error that stopped the ICE test and provides some
diagnostics that to help troubleshooting.

- Run a dataplane test over UDP and TCP:

  ``` console
  stunnerctl icetest
  Initializing... completed
  Checking installation... completed
  Checking Gateway... completed
  Obtaining ICE server configuration... completed
  Running asymmetric ICE test over TURN-UDP... completed
  	Statistics: rate=48.65pps, loss=0/973pkts=0.00%, RTT:mean=20.67ms/median=20.54ms/P95=22.23ms/P99=23.34ms
  	LocalICECandidates:
  	  * udp4 relay 10.244.0.24:43988 related 0.0.0.0:43716 (resolved: 10.244.0.24:43988)
  	RemoteICECandidates:
  	  * udp4 host 10.244.0.163:35242 (resolved: 10.244.0.163:35242)
  Running asymmetric ICE test over TURN-TCP... completed
  	Statistics: rate=48.55pps, loss=0/971pkts=0.00%, RTT:mean=21.00ms/median=20.89ms/P95=22.45ms/P99=23.47ms
  	LocalICECandidates:
  	  * udp4 relay 10.244.0.162:45090 related 0.0.0.0:45654 (resolved: 10.244.0.162:45090)
  	RemoteICECandidates:
  	  * udp4 host 10.244.0.163:51653 (resolved: 10.244.0.163:51653)
  Running symmetric ICE test over TURN-UDP... completed
  	Statistics: rate=48.65pps, loss=0/973pkts=0.00%, RTT:mean=20.63ms/median=20.47ms/P95=21.85ms/P99=23.01ms
  	LocalICECandidates:
  	  * udp4 relay 10.244.0.24:47282 related 0.0.0.0:55122 (resolved: 10.244.0.24:47282)
  	RemoteICECandidates:
  	  * udp4 relay 10.244.0.24:47367 related 0.0.0.0:51777 (resolved: 10.244.0.24:47367)
  Running symmetric ICE test over TURN-TCP... completed
  	Statistics: rate=48.60pps, loss=0/972pkts=0.00%, RTT:mean=24.61ms/median=20.56ms/P95=39.96ms/P99=133.40ms
  	LocalICECandidates:
  	  * udp4 relay 10.244.0.162:56555 related 0.0.0.0:42600 (resolved: 10.244.0.162:56555)
  	RemoteICECandidates:
  	  * udp4 relay 10.244.0.162:33397 related 10.244.0.163:53124 (resolved: 10.244.0.162:33397)
  ```

- Clean up the Kubernetes resources the tester might have left behind on a previous run and perform
  the test only on TURN-UDP with at a rate of 100 packets per second using a 2 minute timeout:

  ``` console
  stunnerctl icetest  --force-cleanup -packet-rate 100 --timeout 2m udp
  ```

Run `stunnerctl icetest --help` for further useful command line arguments.

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
