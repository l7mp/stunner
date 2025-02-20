# Premium features

[STUNner](https://github.com/l7mp/stunner) comes in multiple flavors. The open-source [*STUNner free* tier](/README.md) will suit most simple applications. Certain advanced features are however available only in STUNner's paid premium editions. The *STUNner member* tier includes the premium features targeted for small and medium scale operations, while the fully featured *STUNner enterprise* tier is optimized for large-scale deployments.

This guide helps you configure STUNner's premium features; see [here](https://l7mp.io/#/products) for up-to-date info on how to purchase a license.

## Getting started

Start with a fresh Kubernetes cluster. Remove all previous STUNner installations, otherwise some premium features may not be available.

Use the below Helm chart to deploy the premium version of STUNner:

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner stunner/stunner-premium --create-namespace --namespace=stunner-system
```

We recommend you deploy STUNner into the `stunner-system` namespace, this simplifies configuration later. See the [installation guide](INSTALL.md) for more info on customization options for the Helm chart.

## License validation

In order to unlock the premium features, STUNner will need a valid customer key. You should have received one during the subscription procedure; if not, [contact us](mailto:info@l7mp.io). STUNner will search for the customer key in the Kubernetes Secret named `stunner-gateway-operator-customer-secret` in the namespace where you deployed STUNner (usually `stunner-system`).

1. Set your customer key: The first step is to update the default Secret created by the Helm chart with your customer key. The simplest way is the manually edit the Secret:

   ```console
   EDITOR=nano kubectl -n stunner-system edit secret stunner-gateway-operator-customer-secret
   ```

   You should see something like the below (with some additional lines that you can safely ignore):

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: stunner-gateway-operator-customer-secret
     namespace: stunner-system
   type: Opaque
   data:
     CUSTOMER_KEY: X19kZWJ1Z19jdXNfUkZPU2FHMnNJNWtwNms=
   ```

   Rewrite `data.CUSTOMER_KEY` with your customer key. In order to prevent Kubernetes from base64-encoding the key, use the `stringData` field instead of `data`: `stringData.CUSTOMER_KEY`. Eventually you should see something like the below:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: stunner-gateway-operator-customer-secret
     namespace: stunner-system
   type: Opaque
   stringData:
     CUSTOMER_KEY: <YOUR-CUSTOMER-KEY>
   ```

   Don't forget to replace the placeholder `<YOUR_CUSTOMER_KEY>` with your own customer key.  Save and exit. If all goes well, `kubectl` should report that the secret has been successfully modified:

   ```
   secret/stunner-gateway-operator-customer-secret edited
   ```

   Alternatively, you can use the below to patch the Secret with your customer key:

   ```console
   kubectl -n stunner-system patch secret stunner-gateway-operator-customer-secret --type='json' \
      -p='[{"op": "add" ,"path": "/stringData" ,"value": {}}, {"op": "replace" ,"path": "/stringData/CUSTOMER_KEY" ,"value": "<YOUR_CUSTOMER_KEY>"}]'
   ```

2. Restart the operator: STUNner will read the customer key on startup so every time you update your customer key you have to restart the operator:

   ```console
   kubectl -n stunner-system rollout restart deployment stunner-gateway-operator-controller-manager
   ```

   If all goes well, the operator will validate the license associated with your customer key and unlock the premium features available in your tier.

3. Check your license status: The simplest way to check your license status is via the handy [`stunnerctl`](/cmd/stunnerctl/README.md) command line tool:

   ```console
   stunnerctl license
   License status:
      Subscription type: member
      Enabled features: DaemonSet, STUNServer, ...
      Last updated: ...
   ```

   This command will connect to your STUNner operator and report the license status. It will also report any errors encountered while validating your license.

   It is also possible to check the license status of STUNners's dataplane pods. The below [`stunnerctl`](/cmd/stunnerctl/README.md) command will connect to each dataplane pod deployed into the `stunner` namespace and report the running licensing status.

   ```console
   stunnerctl -n stunner status
   stunner/udp-gateway-...:
           admin:{id="stunner/udp-gateway",logLevel="all:INFO",health-check="http://:8086",quota=0,license-info={tier=enterprise,unlocked-features=TURNOffload,UserQuota,..,valid-until=...}}
           ...
   ```

   Look for the `license-info` field in the above admin status: you should see your subscription tier (e.g., `free`, `member` or `enterprise`) with all the available premium features in `unlocked-features` listed.

   If something goes wrong, check the gateway operator logs for lines like the below that should help you debug the problem:

   ```
   kubectl -n stunner-system logs $(kubectl -n stunner-system get pods -l \
       control-plane=stunner-gateway-operator-controller-manager -o jsonpath='{.items[0].metadata.name}')
   ...
   license-mgr     license manager client created  {"server": "https://license.l7mp.io:443", "customer-key-status": "set"}
   ...
   license-mgr     new license status      {"subscription-type": "enterprise", "enabled-features": ["DaemonSet", "UserQuota", "STUNServer", "TURNOffload"], "last-updated": "..."}
   ...
   ```

## Premium features

STUNner's premium features are designed to help medium to large scale enterprises to administer, scale and operate a fleet of STUN and TURN servers. Below is a list of the premium features currently available in STUNner.

### User quota (feature: `UserQuota`, available in the member and enterprise tiers)

Once a client has obtained a valid TURN authentication credential, they can open any number of TURN connections via STUNner by reusing the same credential. Since TURN credentials are available in plain text format at clients (this is by WebRTC API design), malicious clients can easily launch a Denial-of-Service (DoS) attack by creating many TURN allocations in quick succession. Unfortunately, even an [`ephemeral` credential](AUTH.md) leaves an open time window for a DoS attack before it expires.

STUNner's `UserQuota` feature allows to set an upper limit on the number of simultaneous allocations that can be made with the same TURN credential. This feature is available in your tier if `UserQuota` is enabled in the license status (recall, the status can be obtained using `stunnerctl license`).

Note that STUNner's quotas are per per-user-id. This means if that if you obtain multiple different credentials for the same user-id (e.g., by using `stunnerctl auth --username my-user`) then the credentials map to the same quota: the TURN allocations authenticated with the same credential add up when imposing the quota. Also note that stale TURN allocations also count towards the quota. If a client fails to close unused TURN allocations (which TURN clients routinely do) then these stale allocations will be active until they time out (usually 5 mins). This may prevent clients from re-connecting when an overly restrictive user quota is in effect.

Configure a user quota for a Gateway by setting the `userQuota` field in the corresponding GatewayConfig:

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
stunnerctl -n stunner status udp-gateway -o jsonpath='{.admin.quota}'
10
```

<!-- Alternatively, You can query the gateway's quota from the corresponding dataplane config: -->

<!-- ```console -->
<!-- stunnerctl -n stunner config udp-gateway -o jsonpath='{.admin.user_quota}' -->
<!-- 10 -->
<!-- ``` -->

Once the number of allocations created for a user-id reach the configured quota, new connections will be rejected with an `error 486: Allocation Quota Exceeded` error status.

### STUN server mode (feature: `STUNServer`, available in the member and enterprise tiers)

By default STUNner is configured to run as a TURN server. As TURN is an extension of the STUN protocol, this setting lets STUNner to serve plain [STUN requests](https://medium.com/l7mp-technologies/deploying-a-scalable-stun-service-in-kubernetes-c7b9726fa41d) as well. Running a TURN server, however, comes at a potentially high cost, typically needing a high-bandwidth network connection and consuming pricey CPU resources. This is suboptimal for the case when STUNner is deployed as a pure STUN service, since malicious clients can consume excess server resources by creating phony TURN allocations.

In order to prevent this potential DoS attack vector, STUNner's TURN protocol engine can be completely turned off. This makes prohibits clients from making new TURN allocations, but still guarantees that STUNner will serve STUN requests. This feature is available in your tier if `STUNServer` is enabled in the license status (recall, the status can be obtained using `stunnerctl license`).

To switch a Gateway into STUN server mode, set `STUNServer: true` in the corresponding GatewayConfig:

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
bin/stunnerctl -n stunner status udp-gateway -o jsonpath='{.auth.type}'
none
```

Set `STUNServer: false` to re-enable the TURN protocol engine.

### Deploying the dataplane in a DaemonSet (feature: `DaemonSet`, available in the member and enterprise tiers)

By default, the TURN server pods that run the dataplane for STUNner gateways are deployed into a Kubernetes Deployment. This ensures that a fix number of TURN servers are available per each Gateway. In certain cases, however, it may be desirable to deploy STUNner with a single dataplane pod per each Kubernetes node instead. This is crucial, for instance, when the STUNner dataplane is [deployed in the host-network namespace](https://github.com/l7mp/stunner/blob/main/docs/GATEWAY.md#dataplane) to run a [public TURN service](https://medium.com/l7mp-technologies/running-stunner-as-a-public-turn-server-1a2c61f78e67), or when a Gateway is exposed with the [`service.spec.externalTrafficPolicy: Local`](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip) configuration to implement [Direct Server Return](https://en.wikipedia.org/w/index.php?title=Load_balancing_(computing)#Load_balancer_features) for minimizing clients' round-trip-time.

To configure STUNner to run a single STUNner dataplane pod per each node in the Kubernetes cluster, you can set `spec.dataplaneResource` to `DaemonSet` in the [`Dataplane` resource](https://github.com/l7mp/stunner/blob/main/docs/GATEWAY.md#dataplane) corresponding to your Gateway. This will instruct STUNner to re-deploy the dataplane into a [Kubernetes DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset) instead of a Deployment. This feature is available in your tier if `DaemonSet` is enabled in the license status (recall, the status can be obtained using `stunnerctl license`).

The below will set the dataplane for all gateways using the `default` Dataplane to use a DaemonSet. The `hostNetwork: true` setting will deploy the TURN server pod in the host-network namespace of each Kubernetes node in a cluster.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: Dataplane
metadata:
  name: default
spec:
  dataplaneResource: DaemonSet
  hostNetwork: true
```

Set `dataplaneResource: Deployment` to return to the default deployment mode.

### TURN offload (available only in the enterprise tier)

To be available soon.
