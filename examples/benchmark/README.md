# Performance Benchmarking

With the help of this guide you are able to take performance measurments in your setup using STUNner. Both running STUNner locally (outside of Kubernetes) and running STUNner in Kubernetes can be evaluated.

Compare the locally measured result to the result measured in Kubernetes and figure out the overhead cost. The extra cost of your cluster's networking may surprise you in terms of extra delay or more packet drops using the same bandwidth.

Locally there is no installation needed, it should take less than a minute to measure.
If you have a Kubernetes cluster up and running, the installation and measurement should take a few minutes max.

## Tools

The tools used in the measurment are the following:
* `iperf` Using to create traffic flows between the clients and server
* `turncat` Using to open a connection through STUNner to the iperf server
* `STUNner` Acting as a STUN server towards `turncat` clients

### Caveats

When measuring latency with `iperf` you might be fooled because it is [measuring one-way latency](https://stackoverflow.com/questions/63793030/iperf2-latency-is-a-two-way-or-one-way-latency) which requires the clocks to be synchronized. This means you might see corrupted latencies such as negative ones.

## Measurement Setup

### Local setup

All the components are running locally. All of them are using `127.0.0.1` addresses.

![STUNner benchmark local test architecture](../../doc/stunner_benchmark_local.svg)

### Kubernetes setup

`iperf` and `turncat` clients are running locally, both `STUNner` and `iperf` server are running inside a Kubernetes Cluster in a pod. 

![STUNner benchmark Kubernetes test architecture](../../doc/stunner_benchmark_k8s.svg)

## Prerequisites

You must have [`iperfv2`](https://iperf.fr), [`jq`](https://stedolan.github.io/jq/) and most importantly [Go](https://go.dev/doc/install) installed locally to run this tutorial.

## Install locally

You are good to go. No installation steps required.

## Install on Kubernetes

Install it in case you would like to benchmark your Kubernetes setup. If you want to benchmark locally skip this step.
Note that the benchmarking script does not support the standalone deployment.
Install the STUNner Gateway operator and STUNner ([more info](https://github.com/l7mp/stunner-helm)):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator -create-namespace --namespace=stunner-system
helm install stunner stunner/stunner -create-namespace --namespace=stunner
```

Configure STUNner to act as a STUN server towards [`turncat`](../turncat/README.md) clients, and to let `iperf` client's traffic reach the `iperf` server.

```
kubectl apply -f iperf-server.yaml
kubectl apply -f performance-stunner.yaml
```

## Executing measurements

### Helper script parameters

We bundle a helper script for executing performance measurements. The script uses optional arguments. The flags are the following: 
* `-h` Show help text
* `-n` Number of `turncat` clients (more of them can be used, this way each client will forward lesser traffic and none of them becomes the bottleneck while measuring)
* `-t` Time in seconds to transmit for
* `-s` Size of the packet in bytes
* `-b` Bandwidth to send in bits/sec
* `-p` Platform, can be `local` or `k8s`

```
./benchmark.sh -n 5 -t 5 -s 1000 -b 100000 -p k8s
```

### Performance measuring locally without Kubernetes

The below command will open:
* a `stunnerd` UDP listener at `127.0.0.1:5001`
* one or more `turncat` clients at `127.0.0.1:90XY` (90XY are ports used for measurement purposes starting from 9000) to open a connection through STUNner to the iperf server
* an `iperf` server at `127.0.0.1:5000`
* an `iperf` client sending its traffic to the turn

An example for:
* a local benchmark
* 5 turncats
* 5 seconds of evaluation time
* 1000 byte packets
* 100 Mbits/sec
```
./benchmark.sh -n 5 -t 5 -s 1000 -b 100000000 -p local
```
### Performance measuring with Kubernetes

The below command will open:
* one or more `turncat` clients at `127.0.0.1:90XY` (90XY are ports used for measurement purposes starting from 9000) to open a connection through STUNner to the iperf server. Traffic will be forwared to the STUNner public address obtained from STUNner configuration
* an `iperf` client sending its traffic to the turn

`STUNner` and `iperf` are running inside the Kubernetes Cluster.

An example for:
* a Kubernetes benchmark
* 5 turncats
* 5 seconds of evaluation time
* 1000 byte packets
* 100 Mbits/sec
```
./benchmark.sh -n 5 -t 5 -s 1000 -b 100000000 -p k8s
```

## Expected output / Getting the results

The output will be a standard `iperf` server output trimmed to show only the results. There are per-connections results and summarized results.

* **Per-connection results:** The number of connections are set with the `-n` argument of the helper script. Rows starting like `[  4]` show per-connection results (e.g., throughput, jitter, and latency).
* **Summarized results:** The `[SUM]` row summarizes the amount of transfered data, the effective bandwidth, the rate of the dropped/lost packets and the total number of sent packets and finally the packets/second (pps) rate.

Next, we see an example output for a local measurement and a measurement in Kubernetes.

### Local measurment

In a local measurement the output contains a single summarized test.
You should see a similar output:
```
Number of concurrent turncat clients: 5
Evaluation time: 1 sec
Packet size: 1000 bytes
Bandwidth: 10000 Kbits/sec or 10 Mbits/sec per turncat client
Platform: local
Results
[ ID] Interval            Transfer     Bandwidth        Jitter   Lost/Total  Latency avg/min/max/stdev PPS NetPwr
[  4] 0.0000-0.9977 sec  1.19 MBytes  10.0 Mbits/sec   0.038 ms 3/1253 (0.24%) 0.061/0.020/3.622/0.161 ms 1253 pps 20465
[  1] 0.0000-0.9992 sec  1.19 MBytes  10.0 Mbits/sec   0.049 ms 0/1253 (0%) 0.107/0.021/3.708/0.186 ms 1254 pps 11770
[  3] 0.0000-0.9980 sec  1.19 MBytes  10.0 Mbits/sec   0.003 ms 0/1253 (0%) 0.040/0.023/2.927/0.145 ms 1256 pps 31217
[  2] 0.0000-0.9989 sec  1.19 MBytes  10.0 Mbits/sec   0.042 ms 1/1253 (0.08%) 0.115/0.024/1.391/0.091 ms 1253 pps 10864
[  5] 0.0000-0.9977 sec  1.19 MBytes  10.0 Mbits/sec   0.003 ms 1/1253 (0.08%) 0.036/0.023/1.818/0.097 ms 1255 pps 34840
[ ID] Interval            Transfer     Bandwidth      Write/Err  PPS
[SUM] 0.0000-0.9985 sec  5.97 MBytes  50.2 Mbits/sec  5/6265     6269 pps
```

### Kubernetes measurment

In case of a Kubernetes measurement, the output contains one or more summarized tests. In case the user reruns the script, `iperf` outputs will be appended. You should see an output similar to this:
```
Number of concurrent turncat clients: 5
Evaluation time: 1 sec
Packet size: 1000 bytes
Bandwidth: 1000 Kbits/sec or 1 Mbits/sec per turncat client
Platform: k8s
Results
[ ID] Interval            Transfer     Bandwidth        Jitter   Lost/Total  Latency avg/min/max/stdev PPS  inP NetPwr
[  3] 0.0000-4.9589 sec   613 KBytes  1.01 Mbits/sec   0.756 ms    0/  628 (0%)  7.780/ 7.094/56.286/ 4.166 ms  127 pps  985 Byte 16.28
[  4] 0.0000-4.9592 sec   613 KBytes  1.01 Mbits/sec   0.623 ms    0/  628 (0%)  7.739/ 7.121/56.342/ 4.182 ms  127 pps  980 Byte 16.36
[  6] 0.0000-4.9579 sec   613 KBytes  1.01 Mbits/sec   0.593 ms    0/  628 (0%)  8.019/ 7.363/57.748/ 4.300 ms  127 pps 1016 Byte 15.80
[  5] 0.0000-4.9577 sec   613 KBytes  1.01 Mbits/sec   0.769 ms    0/  628 (0%)  7.949/ 7.222/58.249/ 4.400 ms  127 pps 1007 Byte 15.94
[  7] 0.0000-4.9600 sec   613 KBytes  1.01 Mbits/sec   1.017 ms    0/  628 (0%)  7.886/ 7.230/55.612/ 4.098 ms  127 pps  998 Byte 16.06
[SUM] 0.0000-4.9643 sec  2.99 MBytes  5.06 Mbits/sec   0.000 ms    0/ 3140 (0%)
[ ID] Interval            Transfer     Bandwidth        Jitter   Lost/Total  Latency avg/min/max/stdev PPS  inP NetPwr
[  8] 0.0000-4.9594 sec   625 KBytes  1.03 Mbits/sec   0.078 ms    0/  628 (0%)  8.837/ 7.591/56.173/ 6.396 ms  129 pps 1.11 KByte 14.60
[  6] 0.0000-4.9435 sec   604 KBytes  1.00 Mbits/sec   0.112 ms    9/  628 (1.4%)  7.554/ 7.313/11.592/ 0.385 ms  125 pps  946 Byte 16.58
[  4] 0.0000-4.9519 sec   605 KBytes  1.00 Mbits/sec   0.091 ms    8/  628 (1.3%)  7.950/ 7.735/11.985/ 0.371 ms  125 pps  995 Byte 15.75
[  3] 0.0000-4.9594 sec   613 KBytes  1.01 Mbits/sec   0.081 ms    0/  628 (0%)  8.339/ 7.792/56.527/ 4.156 ms  127 pps 1.03 KByte 15.18
[  5] 0.0000-4.9441 sec   604 KBytes  1.00 Mbits/sec   0.123 ms    9/  628 (1.4%)  7.538/ 7.320/11.560/ 0.363 ms  125 pps  944 Byte 16.61
[SUM] 0.0000-4.9597 sec  2.98 MBytes  5.04 Mbits/sec   0.000 ms   14/ 3140 (0.45%)
[SUM] 0.0000-4.9597 sec  12 datagrams received out-of-order
```

Notice that the average packets/second rate will be slightly lower in case of a hosted Kubernetes cluster than in case of a local `STUNner` installation.

## Tips and Tricks

* It is advised to repeat the measurment with different packet sizes.

Recommended packet sizes in bytes are 64, 128, 256, 512, 1024, and 1200.

**Effect of packet sizes:** With smallish packets (e.g., 64B), the average packets/second rate will be higher than with largish packets (e.g., 1200B). Small packet sizes result lower effective throughput (when packet drop is < 1%). You should definitely change the arguments to test the performance of your setup ideally.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
