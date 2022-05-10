# turncat: Tunnel a local connection through a TURN server

A simple STUN/TURN client to tunnel a local connection through a TURN server to an arbitrary remote
address/port. The main use is to open a connection to any service running inside a Kubernetes
cluster.  This is very similar in functionality to `kubectl proxy`, but it uses STUN/TURN to enter
the cluster.

## Getting Started

### Installing

As simple as it gets:

```console
$ cd stunner
$ go build -o turncat utils/turncat/main.go
```

### Usage

Tunnel the local UDP connection at `127.0.0.1:5000` through the TURN server `192.0.2.1:3478` to the
remote DNS server located at `192.0.2.2:53`, and use the long-term STUN/TURN credential with
user/passwd `test/test` and realm `REALM`:

```console
$ ./turncat --realm=REALM --log=all:INFO,turncat:DEBUG \
      udp://127.0.0.1:5000 turn://test:test@192.0.2.1:3478 udp://192.0.2.2:53
```

### Advanced usage

The below will execute an [`iperfv2` benchmark](https://iperf.fr) to measure the available
bandwidth and/or latency from the local endhost to the Kubernetes cluster through STUNner.

If you haven't done that so far, [deploy](/README.md/#getting-started) STUNner into Kubernetes. The
below description assumes `plaintext` authentication (this is the default), see the [authentication
guide](/doc/AUTH.md) on how to enable this mode. Then, fire up an `iperfv2` UDP server in the
cluster and store the service IP: this will be the peer address for our TURN tunnel.

```console
$ kubectl create deployment iperf-server --image=l7mp/net-debug:0.5.3
$ kubectl expose deployment iperf-server --name=iperf-server --type=ClusterIP --port=5001
$ kubectl exec -it $(kubectl get pod -l app=iperf-server -o jsonpath="{.items[0].metadata.name}") -- \
      iperf -s -p 5001 -u -e
```

NOTE: The `l7mp/net-debug` image contains lots of useful network debugging utilities. You can use
it to debug your cluster dataplane, e.g., by deploying it as a sidecar container next to
`stunnerd`.

Next, start `turncat` in a local terminal to tunnel the `localhost:5001` UDP connection through
STUNner to the `iperf` server.

```console
$ export IPERF_ADDR=$(kubectl get pod -l app=iperf-server -o jsonpath="{.items[0].status.podIP}")
$ export STUNNER_PUBLIC_ADDR=$(kubectl get service stunner -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PORT=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_PORT}')
$ export STUNNER_REALM=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_REALM}')
$ export STUNNER_USERNAME=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_USERNAME}')
$ export STUNNER_PASSWORD=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_PASSWORD}')
$ ./turncat --realm=$STUNNER_REALM --log=all:INFO udp://127.0.0.1:5001 \
    turn://$STUNNER_USERNAME:$STUNNER_PASSWORD@$STUNNER_PUBLIC_ADDR:$STUNNER_PORT udp://$IPERF_ADDR:5001
```

Temporarily open up the STUNner `NetworkPolicy` to allow STUNner to send traffic to the
`iperf-server` deployment.

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels: {}
      podSelector:
        matchLabels:
         app: iperf-server
    ports:
    - protocol: UDP
      port: 5001
EOF
```

And finally start the `iperf` client locally to execute the benchmark. The `iperfv2` utility must
be installed locally for this to work; e.g., use `apt-get install iperf` to install it on
Debian/Ubuntu. The below will send 100-byte UDP test packets through STUNner at 10pps rate for 10
seconds.

```console
$ iperf -c -u localhost -p 5001 -i 1 -l 100 -b 8000 -t 10
```

The `iperf` server output will contain the results, something akin to the below:
```
------------------------------------------------------------
Server listening on UDP port 5001 with pid 50
Read buffer size: 1.44 KByte (Dist bin width= 183 Byte)
UDP buffer size:  208 KByte (default)
------------------------------------------------------------
[  3] local 10.116.2.10%eth0 port 5001 connected with A.B.C.D port XXXXX (peer 2.1.5)
[ ID] Interval            Transfer     Bandwidth        Jitter   Lost/Total  Latency avg/min/max/stdev PPS  inP NetPwr
[  3] 0.0000-10.0172 sec  10.1 KBytes  8.23 Kbits/sec   1.147 ms    0/  103 (0%) 13.345/10.950/94.259/12.519 ms   10 pps 13.7 Byte 0.08
```

The below will execute a benchmark at 4000 pps.

```console
$ iperf -u -c localhost -p 5001 -i 1 -l 100 -b 3200000 -t 10
```

### Cleanup

Make sure to revert the STUNner `NetworkPolicy` in order to close down all unintended external
access to sensitive services running inside the cluster.

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
EOF
```
Then, stop `turncat` and delete the `iperf-server` deployment and service.

```console
$ kubectl delete service iperf-server 
$ kubectl delete deployment iperf-server 
```

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
