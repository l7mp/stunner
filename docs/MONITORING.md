# Monitoring

STUNner can export various statistics into an external timeseries database like [Prometheus](https://prometheus.io). This allows observing the state of a STUNner gateway instance (such as CPU usage, memory consumption, and the volume of data received and sent) in near real-time. These statistics can then be presented to the operator in a monitoring dashboard using, e.g., [Grafana](https://grafana.com).

## Table of Contents

1. [Configuration](#configuration)
1. [Metrics](#metrics)

## Configuration

Metrics collection is *not* enabled by default. To enable it, set the `enableMetricsEndpoint` field to true in the [Dataplane](GATEWAYCLASS.md#dataplane) template. This will configure the [`stunnerd`](cmd/stunnerd.md) dataplane pods to expose an HTTP metrics endpoint on port 8080 that Prometheus can scrape for metrics.

## Metrics

STUNner exports two types of metrics: the *Go collector metrics* describe the state of the Go runtime, while the *Connection statistics* expose traffic monitoring data.

### Go collector metrics

Each STUNner gateway instance exports a number of standard metrics that describe the state of the current Go process. Some notable metrics are listed below; see more in the [documentation](https://github.com/prometheus/client_golang).

| Metric | Description |
| :--- | :--- |
| `process_cpu_seconds_total` | Total user and system CPU time spent in seconds.|
| `go_memstats_alloc_bytes` | Number of bytes allocated and still in use. |
| `go_goroutines` | Number of goroutines that currently exist. |
| `go_threads`  | Number of OS threads created. |
| `process_open_fds` | Number of open file descriptors.|
| `process_resident_memory_bytes` | Resident memory size in bytes. |
| `process_virtual_memory_bytes` | Virtual memory size in bytes. |

### Connection statistics

STUNner provides deep visibility into the amount of traffic sent and received on each listener (downstream connections) and cluster (upstream connections). The gateway operator names a listener `<gateway-namespace>/<gateway-name>/<listener-name>` and the cluster of a route `<route-namespace>/<route-name>/udp` (`tcp` for a TCPRoute). The particular metrics are as follows.

| Metric | Description | Type | Labels |
| :--- | :--- | :--- | :--- |
| `stunner_allocations_active` | Number of active allocations. | gauge | none |
| `stunner_build_info` | The version of the `stunnerd` binary, as labels; always 1. Quote it in an issue report. | gauge | `version`, `commit`, `build_date` |
| `stunner_listener_connections` | Number of *active* downstream connections at a listener. Stays constant when using only UDP listeners. | gauge | `name=<listener-name>` |
| `stunner_listener_connections_total` | Number of downstream connections at a listener. | counter | `name=<listener-name>` |
| `stunner_listener_packets_total` | Number of datagrams sent or received at a listener. Unreliable for listeners running on a connection-oriented transport protocol (TCP/TLS).  | counter | `direction=<rx\|tx>`, `name=<listener-name>`|
| `stunner_listener_bytes_total` | Number of bytes sent or received at a listener. | counter | `direction=<rx\|tx>`, `name=<listener-name>` |
| `stunner_cluster_packets_total` | Number of datagrams sent to backends or received from backends of a cluster.  Unreliable for clusters running on a connection-oriented transport protocol (TCP/TLS).| counter | `direction=<rx\|tx>`, `name=<cluster-name>` |
| `stunner_cluster_bytes_total` | Number of bytes sent to backends or received from backends of a cluster. | counter | `direction=<rx\|tx>`, `name=<cluster-name>` |

