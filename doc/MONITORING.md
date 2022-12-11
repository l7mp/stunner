# Monitoring

STUNner can export various statistics into an external timeseries database like
[Prometheus](https://prometheus.io). This allows one to observe the state of the STUNner media
gateway instances, like CPU or memory use, as well as the amount of data received and sent, in
quasi-real-time. Such statistics can then be presented to the operator in easy-to-use monitoring
dashboards in [Grafana](https://grafana.com).

## Configuration

Metrics collection is *not* enabled in the default installation. In order to open the
metrics-collection endpoint for a [gateway hierarchy](/doc/GATEWAY.md#overview), configure an
appropriate HTTP URL in the `metricsEndpoint` field of corresponding the
[GatewayConfig](/doc/GATEWAY.md#gatewayconfig) resource.

For instance, the below GatewayConfig will expose the metrics-collection server on the URL
`http://:8080/metrics` in all the STUNner media gateway instances of the current gateway hierarchy.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  userName: "my-user"
  password: "my-password"
  metricsEndpoint: "http://:8080/metrics"
```

## Metrics

STUNner exports two types of metrics: the *Go collector metrics* describe the state of the Go
runtime, while the *Connection statistics* expose traffic monitoring data.

### Go collector metrics

Each STUNner gateway instance exports a number of standard metrics that describe the state of the
current Go process runtime. Some notable metrics as listed below, see more in the
[documentation](https://github.com/prometheus/client_golang).

| Metric | Description |
| :--- | :--- |
| `process_cpu_seconds_total` | Total user and system CPU time spent in seconds.|
| `go_memstats_alloc_bytes` | Number of bytes allocated and still in use. |
| `go_goroutines` | Number of goroutines that currently exist. |
| `go_threads`  | Number of OS threads created. |
| `process_open_fds` | Number of open file descriptors.|
| `process_virtual_memory_bytes` | Virtual memory size in bytes. |

### Connection statistics

STUNner allows deep visibility into the amount of traffic sent and received on each listener
(downstream connections) and cluster (upstream connections). The particular metrics are as follows.

| Metric | Description | Type | Labels |
| :--- | :--- | :--- | :--- |
| `stunner_listener_connections` | Number of *active* downstream connections at a listener. | gauge | `name=<listener-name>` |
| `stunner_listener_connections_total` | Number of downstream connections at a listener. | counter | `name=<listener-name>` |
| `stunner_listener_packets_total` | Number of datagrams sent or received at a listener. Unreliable for listeners running on a connection-oriented a protocol (TCP/TLS).  | counter | `direction=<rx|tx>`, `name=<listener-name>`|
| `stunner_listener_bytes_total` | Number of bytes sent or received at a listener. | counter | `direction=<rx|tx>`, `name=<listener-name>` |
| `stunner_cluster_connections` | Number of *active* upstream connections on behalf of a listener. | gauge | `name=<listener-name>` |
| `stunner_cluster_connections_total` | Number of upstream connections on behalf of a listener. | counter | `name=<listener-name>` |
| `stunner_cluster_packets_total` | Number of datagrams sent to backends or received from backends on behalf of a listener.  Unreliable for clusters running on a connection-oriented a protocol (TCP/TLS).| counter | `direction=<rx|tx>`, `name=<listener-name>` |
| `stunner_cluster_bytes_total` | Number of bytes sent to backends or received from backends on behalf of a listener. | counter | `direction=<rx|tx>`, `name=<listener-name>` |

## Integration with Prometheus

## Integration with Grafana

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
