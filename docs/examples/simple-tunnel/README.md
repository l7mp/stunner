# STUNner Tutorial: Opening a UDP tunnel via STUNner

This tutorial shows how to tunnel a local UDP client through STUNner to a UDP service in the cluster, and how to measure the loss and the round trip of the path to evaluate your cloud provider's UDP and TCP stack.

In this tutorial you will learn how to:
* configure a UDP service in Kubernetes,
* configure STUNner to expose the service to clients,
* use the [tunnel mode of `stunnerd`](../../cmd/stunnerd.md#tunnel-mode) to connect to the UDP service via STUNner,
* measure the path with [`mediactl`](../../cmd/mediactl.md).

## Installation

### Prerequisites

The tutorial assumes a fresh STUNner installation; see the [STUNner installation and configuration guide](../../INSTALL.md). Create a namespace called `stunner` if there is none. Issue `make build` to build `stunnerd`, `stunnerctl` and `mediactl` in the `bin/` directory.

### Setup

In this tutorial we perform a quick Kubernetes/STUNner test: we fire up a `mediactl` UDP echo server inside the cluster and measure the path to it from the local console. We will use the [tunnel mode of `stunnerd`](../../cmd/stunnerd.md#tunnel-mode) to tunnel test traffic to the echo server via STUNner acting as a STUN/TURN gateway.

![STUNner benchmarks setup](../../img/stunner_benchmark.svg)

You can easily implement a makeshift VPN with STUNner using a similar setup.

### Server configuration

Set up a UDP echo server in the `default` Kubernetes namespace and wrap it in a Kubernetes
service called `echo-server`.
```console
cd stunner
kubectl apply -f docs/examples/simple-tunnel/echo-server.yaml
```

This will start a Deployment that runs `mediactl udp echo` on UDP port 9001 and wraps it in a Kubernetes service called `echo-server` of type ClusterIP. Check this service and make sure that it is not exposed to the outside world (i.e., `EXTERNAL-IP` is set to `<none>` by Kubernetes); this makes sure that the only way to reach this service from the local test client is through STUNner.

```console
kubectl get service echo-server -o wide
NAME          TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)    AGE   SELECTOR
echo-server   ClusterIP   10.120.5.36   <none>        9001/UDP   19s   app=echo-server
```

### STUNner configuration

Expose the service via the STUNner. The pre-compiled manifest below will create the required GatewayClass and GateayConfig resources, fire up a Gateway listener at UDP:3478 and another one on TCP:3478, and route client connections received on the gateways to the `echo-server` service.

```console
kubectl apply -f docs/examples/simple-tunnel/tunnel-stunner.yaml
```

For convenience, below is a dump of the Gateway and UDPRoute resources the manifests create. Note that the UDPRoute specifies the `echo-server` service as the `backendRef`, which makes sure that STUNner will forward the client connections received in any of the Gateways to the echo server.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: udp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: TURN-UDP

---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: tcp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: tcp-listener
      port: 3478
      protocol: TURN-TCP

---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: echo-server
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
    - name: tcp-gateway
  rules:
    - backendRefs:
        - name: echo-server
          namespace: default
```

### Check your configuration

Check whether you have all the necessary STUNner resources installed namespace.

```console 
kubectl get gatewayconfigs,gateways,udproutes.stunner.l7mp.io -n stunner 
NAME                                                  REALM             DATAPLANE   AGE
gatewayconfig.stunner.l7mp.io/stunner-gatewayconfig   stunner.l7mp.io   default     139m

NAME                                            CLASS                  ADDRESS         PROGRAMMED   AGE
gateway.gateway.networking.k8s.io/tcp-gateway   stunner-gatewayclass   35.187.97.94    True         139m
gateway.gateway.networking.k8s.io/udp-gateway   stunner-gatewayclass   35.205.10.190   True         139m

NAME                                    AGE
udproute.stunner.l7mp.io/echo-server    139m
```

You can also use the handy [`stunnerctl` CLI tool](/docs/cmd/stunnerctl.md) to dump the running STUNner configuration for the UDP gateway.

``` console
bin/stunnerctl -n stunner config udp-gateway
Gateway: stunner/udp-gateway (loglevel: "all:INFO")
Authentication default: static, username/password: user-1/pass-1
Listeners:
  - Name: stunner/udp-gateway/udp-listener
    Protocol: UDP
    Server: stunner/udp-gateway/udp-listener (TURN)
    Address:port: <none>:3478
    Public address:port: 34.118.88.91:3478
    Clusters: [stunner/echo-server/udp]
    Endpoints: [10.76.1.4, 10.80.4.47]
```

Likewise, the below will dump the config for the TCP gateway.

``` console
bin/stunnerctl -n stunner config tcp-gateway
Gateway: stunner/tcp-gateway (loglevel: "all:INFO")
Authentication default: static, username/password: user-1/pass-1
Listeners:
  - Name: stunner/tcp-gateway/tcp-listener
    Protocol: TCP
    Server: stunner/tcp-gateway/tcp-listener (TURN)
    Address:port: <none>:3478
    Public address:port: 34.116.180.89:3478
    Clusters: [stunner/echo-server/udp]
    Endpoints: [10.76.1.4, 10.80.4.47]
```

NOTE: It usually takes 30-60 seconds for Kubernetes to assign an external IP address to STUNner gateways. As long as the external address is in `<PENDING>` status, STUNner exposes the Gateway on a NodePort.  Once Kubernetes finishes the exposition of the Gateway service, STUNner will pick up the new address/port and update the config accordingly.

If in doubt, you can always query Kubernetes for the service statuses.

``` console
kubectl get -n stunner services
NAME          TYPE           CLUSTER-IP    EXTERNAL-IP     PORT(S)          AGE
stunner       ClusterIP      10.0.9.70     <none>          3478/UDP         15m
tcp-gateway   LoadBalancer   10.0.3.91     35.187.97.94    3478:31781/TCP   143m
udp-gateway   LoadBalancer   10.0.14.218   35.205.10.190   3478:31048/UDP   143m
```

### Run the test

Set up a tunnel to listen on `UDP:127.0.0.1:5000` and relay the packets from this listener via the STUNner STUN/TURN listener `udp-listener` to the echo server. Luckily, `stunnerd` is clever enough to [parse the running STUNner configuration](../../cmd/stunnerd.md#tunnel-mode) from Kubernetes and set the STUN/TURN server public address/port and the authentication credentials accordingly, and to find the echo server from its Service.

``` console
bin/stunnerd --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/udp-gateway/udp-listener \
     k8s://default/echo-server:udp
```

The two meta-URIs do the heavy lifting: `k8s://stunner/udp-gateway/udp-listener` instructs the tunnel to look for the Gateway called `udp-gateway` in the `stunner` namespace and create a connection to the TURN listener called `udp-listener` of the Gateway, and `k8s://default/echo-server:udp` is the first ready pod of the `echo-server` Service in the `default` namespace, at the port of the Service port called `udp`.

From another terminal, send packets through the tunnel for 10 seconds and measure the echo. By default `mediactl` sends 50 packets of 160 bytes a second, the 20 ms frames of a G.711 call.

```console
bin/mediactl udp send -t 10s 127.0.0.1:5000
14:31:02.897379 mediactl.go:298: mediactl INFO: interval  0.0- 1.0s: sent 50 pkts 63.99 kbit/s, received 49 pkts 62.71 kbit/s
14:31:03.897349 mediactl.go:298: mediactl INFO: interval  1.0- 2.0s: sent 50 pkts 64 kbit/s, received 50 pkts 64 kbit/s
...
14:31:12.398291 mediactl.go:344: mediactl INFO: summary 10.0s, sent 500 pkts 80 kB 63.99 kbit/s, received 500 pkts 80 kB 63.99 kbit/s, lost 0/500 (0.00%), rtt min/avg/max 215µs/460µs/2.367ms
```

Every packet carries a sequence number and a timestamp, so `mediactl` reports the loss and the round trip of the whole path: the tunnel, STUNner and the echo server. Add `--max-loss` or `--max-rtt` to make `mediactl` fail above a threshold, or `-o json` for a machine-readable report. The echo server logs every flow it echoed:

```console
kubectl logs deploy/echo-server
14:31:01.876212 net.go:52: mediactl INFO: echoing UDP at [::]:9001
14:31:01.896914 echo.go:51: mediactl INFO: echoing 10.116.1.21:34475
14:31:14.898450 echo.go:56: mediactl INFO: flow 10.116.1.21:34475: 10.0s, received 500 pkts 80 kB 64.13 kbit/s
```

Repeat the test, this time with STUN/TURN over TCP. Notice the new meta-URI: `k8s://stunner/tcp-gateway/tcp-listener` to select the TURN server exposed on TCP for the tunnel.

``` console
bin/stunnerd --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/tcp-gateway/tcp-listener \
     k8s://default/echo-server:udp
```

Run the same test and compare the round trip of the two transports: TURN over TCP retransmits lost packets and holds back everything behind them, which shows up as a growing round trip on a lossy path. That's why you should [avoid TCP at all cost](https://bloggeek.me/why-you-should-prefer-udp-over-tcp-for-your-webrtc-sessions) in real-time communications.

`mediactl` can also relay through STUNner by itself, with no tunnel: it reads the TURN server and the credentials from the Gateway and the peer from the Service the same way.

```console
bin/mediactl turn send -t 10s k8s://stunner/udp-gateway/udp-listener k8s://default/echo-server:udp
```

### Measuring under load

For more load, run several streams in parallel with `-P` and set the packet rate per stream with `-k` and the packet size with `--length`; `mediactl` reports every stream and their total. A stream paces its packets in user space and falls short of high rates, so keep `-k` moderate and add streams instead. Five streams of 250 packets of 1000 bytes a second, 10 Mbit/s in total:

```console
bin/mediactl turn send -P 5 -t 10s -k 250 --length 1000 k8s://stunner/udp-gateway/udp-listener k8s://default/echo-server:udp
```

To learn how much of the round trip the cluster adds, take a local baseline: the same streams through a [standalone TURN server](../../cmd/stunnerd.md#usage) on the loopback to a local echo server.

```console
bin/mediactl udp echo 127.0.0.1:9001 &
bin/stunnerd "turn://user-1:pass-1@127.0.0.1:3478?transport=udp" &
bin/mediactl turn send -P 5 -t 10s -k 250 --length 1000 --username user-1 --password pass-1 \
    "turn:127.0.0.1:3478?transport=udp" 127.0.0.1:9001
```

Repeat the measurements with a few packet sizes, say 64, 256, 512 and 1200 bytes: small packets yield a lower throughput at the same packet rate.

### Cleaning up

Stop the tunnel and wipe all Kubernetes configuration.

```console
kubectl delete -f docs/examples/simple-tunnel/echo-server.yaml
kubectl delete -f docs/examples/simple-tunnel/tunnel-stunner.yaml
```

# Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).
