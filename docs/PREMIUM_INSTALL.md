# Getting started with STUNner premium tiers

Congratulations for subscribing to STUNner premium. You should have received a customer key during the subscription procedure; if not, [contact us](mailto:info@l7mp.io). Make sure to save your customer key to a safe place, you will need it every time you (re)deploy STUNner.

The below guide will walk you through unlocking STUNner's premium features.

## Getting started

[STUNner](https://github.com/l7mp/stunner) comes in multiple flavors. The open-source [*STUNner free* tier](/README.md) will suit most simple applications. Certain advanced features are however available only in STUNner's paid premium editions. The *STUNner member* tier includes the premium features targeted for small and medium scale operations, while the fully featured *STUNner enterprise* tier is optimized for large-scale deployments.

See [here](https://l7mp.io/#/products) for up-to-date info on how to purchase a license. 

## Installation

Start with a fresh Kubernetes cluster. Remove all previous STUNner installations, otherwise some premium features may not be available.

Use the below Helm chart to deploy the premium version of STUNner:

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner stunner/stunner-premium --create-namespace --namespace=stunner-system
```

We recommend you deploy STUNner into the `stunner-system` namespace, this simplifies configuration later. See the [installation guide](INSTALL.md) for more info on customization options for the Helm chart.

## License validation

In order to unlock the premium features, STUNner will need a valid customer key. You should have received one during the subscription procedure; if not, [contact us](mailto:info@l7mp.io). 

STUNner will search for the customer key in the Kubernetes Secret named `stunner-gateway-operator-customer-secret` in the namespace where you deployed STUNner (usually `stunner-system`).

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

Make sure to [contact us](mailto:info@l7mp.io) if something goes wrong.
