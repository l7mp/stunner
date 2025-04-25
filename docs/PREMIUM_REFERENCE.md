# Premium features

STUNner's premium features are designed to help medium to large scale enterprises to deploy, operate and scale pools of STUN and TURN servers over Kubernetes. Below is a reference of the premium features currently available in STUNner.

## Table of Contents

1. [User quota](#user-quota)
1. [STUN server mode](#stun-server-mode)
1. [Deploying into a DaemonSet](#deploying-into-a-daemonset)
1. [Relay address discovery](#relay-address-discovery)
1. [TURN offload](#turn-offload)

## User quota

**Feature:** `UserQuota`. **Availability:** member and enterprise tiers.

Once a client has obtained a valid TURN authentication credential, they can open any number of TURN connections by reusing the same credential. Since TURN credentials are available in plain text at clients (this is by WebRTC JavaScript API design), malicious clients can launch a Denial-of-Service (DoS) attack by creating excess of TURN allocations in quick succession. Unfortunately, even an [`ephemeral` credential](AUTH.md) leaves a time window open for a DoS attack before it expires.

STUNner's `UserQuota` feature allows to set an upper limit on the number of simultaneous allocations that can be made with the same TURN credential. This feature is available in your tier if the `UserQuota` feature is enabled in the license status (recall, the status can be obtained using [`stunnerctl license`](/docs/cmd/stunnerctl.md#license-status)).

Note that STUNner's quotas are per per-user-id. This means that if you obtain multiple different credentials for the same user-id (e.g., by using `stunnerctl auth --username my-user`) then the credentials map to the same quota: the TURN allocations authenticated with the same credential add up when imposing the quota. Also note that stale TURN allocations also count towards the quota. If a client fails to close unused TURN allocations (which TURN clients routinely do) then these stale allocations will be active until they time out (usually after 5 mins). This may prevent clients from re-connecting when an overly restrictive user quota is in effect. Quotas are per-dataplane-pod: if you [scale](SCALING.md) STUNner then the quota multiplies by as much as there are dataplane pods.

Configure a user quota for a Gateway by setting the `userQuota` field in the GatewayConfig:

```yaml
apiVersion: stunner.l7mp.io/v1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
spec:
  authType: ephemeral
  sharedSecret: my-shared-secret
  userQuota: 10
```

This will set the quota to 10. Setting the quota to zero means no quota (the default setting).

You can query the configured user quota using [`stunnerctl`](/cmd/stunnerctl/README.md). Suppose you deployed a TURN/UDP gateway called `udp-gateway` in the `stunner` namespace. The current quota can be obtained as follows:

```console
stunnerctl -n <gateway-namespace> status <gateway-name> -o jsonpath='{.admin.quota}'
10
```

Alternatively, You can query the gateway's quota from the corresponding dataplane config:

```console
stunnerctl -n stunner config udp-gateway -o jsonpath='{.admin.user_quota}' -->
10
```

Once the number of allocations created for a user-id reach the configured quota, new allocations will be rejected with an `error 486: Allocation Quota Exceeded` status.

## STUN server mode

**Feature:** `STUNServer`. **Availability:** member and enterprise tiers.

By default STUNner is configured to run as a TURN server. As TURN is an extension of the STUN protocol, this setting lets STUNner to serve plain [STUN requests](https://medium.com/l7mp-technologies/deploying-a-scalable-stun-service-in-kubernetes-c7b9726fa41d) as well. Running a TURN server, however, comes at a potentially high cost, typically needing a high-bandwidth network connection and consuming pricey CPU resources. This is suboptimal for the case when STUNner is deployed as a pure STUN service, since malicious clients can consume excess server resources by creating phony TURN allocations.

In order to prevent the potential DoS attack vector, STUNner's TURN protocol engine can be completely turned off. This prohibits clients from making TURN allocations, but still guarantees that STUNner will serve STUN requests. Pure STUN server mode is available in your tier if the `STUNServer` feature is enabled in the license status (recall, the status can be obtained using [`stunnerctl license`](/docs/cmd/stunnerctl.md#license-status)).

To switch a Gateway into STUN server mode, set `STUNServer: true` in the GatewayConfig:

```yaml
apiVersion: stunner.l7mp.io/v1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
spec:
  STUNMode: true
```

This will disable STUNner's authentication engine, prohibiting clients from creating TURN allocations all together:

```console
bin/stunnerctl -n <gateway-namespace> status <gateway-name> -o jsonpath='{.auth.type}'
none
```

Set `STUNServer: false` to re-enable the TURN protocol engine.

## Deploying into a DaemonSet

**Feature:** `DaemonSet`. **Availability:** member and enterprise tiers.

By default, the TURN server pods that run the dataplane for STUNner gateways are deployed into a Kubernetes Deployment. This ensures that a configurable number of TURN servers are available per each Gateway. In certain cases, however, it may be desirable to deploy STUNner with a single dataplane pod per each Kubernetes node instead. This is crucial, for instance, when the STUNner dataplane is [deployed in the host-network namespace](https://github.com/l7mp/stunner/blob/main/docs/GATEWAY.md#dataplane) to run a [public TURN service](https://medium.com/l7mp-technologies/running-stunner-as-a-public-turn-server-1a2c61f78e67), or when a Gateway is exposed with the [`service.spec.externalTrafficPolicy: Local`](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip) configuration to implement [direct server return](https://en.wikipedia.org/w/index.php?title=Load_balancing_(computing)#Load_balancer_features) to minimize clients' round-trip-time.

To configure STUNner to run a single STUNner dataplane pod per each node in the Kubernetes cluster, you can set `spec.dataplaneResource` to `DaemonSet` in the [`Dataplane` resource](https://github.com/l7mp/stunner/blob/main/docs/GATEWAY.md#dataplane) corresponding to your Gateway. This will instruct STUNner to re-deploy the dataplane into a [Kubernetes DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset) instead of a Deployment. DaemonSet mode is available in your tier if the `DaemonSet` feature is enabled in the license status (recall, the status can be obtained using [`stunnerctl license`](/docs/cmd/stunnerctl.md#license-status)).

The below will set the dataplane for all gateways using the `default` Dataplane to use a DaemonSet.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: Dataplane
metadata:
  name: default
spec:
  dataplaneResource: DaemonSet
  ...
```

Set `dataplaneResource: Deployment` to return to the default deployment mode.

## Relay address discovery

**Feature:** `RelayAddressDiscovery`. **Availability:** member and enterprise tiers.

STUNner was designed for a specific use case: ingest real-time media into a Kubernetes cluster and forward incoming connections to a pool of WebRTC media servers deployed into the same cluster. This, however, does not prevent you from leveraging STUNner for other purposes, like as a public TURN server, but this may need some tweaking.

Public TURN servers typically run on a public IP address, which makes it possible for both clients and peers to connect via the server. However, STUNner's TURN servers (the `stunnerd` pods) are by default deployed over private IPs. This is perfectly fine when peers, e.g., WebRTC media servers, are deployed into the same cluster (the pivotal use case for STUNner), but this pretty much makes it impossible to deploy STUNner as a public TURN server since peers will not be able to connect to the private IP of STUNner's TURN servers. (Note that ["symmetric ICE mode"](DEPLOYMENT.md#symmetric-ice-mode) would still work but it may increase STUNner's resource consumption.)

<!-- You can experiment with [deploying STUNner into the host-network namespace](GATEWAY.md#dataplane), but most of the time this would not solve the issue either because either Kubernetes will deploy pods running in host-network mode over a private IP address. -->

Relay address discovery, when enabled, will configure STUNner's TURN servers with a public IP address, if available. The public IP is obtained from the Kubernetes node the TURN server runs at. Relay address discovery is available in your tier if the `RelayAddressDiscovery` feature is enabled in the license status (recall, the status can be obtained using [`stunnerctl license`](/docs/cmd/stunnerctl.md#license-status)).

Below is the set of steps to enable relay address discovery:

1. Enable host-network mode.

   Host-networking will re-deploy the STUNner dataplane to run in the network namespace of Kubernetes nodes, so that it will have access to the node's public IP address (if any). You can enable host-networking by configuring `hostNetwork: true` in the Dataplane spec:

   ```yaml
   apiVersion: stunner.l7mp.io/v1
   kind: Dataplane
   metadata:
     name: default
   spec:
     dataplaneResource: DaemonSet
     hostNetwork: true
   ```

   Only enable host-networking with the `DaemonSet` feature also enabled, otherwise you will not be able to scale your TURN server pool.

2. Enable relay address discovery.

   Relay address discovery mode can be switched on by adding the `stunner.l7mp.io/enable-relay-address-discovery: "true"` annotation on a STUNner Gateway:

   ```console
   kubectl annotate --overwrite gateway <your-stunner-gateway> stunner.l7mp.io/enable-relay-address-discovery="true"
   ```

3. Check whether STUNner has successfully discovered the node's public IP.

   As usual, the [`stunnerctl`](/docs/cmd/stunnerctl.md) tool comes in handy. The below will load the dataplane configuration for the gateway `<gateway-namespace>/<gateway-name>` with respect to the node `<node-name>` and print the relay address of each TURN listener:

   ```console
   stunnerctl -n <gateway-namespace> config <gateway-name> --node=<node-name> -o jsonpath='{.listeners[*].address}'
   <relay-address>
   ...
   ```

   The `<relay-address>` above should be the node's public IP for `<node-name>`. You can also request the status directly from the dataplane pods, but in this case parsing the output requires a bit of getting used to:

   ```console
   stunnerctl -n <gateway-namespace> status <gateway-name>
   <gateway-namespace>/<gateway-pod>:
   admin: ...
   auth: ...
   listeners: <listener-name>:{turn://<relay-address>:<port>?transport=TURN-UDP...}
   clusters: ...
   ```

   Note that depending on your Kubernetes provider's platform your nodes may run without a public IP, or host-networking may not be available at all. Symmetric ICE mode is still be usable as a fallback in such cases.

## TURN offload

**Feature:** `TURNOffload`. **Availability:** only the enterprise tier.

User plane TURN message processing may be costly. To cut down CPU usage and latency, STUNner can offload TURN message processing to one of its Linux/eBPF-based kernel packet processing engines. The offload engines support TURN channel processing for UDP, and provide massive bandwidth, delay, and jitter performance boost and can cut down CPU usage by several orders of magnitude.  TURN acceleration is available in your tier if the `TURNOffload` feature is enabled in the license status (recall, the status can be obtained using [`stunnerctl license`](/docs/cmd/stunnerctl.md#license-status)).

STUNner's eBPF offload requires Kubernetes nodes running Linux and elevated privilege access to interact with the eBPF/tc (`TC`) or eBPF/XDP (`XDP`) kernel framework. Both provide outstanding performance: `TC` is supported in most Kubernetes environments (e.g., public clouds), while `XDP` is faster but it is typically limited to bare metal clusters.

To use the TURN offload feature of STUNner, set the `spec.offloadEngine` in the `Dataplane` custom resource: `TC` means eBPF/TC, `XDP` is eBPF/XDP, `None` falls back to user-space TURN processing, and `Auto` will let STUNner to pick the best offload engine for your platform. You can also manually configure the network interfaces on which STUNner will enable TURN offload via the `spec.offloadInterfaces` in the `Dataplane` spec. This parameter assumes a list of network interface names and an empty list means to enable offload on all interfaces (this is the default). To use eBPF offload, you must also enable elevated rights in your STUNner pods. To achieve this, edit the `spec.containerSecurityContext` field and add the necessary `NET_ADMIN`, `SYS_ADMIN`, `SYS_MODULE` capabilities.

The below will set the dataplane for all gateways using the `default` Dataplane to use the TURN offload on all available network interfaces and select the optimal offload mode.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: Dataplane
metadata:
  name: default
spec:
  containerSecurityContext:
    capabilities:
      add:
      - NET_ADMIN
      - SYS_ADMIN
      - SYS_MODULE
  offloadEngine: Auto
```

The simplest way to test whether eBPF offload was successfully enabled is to deploy a simple STUNner tutorial (like the [iperf-test](/docs/examples/simple-tunnel/README.md) example) and watch for the offload statistics in the output or [`stunnerctl status`](/docs/cmd/stunnerctl.md#status):

```console
stunnerctl -n <gateway-namespace> status <gateway-name>
<gateway-namespace>/<gateway-name>:
	admin: ...
	static-auth: ...
	listeners: <listener-name>:{...},offload(rx/tx): 1152/345 pkts 168192/19834 bytes
	clusters: ...
	allocs:1/status=READY
```
