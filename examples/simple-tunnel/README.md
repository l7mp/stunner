# STUNner tutorial: Open a tunnel via STUNner

This introductory demo shows how to tunnel an external connection via STUNner to a UDP service
deployed into Kubernetes. The tutorial can also be used to quickly check a STUNner installation.

In this tutorial you will learn how to:
* configure a UDP greeter service in Kubernetes,
* configure STUNner to expose the greeter service to clients,
* use the [`turncat`](/cmd/turncat) client to connect a Kubernetes service via STUNner,
* benchmark your cloud-setup with [`iperfv2`](https://iperf.fr).

## Installation

### Prerequisites

Consult the [STUNner installation and configuration guide](/doc/INSTALL.md) to set up STUNner. The
tutorial assumes a fresh STUNner installation. You must have [`iperfv2`](https://iperf.fr)
installed locally to run this tutorial.

### Setup

In this tutorial we perform a quick Kubernetes/STUNner benchmark: we fire up an iperf server inside
the cluster and perform a speed test from the local console. We will use the
[`turncat`](/cmd/turncat) client utility to tunnel the test traffic to the iperf server that is
deployed into the Kubernetes cluster, and STUNner will make sure to conveniently expose the iperf
server to the Internet. 

![STUNner benchmarks setup](/doc/stunner_benchmark.svg)

### Configuration

First, set up an iperf server pod in the default Kubernetes namespace and wrap it in the Kubernetes service `iperf-server`.
```console
kubectl apply -f examples/simple-tunnel/iperf-server.yaml
```

Then, expose the service via the STUNner. The pre-compiled manifest below will create the required
GatewayClass and GateayConfig, fire up a Gateway listener at UDP:3478 and another one on TCP:3478,
and route client connection received on the gateways to the `iperf-server` backend service.
```console
kubectl apply -f examples/simple-tunnel/iperf-stunner.yaml
```

### Check your configuration

Use the handy `stunnerctl` CLI tool to dump the running STUNner configuration.

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

NOTE: It usually takes 30-60 seconds for Kubernetes to assign an active external IP address for the
gateways. As long as the external address is in `<PENDING>` status, STUNner exposes the
Gateway on a NodePort: in the above example the UDP Gateway's `udp-listener` is waiting for an
external address to be assigned to it uses one of the node IPs as a public address
(`34.116.220.190`) and the NodePort 30501 to listen to external connections. Once Kubernetes
finished the exposition of the Gateway service, STUNner will pick up the new address and update the
config accordingly. The end results should be something similar to the below; observe how the
`udp-listener` public port has changed to the requested port 3478.

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
stunner-gateway-tcp-gateway-svc   LoadBalancer   10.120.11.196   34.118.93.28    3478:30959/TCP   14h
stunner-gateway-udp-gateway-svc   LoadBalancer   10.120.3.228    34.118.16.31    3478:30501/UDP   6m42s
```

### Run the benchmark

We will need to learn the ClusterIP assigned by Kubernetes to the `iperf-server` service: this will
be the peer address to which `turncat` will try to relay the iperf test traffic via STUNner.
``` console
export IPERF_ADDR=$(kubectl get svc iperf-server -o jsonpath="{.spec.clusterIP}")
```

Next, set up `turncat` to listen on the `UDP:127.0.0.1:500` and tunnel connections from this
listener via the STUNner STUN/TURN listener `udp-listener` to the iperf server. Again, `turncat`
will [parse the running STUNner configuration](/cmd/turncat) from Kubernetes and set the authentication credentials
and STUN/TURN server address/port accordingly.
``` console
./turncat --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/stunnerd-config:udp-listener \
     udp://$IPERF_ADDR:5001
```

Fire up a iperf client from another terminal to start the benchmark.

```console
iperf -c localhost -p 5000 -u -i 1 -l 100 -b 800000 -t 10
```

If the benchmark was successful, the iperf server logs should output something along these lines.
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

It seems that we have managed to squeeze 1000 packets/sec through STUNner without a packet loss, at
an average/min/max one-way latency of 14.256/10.791/97.428 and average jitter 1.426 ms. Not bad
from a Kubernetes cluster fired up in some remote Google datacenter, is it?

Observe that the result is a private IP address: indeed, the `udp-echo` service is not available to
external services at this point. We shall use STUNner to expose the service to the Internet via a
TURN service.

Repeating the test, this time with a STUN/TURN over TCP, casts a somewhat more negative
picture. Change the STUN/TURN URI in the `turncat` CLI to connect via the `tcp-listener`.
``` console
./turncat --log=all:INFO udp://127.0.0.1:5000 k8s://stunner/stunnerd-config:tcp-listener \
     udp://$IPERF_ADDR:5001
```

Run the benchmark again at 10kpps and watch the logs.
``` console
iperf -c localhost -p 5000 -u -l 100 -b 8000000 -o /dev/null-t 10 && \
    kubectl logs $(kubectl get pods -l app=iperf-server -o jsonpath='{.items[0].metadata.name}') | tail -n 1
[  3] 0.0000-9.9365 sec  9.41 MBytes  7.94 Mbits/sec   0.085 ms 1361/100003 (1.4%) 148.261/21.098/454.266/73.704 ms 9927 pps  144 KByte 6.70
```

It seems that average latency jumped to 148 ms, with a max latency of close to 460 ms! That's why
you should try to [avoid TCP at all
cost](https://bloggeek.me/why-you-should-prefer-udp-over-tcp-for-your-webrtc-sessions) in real-time
communications.

### Cleaning up

Stop `turncat` and wipe all Kubernetes configuration.
```console
kubectl delete -f examples/simple-tunnel/iperf-server.yaml 
kubectl delete -f examples/simple-tunnel/iperf-stunner.yaml 
```

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.
