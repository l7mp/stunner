# STUNner Tutorial

# Open a tunnel via STUNner

This tutorial shows how to tunnel an external connection via STUNner to a UDP service deployed into
Kubernetes. The tutorial can also be used to quickly check a STUNner installation.

In this tutorial you will learn how to:
* configure a UDP service in Kubernetes,
* configure STUNner to expose the service to clients,
* use [`turncat`](../../cmd/turncat.md) to connect to the UDP service via STUNner,
* benchmark your cloud-setup with [`iperfv2`](https://iperf.fr).

## Installation

### Prerequisites

The tutorial assumes a fresh STUNner installation; see the [STUNner installation and configuration
guide](../../INSTALL.md). Create a namespace called `stunner` if there is none. You must have
[`iperfv2`](https://iperf.fr) installed locally to run this tutorial.

### Setup

In this tutorial we perform a quick Kubernetes/STUNner benchmark: we fire up an iperf server inside
the cluster and perform a speed test from the local console. We will use the
[`turncat`](../../cmd/turncat) client utility to tunnel test traffic to the iperf server via STUNner
acting as a STUN/TURN gateway.

![STUNner benchmarks setup](../../img/stunner_benchmark.svg)

You can easily implement a makeshift VPN with STUNner using a similar setup.

### Server configuration

Set up an iperf server in the `default` Kubernetes namespace and wrap it in a Kubernetes
service called `iperf-server`.
```console
cd stunner
kubectl apply -f docs/examples/simple-tunnel/iperf-server.yaml
```

This will start an Deployment that runs the iperf server and wraps it in a Kubernetes service
called `iperf-server` of type ClusterIP. Check this service and make sure that it is not exposed to
the outside world (i.e., `EXTERNAL-IP` is set to `<none>` by Kubernetes); this makes sure that the
only way to reach this service from the local iperf speed-test client is through STUNner.

```console
kubectl get service iperf-server  -o wide
NAME           TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)             AGE   SELECTOR
iperf-server   ClusterIP   10.120.5.36   <none>        5001/UDP,5001/TCP   19s   app=iperf-server
```

### STUNner configuration

Expose the service via the STUNner. The pre-compiled manifest below will create the required
GatewayClass and GateayConfig resources, fire up a Gateway listener at UDP:3478 and another one on
TCP:3478, and route client connections received on the gateways to the `iperf-server`
service.
```console
kubectl apply -f docs/examples/simple-tunnel/iperf-stunner.yaml
```

For convenience, below is a dump of the Gateway and UDPRoute resources the manifests create. Note
that the UDPRoute specifies the `iperf-server` service as the `backendRef`, which makes sure that
STUNner will forward the client connections received in any of the Gateways to the iperf server.

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
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
apiVersion: gateway.networking.k8s.io/v1beta1
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
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: iperf-server
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
    - name: tcp-gateway
  rules:
    - backendRefs:
        - name: iperf-server
          namespace: default
```

### Check your configuration

Check whether you have all the necessary STUNner resources installed namespace.

```console 
kubectl get gatewayconfigs,gateways,udproutes -n stunner 
NAME                                                  REALM             AUTH        AGE
gatewayconfig.stunner.l7mp.io/stunner-gatewayconfig   stunner.l7mp.io   plaintext   3m53s

NAME                                            CLASS                  ADDRESS   READY   AGE
gateway.gateway.networking.k8s.io/tcp-gateway   stunner-gatewayclass             True    14s
gateway.gateway.networking.k8s.io/udp-gateway   stunner-gatewayclass             True    14s

NAME                                              AGE
udproute.gateway.networking.k8s.io/iperf-server   14s
```

You can also use the handy `stunnerctl` CLI tool to dump the running STUNner configuration.

``` console
cmd/stunnerctl/stunnerctl running-config stunner/stunnerd-config
STUN/TURN authentication type:  plaintext
STUN/TURN username:             user-1
STUN/TURN password:             pass-1
Listener 1
        Name:   udp-listener
        Listener:       udp-listener
        Protocol:       UDP
        Public address: 34.116.220.190
        Public port:    30501
Listener 2
        Name:   tcp-listener
        Listener:       tcp-listener
        Protocol:       TCP
        Public address: 34.118.93.28
        Public port:    3478
```

NOTE: It usually takes 30-60 seconds for Kubernetes to assign an external IP address to STUNner
gateways. As long as the external address is in `<PENDING>` status, STUNner exposes the Gateway on
a NodePort: in the above example the UDP Gateway's `udp-listener` is exposed on a node IP
(`34.116.220.190`) and the NodePort 30501. Once Kubernetes finishes the exposition of the Gateway
service, STUNner picks up the new address/port and updates the config accordingly. The end
result should be something similar to the below; observe how the `udp-listener` public port has
changed to the requested port 3478 and the public address is updated as well.

``` console
cmd/stunnerctl/stunnerctl running-config stunner/stunnerd-config
[...]
Listener 1
        Name:   udp-listener
        Listener:       udp-listener
        Protocol:       UDP
        Public address: 34.118.16.31
        Public port:    3478
[...]
```

If in doubt, you can always query Kubernetes for the service statuses.
``` console
kubectl get -n stunner services
NAME                              TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)          AGE
stunner                           ClusterIP      10.120.4.118    <none>          3478/UDP         2d
tcp-gateway                       LoadBalancer   10.120.11.196   34.118.93.28    3478:30959/TCP   14h
udp-gateway                       LoadBalancer   10.120.3.228    34.118.16.31    3478:30501/UDP   6m42s
```

### Run the benchmark

We will need to learn the ClusterIP assigned by Kubernetes to the `iperf-server` service: this will
be the peer address to which `turncat` will ask STUNner to relay the iperf test traffic.
``` console
export IPERF_ADDR=$(kubectl get svc iperf-server -o jsonpath="{.spec.clusterIP}")
```

Next, set up `turncat` to listen on `UDP:127.0.0.1:5000` and tunnel connections from this
listener via the STUNner STUN/TURN listener `udp-listener` to the iperf server. Luckily, `turncat`
is clever enough to [parse the running STUNner configuration](../../cmd/turncat) from Kubernetes and set
the STUN/TURN server public address/port and the authentication credentials
accordingly.
``` console
./turncat --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/stunnerd-config:udp-listener \
     udp://$IPERF_ADDR:5001
```

Fire up an iperf client from another terminal to start the benchmark.

```console
iperf -c localhost -p 5000 -u -i 1 -l 100 -b 800000 -t 10
```

If successful, the iperf server logs should contain the benchmark results.
```console
kubectl logs $(kubectl get pods -l app=iperf-server -o jsonpath='{.items[0].metadata.name}')
------------------------------------------------------------
Server listening on UDP port 5001 with pid 1
Read buffer size: 1.44 KByte (Dist bin width= 183 Byte)
UDP buffer size:  208 KByte (default)
------------------------------------------------------------
[  1] local 10.116.2.30%eth0 port 5001 connected with 10.116.1.21 port 56439 (peer 2.1.7)
[ ID] Interval            Transfer     Bandwidth        Jitter   Lost/Total   Latency avg/min/max/stdev PPS  inP NetPwr
[  1] 0.0000-9.9204 sec   977 KBytes   807 Kbits/sec    1.426 ms 0/10003 (0%) 14.256/10.791/97.428/ 4.993 ms 1008 pps 1.40 KByte 7.07
```

The results show that we have managed to squeeze 1000 packets/sec through STUNner without packet
loss, at an average one-way latency of 14.2 ms and average jitter 1.426 ms. Not bad from a
Kubernetes cluster running in some remote datacenter!

Repeating the test, this time with a STUN/TURN over TCP, casts a somewhat more negative
picture. Change the STUN/TURN URI in the `turncat` CLI to connect via the `tcp-listener`.
``` console
./turncat --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/stunnerd-config:tcp-listener \
     udp://$IPERF_ADDR:5001
```

Run the benchmark again at 10kpps and watch the logs.
``` console
iperf -c localhost -p 5000 -u -l 100 -b 8000000 -o /dev/null -t 10 && \
    kubectl logs $(kubectl get pods -l app=iperf-server -o jsonpath='{.items[0].metadata.name}') | tail -n 1
[  3] 0.0000-9.9365 sec  9.41 MBytes  7.94 Mbits/sec   0.085 ms 1361/100003 (1.4%) 148.261/21.098/454.266/73.704 ms 9927 pps  144 KByte 6.70
```

It seems that average latency has jumped to 148 ms, with a max latency of close to 460 ms! That's
why you should try to [avoid TCP at all
cost](https://bloggeek.me/why-you-should-prefer-udp-over-tcp-for-your-webrtc-sessions) in real-time
communications.

### Cleaning up

Stop `turncat` and wipe all Kubernetes configuration.
```console
kubectl delete -f docs/examples/simple-tunnel/iperf-server.yaml
kubectl delete -f docs/examples/simple-tunnel/iperf-stunner.yaml
```
