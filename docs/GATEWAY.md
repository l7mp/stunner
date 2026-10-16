# Reference

The [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) exposes the control plane configuration using the standard [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io). This allows to configure STUNner in the familiar YAML-engineering style via Kubernetes manifests. The below reference gives an overview of the subset of the Gateway API supported by STUNner, see [here](https://github.com/l7mp/stunner-gateway-operator#caveats) for a list of the most important simplifications.

1. [GatewayClass](#gatewayclass)
1. [GatewayConfig](#gatewayconfig)
1. [Gateway](#gateway)
1. [UDPRoute](#udproute)
1. [TCPRoute](#tcproute)
1. [TURNRoute](#turnroute)
1. [StaticService](#staticservice)
1. [Dataplane](#dataplane)

## GatewayClass

The GatewayClass resource provides the root of a STUNner gateway configuration. GatewayClass resources are cluster-scoped, so they can be attached to from any namespace.

Below is a sample GatewayClass resource. Each GatewayClass specifies a controller that will manage the Gateway objects created under the class; this must be set to `stunner.l7mp.io/gateway-operator` for the STUNner gateway operator to pick up the GatewayClass. In addition, a GatewayClass can refer to further implementation-specific configuration via a `parametersRef`; in the case of STUNner this will always be a GatewayConfig object (see [below](#gatewayconfig)).

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: stunner-gatewayclass
spec:
  controllerName: "stunner.l7mp.io/gateway-operator"
  parametersRef:
    group: "stunner.l7mp.io"
    kind: GatewayConfig
    name: stunner-gatewayconfig
    namespace: stunner
  description: "STUNner is a WebRTC ingress gateway for Kubernetes"
```

Below is a quick reference of the most important fields of the GatewayClass [`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects).

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `controllerName` | `string` | Reference to the controller that is managing the Gateways of this class. The value of this field MUST be specified as `stunner.l7mp.io/gateway-operator`. | Yes |
| `parametersRef` | `object` | Reference to a GatewayConfig resource, identified by the `name` and `namespace`, for general STUNner configuration. The settings `group: "stunner.l7mp.io"` and `kind: GatewayConfig` are default and can be omitted, any other group or kind is an error. | Yes |
| `description` | `string` | Description helps describe a GatewayClass with more details. | No |

## GatewayConfig

The GatewayConfig resource provides general configuration for STUNner, most importantly the STUN/TURN authentication [credentials](AUTH.md) clients can use to connect to STUNner. GatewayClass resources attach a STUNner configuration to the hierarchy by specifying a particular GatewayConfig in the GatewayClass `parametersRef`.  GatewayConfig resources are namespaced, and every hierarchy can contain at most one GatewayConfig. Failing to specify a GatewayConfig is an error because the authentication credentials cannot be learned otherwise.

The following example takes the [STUNner authentication settings](AUTH.md) from the Secret called `stunner-auth-secret` in the `stunner` namespace, sets the authentication realm to `stunner.l7mp.io`, and sets the dataplane loglevel to `all:DEBUG,turn:INFO` (this will set all loggers to `DEBUG` level except the TURN protocol machinery's logger which is set to `INFO`).

```yaml
apiVersion: stunner.l7mp.io/v1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  logLevel: "all:DEBUG,turn:INFO"
  realm: stunner.l7mp.io
  authRef:
    name: stunner-auth-secret
    namespace: stunner
```

Below is a reference of the most important fields of the GatewayConfig [`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects)

| Field                            | Type                | Description                                                                                                                                                                                                                                    | Required |
|:---------------------------------|:-------------------:|:-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:--------:|
| `dataplane`                      | `string`            | The name of the Dataplane template to use for provisioning `stunnerd` pods. Default: `default`.                                                                                                                                                | No       |
| `logLevel`                       | `string`            | Logging level for the dataplane pods. Default: `all:INFO`.                                                                                                                                                                                     | No       |
| `realm`                          | `string`            | The STUN/TURN authentication realm to be used for clients to authenticate with STUNner. The realm must consist of lower case alphanumeric characters or `-` and must start and end with an alphanumeric character. Default: `stunner.l7mp.io`. | No       |
| `authRef`                        | `reference`         | Reference to a Secret (`namespace` and `name`) that defines the STUN/TURN authentication mechanism and the credentials.                                                                                                                        | No       |
| `authType`                       | `string`            | Type of the STUN/TURN authentication mechanism. Valid only if `authRef` is not set. Default: `static`.                                                                                                                                         | No       |
| `userName`                       | `string`            | The username for [`static` authentication](AUTH.md). Valid only if `authRef` is not set.                                                                                                                                                       | No       |
| `password`                       | `string`            | The password for [`static` authentication](AUTH.md). Valid only if `authRef` is not set.                                                                                                                                                       | No       |
| `sharedSecret`                   | `string`            | The shared secret for [`ephemeral` authentication](AUTH.md). Valid only if `authRef` is not set.                                                                                                                                               | No       |
| `authLifetime`                   | `int`               | The lifetime of [`ephemeral` authentication](AUTH.md) credentials in seconds. Not used by STUNner.                                                                                                                                             | No       |
| `loadBalancerServiceAnnotations` | `map[string]string` | A list of annotations that will go into the LoadBalancer services created automatically by STUNner to obtain a public IP address. See more detail [here](https://github.com/l7mp/stunner/issues/32).                                           | No       |
| `loadBalancerServiceLabels`      | `map[string]string` | A list of labels that will go into the LoadBalancer services created automatically by STUNner. Labels managed by STUNner take precedence. See more detail [here](#load-balancer-configuration).                                                | No       |
| `userQuota`                      | `int`               | Limit the number of allocations per username (**not supported in the free tier**). Default is 0, which means no quota.                                                                                                                         | No       |
| `stunMode`                       | `bool`              | Toggle STUN-only mode (**not supported in the free tier**). In this mode STUNner responds only to STUN requests; any TURN allocation request is rejected. Default is false.                                                                    | No       |

At least a valid username/password pair *must* be supplied for `static` authentication, or a `sharedSecret` for the `ephemeral` mode, either via an external Secret or inline in the GatewayConfig. External authentication settings override inline settings. Missing both is an error.

Except the TURN authentication realm, all GatewayConfig resources are safe for modification. That is, the `stunnerd` daemon knows how to reconcile a change in the GatewayConfig without restarting listeners/TURN servers. Changing the realm, however, restarts the TURN servers that use the auth config, terminating all active client sessions.

## Gateway

Gateways describe the STUN/TURN server listeners exposed to clients.

The below Gateway resource will configure STUNner to open a STUN/TURN listener over the UDP port 3478 and make it available on a public IP address and port to clients. Each Gateway will have a `stunnerd` Deployment that will run the dataplane and a LoadBalancer Service that will expose the gateway to the Internet, both using the same name and namespace as the Gateway. Once the Gateway is removed, the corresponding resources are automatically garbage-collected.

```yaml
apiVersion: gateway.networking.k8s.io/v1
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
```

The below example defines two TURN listeners: a TURN listener at the UDP:3478 port that accepts routes from any namespace (see below), and a TURN listener at port TLS/TCP:443 that accepts routes only from namespaces labeled with `app=dev`.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: complex-gateway
  namespace: stunner
  annotations:
    stunner.l7mp.io/service-type: NodePort
    stunner.l7mp.io/enable-mixed-protocol-lb: true
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-port: "8086"
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-protocol: "http"
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-path: "/live"
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: TURN-UDP
      allowedRoutes:
        namespaces:
          from: All
    - name: tls-listener
      port: 443
      protocol: TURN-TLS
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            namespace: stunner
            name: tls-secret
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels:
              app: dev
```

Below is a reference of the most important fields of the Gateway [`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects).

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `gatewayClassName` | `string` | The name of the GatewayClass that provides the root of the hierarchy the Gateway is attached to. | Yes |
| `listeners` | `list` | The list of listeners. | Yes |
| `addresses` | `list` | The list of manually hinted public addresses (only the first one is used).                                   | No |

> [!WARNING]
>
> Gateway resources are *not* safe for modification. This means that certain changes to a Gateway will restart the underlying TURN server listener, causing all active client sessions to terminate.  The particular rules are as follows:
> - adding or removing a listener will start/stop *only* the TURN listener being created/removed, without affecting the rest of the listeners on the same Gateway;
> - changing the transport protocol or port of an *existing* listener will restart the listener but leave the rest of the listeners intact; TLS keys and certificates apply to future connections without a restart;
> - changing the TURN authentication realm will restart *all* TURN servers.

Manually hinted public address describes an address that can be bound to a Gateway. It is defined by an address type, which can be either `IPAddress` (default) or `Hostname`, and an address value. Note that only the first address is used. Setting the `spec.addresses` field in the Gateway will enforce the use of that address all over STUNner as a public address for the gateway. If the address type specifies an IP address then that address will also be used in the Service created by STUNner to expose the Gateway as a [loadBalancerIP](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=non%20%27LoadBalancer%27%20type.-,loadBalancerIP,-string) and [externalIPs](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=and%2Dservice%2Dproxies-,externalIPs,-string%20array).

| Field   | Type     | Description                                                   | Required |
|:--------|:--------:|:--------------------------------------------------------------|:--------:|
| `type`  | `string` | Type of the address, either `IPAddress` (default) or `Hostname`.  | Yes      |
| `value` | `string` | Address that should be bound to the Gateway's service.        | Yes      |

> [!WARNING]
>
> Be careful when using this feature. Since Kubernetes v1.24 the `loadBalancerIP` field is deprecated and it will be ignored if your Kubernetes install does not support the feature. In addition, the `externalIPs` field is denied by some cloud-providers.

### Listener configuration

Each `listener` is defined by a unique name, a protocol and a port. The protocol decides what the listener serves: a TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS listener is a TURN server over the respective transport, while a plain UDP, TCP, TLS or DTLS listener is an L4 proxy that relays each client connection to a backend of its routes, choosing the backends in turn. In addition, a `tls` configuration is required for TLS and DTLS listeners of both kinds. Per-listener configuration is as follows.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `name` | `string` | Name of the listener. Must be unique per Gateway. | Yes |
| `port` | `int` | Network port for the listener. | Yes |
| `protocol` | `string` | Protocol of the listener: TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS for a TURN listener, UDP, TCP, TLS or DTLS for an L4 listener. | Yes |
| `tls` | `object` | [TLS configuration](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig).| Yes (for TLS/DTLS listeners) |
| `allowedRoutes.from` | `object` | [Route attachment policy](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.AllowedRoutes), either `All`, `Selector`, or `Same`. Default: `Same`. | No |

For TLS and DTLS listeners, `tls.mode` must be set to `Terminate` or omitted (STUNner terminates TLS, there is no `Passthrough`), and `tls.certificateRefs` must be a [reference to a Kubernetes Secret](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig) of type `tls` or `opaque` with exactly two keys: `tls.crt` must hold the TLS PEM certificate and `tls.key` must hold the TLS PEM key. The certificate may come from anywhere. If you use cert-manager's Gateway API support (cert-manager v1.21 or later with `--enable-gateway-api`, DNS-01 challenges only), a TLS listener gets its certificate without further setup, while TURN-TLS and TURN-DTLS listeners need cert-manager to accept these protocols with `--gateway-api-extra-protocols=TURN-TLS,TURN-DTLS`.

### Load balancer configuration

STUNner will automatically generate a Kubernetes LoadBalancer Service to expose each Gateway to clients. All TURN listeners specified in the Gateway are wrapped by a single Service and will be assigned a single externally reachable IP address. If you want multiple TURN listeners on different public IPs, create multiple Gateways. UDP and DTLS listeners (TURN or L4) are exposed as UDP services, TCP and TLS listeners as TCP.

STUNner implements several ways to customize the automatically created Service, each involving certain pre-defined [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations).  First, you can add global annotations to the `loadBalancerServiceAnnotations` field of the [GatewayConfig spec](#gatewayconfig), which affect the Service created for each Gateway that links to the GatewayConfig (via the GatewayClass `parametersRef`). To customize annotations on a per-gateway status, you can also add specific annotations to the Gateway itself. Gateway annotations override the global annotations on conflict. Finally, you can also add custom labels/annotations to the automatically created STUNner Services manually, these are retained on Service update (unless there is a conflict). Note that labels/annotations added via the Gateway and the GatewayConfig are also propagated to the corresponding `stunnerd` Deployments.

You can also add labels to the automatically created Services via the `loadBalancerServiceLabels` field of the [GatewayConfig spec](#gatewayconfig), for instance, to make the Services selectable by monitoring or network policy tooling. These labels are added to the Service of each Gateway that links to the GatewayConfig, but they are not propagated to the `stunnerd` Deployments. Labels managed by STUNner itself (the `stunner.l7mp.io/owned-by` and the `stunner.l7mp.io/related-gateway-*` labels) always take precedence: a conflicting label in the GatewayConfig is ignored. Unlike annotations, labels removed from the GatewayConfig are also removed from the Services. STUNner records the labels it has added in the `stunner.l7mp.io/managed-labels` annotation on the Service and only ever removes those, so labels added by other parties are never touched.

The following rules apply:
- Each annotation is copied verbatim into the Service created for any Gateway. This can be used, for instance, to specify health-check settings on the load-balancer Service (using the `service.beta.kubernetes.io/*-loadbalancer-healthcheck-*` annotations, see above).
- Annotations with the prefix `stunner.l7mp.io/...` have special meaning: apart from being copied into the Service these annotations also affect some specifics of the created Service, like the service type or the nodeports assigned to listeners.

STUNner defines the following special annotations:

1. **Service type:** The special annotation `stunner.l7mp.io/service-type` can be used to customize the type of the Service created by STUNner. The value can be either `ClusterIP`, `NodePort`, or `LoadBalancer` (this is the default); for instance, setting `stunner.l7mp.io/service-type: ClusterIP` will prevent STUNner from exposing a Gateway publicly (useful for testing).

1. **Mixed-protocol support:** Currently, STUNner limits each Gateway to a single transport protocol, e.g., UDP or TCP. This is intended to improve the consistency across the Kubernetes services of different cloud providers, which provide varying support for [mixed multi-protocol LoadBalancer Services](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types). If you still want to expose a UDP and a TCP port on the same IP using a single Gateway, add the annotation `stunner.l7mp.io/enable-mixed-protocol-lb: true` to the Gateway. Since mixed-protocol LB support is not supported in many popular Kubernetes offerings, STUNner currently defaults to disabling this feature.

   The below Gateway will expose both ports with their respective protocols.

   ```yaml
   apiVersion: gateway.networking.k8s.io/v1
   kind: Gateway
   metadata:
     name: mixed-protocol-gateway
     annotations:
       stunner.l7mp.io/enable-mixed-protocol-lb: true
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
       - name: udp-listener
         port: 3478
         protocol: TURN-UDP
       - name: tcp-listener
         port: 3479
         protocol: TURN-TCP
   ```

1. **Retaining the source IP:** Normally, Kubernetes load balancers apply source IP address translation when ingesting packets into the cluster. This replaces clients' original IP address with a private IP address. For STUNner's intended use case, as an ingress media gateway exposing the cluster's media services over the TURN protocol, this does not matter. However, STUNner can also act as a STUN server, which requires clients' source IP to be retained at the load balancer. This can be achieved by adding the annotation `stunner.l7mp.io/external-traffic-policy: local` to a Gateway, which will set the [`service.spec.externalTrafficPolicy`](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip) field in the Service created by STUNner for the Gateway to `Local`. Note that this Kubernetes feature comes with fairly complex [limitations](https://kubernetes.io/docs/tutorials/services/source-ip): if a STUN or TURN request hits a Kubernetes node that is not running a `stunnerd` pod, then the request will silently fail. This is required for Kubernetes to retain the client IP, which otherwise would be lost when passing packets between nodes. Use this setting at your own [risk](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#caveats-and-limitations-when-preserving-source-ips).

1. **Manually provisioning the dataplane:** In some cases it may be useful to manually provision a dataplane for a Gateway, e.g., to deploy `stunnerd` in a DaemonSet instead of a Deployment. Adding the annotation `stunner.l7mp.io/disable-managed-dataplane: true` to a Gateway will prevent STUNner from spawning a dataplane for the Gateway. This then allows one to manually create a `stunnerd` dataplane and connect it to the CDS server exposed by the operator to obtain the dataplane configuration. Remove the annotation to revert to the default mode and let STUNner to manage the dataplane for the Gateway. Manual dataplane provisioning requires intimate knowledge with the STUNner internals, use this feature only if you know what you are doing.

1. **Selecting the NodePort:** By default, Kubernetes assigns a random external port from the range [32000-32767] to each listener of a Gateway exposed with a NodePort Service. This requires all ports in the [32000-32767] range to be opened on the external firewall, which may raise security concerns for hardened deployments. In order to assign specific nodeports to particular listeners, add the annotation `stunner.l7mp.io/nodeport:` `{listener_name_1:nodeport_1,listener_name_2:nodeport_2,...}` to the Gateway, where each key-value pair is a name of a listener and the selected (numeric) NodePort. The value itself must be proper a JSON map. Unknown listeners are silently ignored. Note that STUNner makes no specific effort to reconcile conflicting NodePorts: whenever the selected NodePort is unavailable Kubernetes will silently reject the Service, which can lead to hard-to-debug failures. Use this feature at your own risk.

1. **Selecting the target port:** Some hardened Kubernetes deployments prohibit containers to open privileged ports (i.e., everything under 1024). This causes problems when one wants to ingest TURN over the standard TCP/TLS port 443. Kubernetes lets Services to choose an arbitrary [target port](https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service) for each service port, which makes it possible to map a particular external port to an arbitrary (potentially non-privileged) port in the containers. In order to enforce a particular target port per listener, add the annotation `stunner.l7mp.io/targetport:` `{listener_name_1:targetport_1,...}` to the corresponding Gateway (the syntax and semantics are the same as those for the nodeport annotation). For instance, the below Gateway would expose the TURN TCP/TLS on port 443, but it would map the egress port to the target port 44321 in the STUNner dataplane container:

   ```yaml
   apiVersion: gateway.networking.k8s.io/v1
   kind: Gateway
   metadata:
     name: tls-gateway
     annotations:
       stunner.l7mp.io/targetport: "{\"tls-listener\":44321}"
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
     - name: tls-listener
       port: 443
       protocol: TURN-TLS
       tls:
         certificateRefs:
           - kind: Secret
             namespace: stunner
             name: tls-secret
   ```

1. **Disabling the exposition of the health-check port:** Some older Kubernetes load-balancer providers required the exposition of the health-check port on LoadBalancer Services for UDP listeners to become externally reachable. Therefore, by default STUNner adds the health-check port (usually set via specific Gateway annotations) to the service-ports in automatically created LoadBalancer services. This has the unfortunate consequence that the health-check port becomes publicly reachable, which is considered a security issue by some, see https://github.com/l7mp/stunner-gateway-operator/issues/49. To prevent STUNner from exposing the health-check port, add the annotation `stunner.l7mp.io/disable-health-check-expose: true` to the corresponding Gateway. Note that this may cause TURN/UDP listeners unreachable on the Gateway, so use this only if you know that this setting will work with your Kubernetes provider.

1. **Disabling session affinity:** By default STUNner applies the `sessionAffinity: ClientIP` setting on the LB services it creates to expose Gateways. Normally this setting improves stability by ensuring that each TURN session is safely pinned to the same dataplane pod for its entire lifetime. Certain hosted Kubernetes platforms, however, seem to reject UDP LB services that have this setting on, [breaking STUNner deployments](https://github.com/l7mp/stunner/issues/155) on these systems. In order to prevent STUNner from enforcing session affinity on the LB Service corresponding to a Gateway, just set the `stunner.l7mp.io/disable-session-affinity: true` annotation on the Gateway. Otherwise, session affinity is turned on.

The below table summarizes the Gateway annotations supported by STUNner.

| Key/value                                           | Description                                                                                                                                                | Default        |
|:----------------------------------------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------|:--------------:|
| `stunner.l7mp.io/service-type: <svc-type>`          | [Type of the Service](https://kubernetes.io/docs/concepts/services-networking/service) per Gateway, either `ClusterIP`, `NodePort`, or `LoadBalancer`.     | `LoadBalancer` |
| `stunner.l7mp.io/enable-mixed-protocol-lb: <bool>`  | [Mixed protocol load balancer service](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types) support. | False          |
| `stunner.l7mp.io/external-traffic-policy: <string>` | Set the value to `Local` to preserve clients' source IP at the load balancer.                                                                              | `Cluster`      |
| `stunner.l7mp.io/disable-managed-dataplane: <bool>` | Switch managed-dataplane support off for a Gateway.                                                                                                        | False          |
| `stunner.l7mp.io/nodeport: <map>`                   | Request a specific NodePort for particular listeners. Value is a JSON map of listener-nodeport key-value pairs.                                            | None           |
| `stunner.l7mp.io/targetport: <map>`                 | Request a specific target port for particular listeners. Value is a JSON map of listener-targetport key-value pairs.                                       | None           |
| `stunner.l7mp.io/disable-health-check-expose: true` | Disable the default exposition of the health-check port (if any).                                                                                          | False          |
| `stunner.l7mp.io/disable-session-affinity: true`    | Disable session affinity for a Gateway.                                                                                                                    | False          |

## UDPRoute

UDPRoute resources can be attached to Gateways in order to specify the backend services permitted to be reached via the Gateway. Multiple UDPRoutes can attach to the same Gateway, and each UDPRoute can specify multiple backend services; in this case access to *all* backends in *each* of the attached UDPRoutes is allowed. An UDPRoute can be attached to a Gateway by setting the `parentRef` to the Gateway's name and namespace. This is, however, contingent on whether the Gateway accepts routes from the given namespace: customize the `allowedRoutes` per each Gateway listener to control which namespaces the listener accepts routes from.

The below UDPRoute will configure STUNner to route client connections received on the Gateway called `udp-gateway` to *any UDP port* on the pods of the media server pool identified by the Kubernetes service `media-server-pool` in the `media-plane` namespace.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
```

Note that STUNner provides its own UDPRoute resource instead of the official UDPRoute resource available in the Gateway API. In contrast to the official version, still at version v1alpha2, STUNner's UDPRoutes can be considered stable and expected to be supported throughout the entire lifetime of STUNner v1. You can still use the official UDPRoute resource as well, by changing the API version and adding an arbitrary port to the backend references (this is required by the official API). Note that the port will be omitted.

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
          port: 1
```

Below is a reference of the most important fields of the STUNner UDPRoute `spec`.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `parentRefs` | `list` | Name/namespace of the Gateways to attach the route to. If no namespace is given, then the Gateway will be searched in the UDPRoute's namespace. | Yes |
| `rules.backendRefs` | `list` | A list of backends (Services or StaticServices) reachable through the UDPRoute. It is allowed to specify a service from a namespace other than the UDPRoute's own namespace. | No |

Backend reference configuration is as follows:

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `group` | `string` | API group for the backend, either empty string for Service backends or `stunner.l7mp.io` for StaticService backends. Default: `""`. | No |
| `kind` | `string` | The kind of the backend resource, either `Service` or `StaticService`. Default: `Service`. | No |
| `name` | `string` | Name of the backend Service or StaticService. | Yes |
| `namespace` | `string` | Namespace of the backend Service or StaticService. | No |
| `port` | `int` | The port an L4 listener dials the backend at: without it an L4 listener has nothing to dial. Also the start of the port range of the backend, effective only with `endPort`: a `port` alone restricts nothing. Default: empty (every port).| No (Yes for L4 listeners) |
| `endPort` | `int` | End of the port range of the backend, if `port` is also specified: the dataplane drops traffic to the ports of the backend outside the range. Default: empty.| No |

UDPRoute resources are safe for modification: `stunnerd` knows how to reconcile modified routes without restarting any listeners/TURN servers.

> [!NOTE]
> Port ranges are ambiguous on a Gateway listener with several routes attached: the port range of the first route applies to every peer of the listener, so the peers of the other routes are reachable only on the ports the first route admits. 

## TCPRoute

TCPRoute is the TCP counterpart of the UDPRoute: same spec, same backend kinds, but the relay leg towards the backend is a TCP connection instead of a UDP flow. Use it when the backend service speaks TCP.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: TCPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: tcp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
```

The `spec` and the backend reference fields are identical to those of the [UDPRoute](#udproute). As with UDPRoutes, the official Gateway API TCPRoute resource can be used instead of STUNner's own; a same-name STUNner-native TCPRoute masks the official one.

There is no protocol conversion anywhere in STUNner: the protocol of the relay leg is fixed by the client's TURN allocation, so a TCP backend is reachable only over an [RFC 6062](https://datatracker.ietf.org/doc/html/rfc6062) TCP allocation. Browsers do not implement RFC 6062, so TCP backends cannot be reached from a browser's relay candidates.

> [!NOTE]
> Rendering a TCPRoute into a dataplane cluster is a [premium feature](PREMIUM_REFERENCE.md#tcp-routes). Without a license the operator still accepts your TCPRoutes and maintains their status, but no cluster is rendered for them and the `ResolvedRefs` status condition reports the reason.

TCPRoute resources are safe for modification, exactly like UDPRoutes.

## TURNRoute

TURNRoute routes the clients of a Gateway listener to their peers through an upstream TURN server, for instance another STUNner or a public TURN service. On a TURN listener the allocations of the clients are relayed through the upstream TURN server, and every peer is admitted: the upstream TURN server decides what its clients may reach. On a plain (L4) listener each client connection is relayed to a backend of the route through the upstream TURN server.

The below TURNRoute relays the allocations made at the `udp-listener` of the Gateway `udp-gateway` through the upstream TURN server `turn.example.com`, authenticating with the credentials in the Secret `upstream-auth`.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: TURNRoute
metadata:
  name: upstream-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
      sectionName: udp-listener
  tunnel:
    uri: "turns:turn.example.com:443?transport=tcp"
    authRef:
      name: upstream-auth
---
apiVersion: v1
kind: Secret
metadata:
  name: upstream-auth
  namespace: stunner
type: Opaque
stringData:
  type: static
  username: my-user
  password: my-password
```

Below is a reference of the TURNRoute `spec`.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `parentRefs` | `list` | Name/namespace of the Gateways to attach the route to, as for UDPRoutes. | Yes |
| `rules.backendRefs` | `list` | The backends (Services or StaticServices) a plain (L4) listener dials through the upstream TURN server, at the `port` of the backend reference. A TURN listener does not use them. | No |
| `tunnel.uri` | `string` | The upstream TURN server as an [RFC 7065](https://datatracker.ietf.org/doc/html/rfc7065) TURN URI, e.g., `turn:turn.example.com:3478?transport=udp`. | Yes |
| `tunnel.authRef` | `object` | Reference to the Secret holding the credentials presented to the upstream TURN server, possibly in another namespace. Default: no authentication. | No |
| `tunnel.insecure` | `bool` | Skip the verification of the certificate of the upstream TURN server over TLS and DTLS. Default: `false`. | No |
| `tunnel.sni` | `string` | The server name the certificate of the upstream TURN server is verified against. Default: the host of the URI. | No |

The Secret referenced by `tunnel.authRef` holds the key `type`, either `static` (the default) or `ephemeral`, the optional keys `realm` and `lifetime` (the validity of the generated ephemeral credentials, e.g., `1h`), and the credentials: `username` and `password` for static, `secret` for ephemeral authentication.

> [!NOTE]
> A TURNRoute is best the only route of its TURN listener: with other routes attached, the first route of the listener makes the relay sockets of every client, and the operator logs a warning.

The TURNRoute CRD is optional: the operator runs without it and ignores TURNRoutes until the CRD is installed (and lists TURNRoute among the supported route kinds of a listener only then). A new Helm install installs the CRD, but `helm upgrade` never updates CRDs: to use TURNRoutes on an upgraded install, apply the STUNner CRDs of the chart by hand and restart the operator.

## StaticService

When the target backend of a UDPRoute is running *inside* Kubernetes then the backend is always a proper Kubernetes Service. However, when the target is deployed *outside* Kubernetes then there is no Kubernetes Service that could be configured as a backend. This is particularly problematic in the cases when STUNner is used as a public TURN service. For such deployments, the StaticService resource provides a way to assign a routable IP address range to a UDPRoute.

The below StaticService represents a hypothetical Kubernetes Service backing a set of pods with IP addresses in the range `192.0.2.0/24` or `198.51.100.0/24`.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: StaticService
metadata:
  name: static-svc
  namespace: stunner
spec:
  prefixes:
    - "192.0.2.0/24"
    - "198.51.100.0/24"
```

Assigning this StaticService to a UDPRoute will make sure allows access to *any* IP address in the specified ranges.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - group: stunner.l7mp.io
          kind: StaticService
          name: static-svc
```

The StaticService `spec.prefixes` must be a list of proper IPv4 or IPv6 prefixes: any IP address in any of the listed prefixes will be whitelisted. An entry may restrict the ports of the prefix with a port range, written as `<prefix>:<min-max>` (e.g., `192.0.2.0/24:<10000-20000>`), which overrides the `port`/`endPort` of the backend reference. Note that a prefix admits only peers of its own address family, so use the prefix `0.0.0.0/0` to provide wildcard access via an UDPRoute, and add `::/0` next to it on an IPv6-only or dual-stack cluster.

> [!WARNING]
>
> Never use StaticServices to access Services running *inside* Kubernetes, this may open up an unintended backdoor to your cluster. Use StaticServices only with *external* target backends.

## Dataplane

The Dataplane resource is used as a template for provisioning [`stunnerd`](/docs/cmd/stunnerd.md) dataplane pods that implement TURN media ingestion. This is useful to choose the `stunnerd` image origin and version, set custom command line arguments and environment variables, configure resource requests/limits, etc.

Below is the `default` Dataplane installed by STUNner.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: Dataplane
metadata:
  name: default
spec:
  command:
  - stunnerd
  args:
  - -w
  image: l7mp/stunnerd:latest
  resources:
    limits:
      cpu: 2
      memory: 512Mi
    requests:
      cpu: 500m
      memory: 128Mi
  terminationGracePeriodSeconds: 3600
```

The following fields can be set in the Dataplane `spec` to customize the provisioning of `stunnerd` pods.

| Field                           | Type       | Description                                                                                                                                                                                                            | Required |
|:--------------------------------|:----------:|:-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:--------:|
| `image`                         | `string`   | The container image.                                                                                                                                                                                                   | Yes      |
| `imagePullPolicy`               | `string`   | Policy for if/when to pull the [`stunnerd`](/docs/cmd/stunnerd.md), either `Always`, `Never`, or `IfNotPresent`. Default: `Always` if the `latest` tag is specified on the image, `IfNotPresent` otherwise.          | No       |
| `imagePullSecrets`              | `list`     | List of Secret references to use for pulling the `stunnerd` image. Each ref is a secret name, namespace is the same as that of the Gateway on behalf of which the dataplane is deployed.                               | No       |
| `labels`                        | `map`      | Custom labels added to `stunnerd` pods. Mandatory labels override whatever is set here. Changing pod labels triggers full dataplane restart for the affected Gateways.                                                 | No       |
| `annotations`                   | `map`      | Custom annotations added to `stunnerd` pods. Mandatory annotations override whatever is set here, which in turn override manually added annotations. Changes trigger full dataplane restart for the affected Gateways. | No       |
| `command`                       | `list`     | Entrypoints for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container).                                                                                                                            | No       |
| `args`                          | `list`     | Command line arguments for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container).                                                                                                                 | No       |
| `envFrom`                       | `list`     | List of sources to populate environment variables for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container). Default: empty.                                                                      | No       |
| `env`                           | `list`     | List of environment variables for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container). Default: empty.                                                                                          | No       |
| `replicas`                      | `int`      | Number of dataplane pods per Gateway to provision. Not enforced if the [dataplane](/docs/cmd/stunnerd.md) Deployment replica count is overwritten manually or by an autoscaler. Default: 1.                          | No       |
| `resources`                     | `object`   | Compute resources per [dataplane](/docs/cmd/stunnerd.md) pod. Default: none.                                                                                                                                         | No       |
| `affinity`                      | `object`   | Scheduling constraints for the [dataplane](/docs/cmd/stunnerd.md) pods. Default: none.                                                                                                                               | No       |
| `tolerations`                   | `object`   | Tolerations for the [dataplane](/docs/cmd/stunnerd.md) pods. Default: none.                                                                                                                                          | No       |
| `securityContext`               | `object`   | Pod-level security attributes for the [dataplane](/docs/cmd/stunnerd.md) pods. Default: none.                                                                                                                        | No       |
| `containerSecurityContext`      | `object`   | Container-level security attributes for the [dataplane](/docs/cmd/stunnerd.md) pods. Default: none.                                                                                                                  | No       |
| `topologySpreadConstraints`     | `object`   | Description of how the [dataplane](/docs/cmd/stunnerd.md) pods for a Gateway ought to spread across topology domains. Default: none.                                                                                 | No       |
| `terminationGracePeriodSeconds` | `duration` | Optional duration in seconds for `stunnerd` to terminate gracefully. Default: 30 seconds.                                                                                                                              | No       |
| `hostNetwork`                   | `bool`     | Deploy the [dataplane](/docs/cmd/stunnerd.md) into the host network namespace of Kubernetes nodes. Useful for implementing headless TURN services. May require elevated privileges. Default: false.                  | No       |
| `disableHealthCheck`            | `bool`     | Disable health-checking. If true, enable HTTP health-checks on port 8086: liveness probe responder will be exposed on path `/live` and readiness probe on path `/ready`. Default: true.                                | No       |
| `enableMetricsEndpoint`         | `bool`     | Enable Prometheus metrics scraping. If true, a metrics endpoint will be available at `http://0.0.0.0:8080`. Default: false.                                                                                            | No       |
| `dataplaneResource`             | `string`   | Kubernetes resource type to be used for deploying the dataplane, either a Deployment (default) or DaemonSet (**not supported in the free tier**).                                                                      | No       |

There can be multiple Dataplane resources defined in a cluster, say, one for the production workload and one for development. Use the `spec.dataplane` field in the GatewayConfig to choose the Dataplane per each STUNner install.

> [!WARNING]
>
> A Dataplane resource called `default` must always be available in the cluster, otherwise the operator will not know how to provision dataplane pods. Removing the `default` template will break your STUNner installation.

<!-- ## Status -->

<!-- Most Kubernetes resources provide a `status` subresource that describes the current state of the resource. The status is supplied and updated by the Kubernetes system and its components. The Kubernetes control plane continually and actively manages every object's actual state to match the desired state you supplied and updates the status field to indicate whether any error was encountered during the reconciliation process. -->

<!-- If you are not sure about whether the STUNner gateway operator successfully picked up your Gateways or UDPRoutes, it is worth checking the status to see what went wrong. -->

<!-- ```console -->
<!-- kubectl get <resource> -n <namespace> <name> -o jsonpath='{.status}' -->
<!-- ``` -->
