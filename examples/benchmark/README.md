# Performance evaluation

With the help of this guide you are able to take performance measurments in your setup using STUNner. Both running STUNner locally (outside of Kubernetes) and running STUNner in Kubernetes can be evaluated.

Compare the locally measured results to the result measured in Kubernetes and figure out the overhead cost. The extra cost of your cluster's networking may surprise you in terms of extra delay or more packet drops using the same bandwidth.

### Prerequisites

The tutorial assumes a fresh STUNner installation; see the [STUNner installation and configuration
guide](/doc/INSTALL.md). Create a namespace called `stunner` if there is none. 
You must have [`iperfv2`](https://iperf.fr), [`jq`](https://stedolan.github.io/jq/) and most importantly [Go](https://go.dev/doc/install) installed locally to run this tutorial.

## Installation

Install it in case you would like to benchmark your Kubernetes setup. If you want to benchmark locally skip this step.

Install the STUNner gateway operator and STUNner ([more info](https://github.com/l7mp/stunner-helm)):
```console
helm repo add stunner https://l7mp.io/stunner
helm repo update

helm install stunner-gateway-operator stunner/stunner-gateway-operator

helm install stunner stunner/stunner
```

Configure STUNner to act as a STUN server towards [`turncat`](../turncat/README.md) clients, and to let `iperf` client's traffic reach the `iperf` server.

```
kubectl apply -f iperf-server.yaml
kubectl apply -f performance-stunner.yaml
```

## Usage

### Parameters

The script uses positional parameters, this means their order is important and cannot be changed. The parameters in order are the following: 
* `<num>` Number of `turncat` clients (more of them can be used, this way each client will forward lesser traffic and none of them becomes the bottleneck while measuring)
* `<time>` Time in seconds to transmit for
* `<p_size>` Size of the packet in bytes
* `<bwidth>` Bandwidth to send in bits/sec
* `<plat>` Platform, can be `local` or `k8s`

```
./benchmark.sh <num> <time> <p_size> <bwidth> <plat>
```

### Performance measuring locally without Kubernetes

The below command will open:
* a `stunnerd` UDP listener at `127.0.0.1:5001`
* one or more `turncat` clients at `127.0.0.1:90XY` to open a connection through STUNner to the iperf server
* an `iperf` server at `127.0.0.1:5000`
* an `iperf` client sending its traffic to the turn

An example for:
* 5 turncats
* 5 seconds of evaluation time
* 1000 byte packets
* 100 Mbits/sec
* local benchmark
```
./benchmark.sh 5 5 1000 100000000 local
```
### Performance measuring with Kubernetes

The below command will open:
* one or more `turncat` at `127.0.0.1:90XY` clients to open a connection through STUNner to the iperf server
* an `iperf` client sending its traffic to the turn

`STUNner` and `iperf` are running inside the Kubernetes Cluster.

An example for:
* 5 turncats
* 5 seconds of evaluation time
* 1000 byte packets
* 100 Mbits/sec
* Kubernetes benchmark
```
./benchmark.sh 5 5 1000 100000000 k8s
```
## Advice

It is advised to repeat the measurment with different packet sizes. Recommended packet sizes in bytes are 64, 128, 256, 512, 1024, 1200.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.