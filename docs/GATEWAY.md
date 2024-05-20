# Reference

The [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) exposes the control plane configuration using the standard [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io). This allows to configure STUNner in the familiar YAML-engineering style via Kubernetes manifests. The below reference gives an overview of the subset of the Gateway API supported by STUNner, see [here](https://github.com/l7mp/stunner-gateway-operator#caveats) for a list of the most important simplifications.

1. [GatewayClass](#gatewayclass)
1. [GatewayConfig](#gatewayconfig)
1. [Gateway](#gateway)
1. [UDPRoute](#udproute)
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

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `dataplane` | `string` | The name of the Dataplane template to use for provisioning `stunnerd` pods. Default: `default`. | No |
| `logLevel` | `string` | Logging level for the dataplane pods. Default: `all:INFO`. | No |
| `realm` | `string` | The STUN/TURN authentication realm to be used for clients to authenticate with STUNner. The realm must consist of lower case alphanumeric characters or `-` and must start and end with an alphanumeric character. Default: `stunner.l7mp.io`. | No |
| `authRef` | `reference` | Reference to a Secret (`namespace` and `name`) that defines the STUN/TURN authentication mechanism and the credentials. | No |
| `authType` | `string` | Type of the STUN/TURN authentication mechanism. Valid only if `authRef` is not set. Default: `static`. | No |
| `userName` | `string` | The username for [`static` authentication](AUTH.md). Valid only if `authRef` is not set. | No |
| `password` | `string` | The password for [`static` authentication](AUTH.md). Valid only if `authRef` is not set. | No |
| `sharedSecret` | `string` | The shared secret for [`ephemeral` authentication](AUTH.md). Valid only if `authRef` is not set. | No |
| `authLifetime` | `int` | The lifetime of [`ephemeral` authentication](AUTH.md) credentials in seconds. Not used by STUNner.| No |
| `loadBalancerServiceAnnotations` | `map[string]string` | A list of annotations that will go into the LoadBalancer services created automatically by STUNner to obtain a public IP address. See more detail [here](https://github.com/l7mp/stunner/issues/32). | No |

At least a valid username/password pair *must* be supplied for `static` authentication, or a `sharedSecret` for the `ephemeral` mode, either via an external Secret or inline in the GatewayConfig. External authentication settings override inline settings. Missing both is an error.

Except the TURN authentication realm, all GatewayConfig resources are safe for modification. That is, the `stunnerd` daemons know how to reconcile a change in the GatewayConfig without restarting listeners/TURN servers. Changing the realm, however, induces a *full* dataplane restart.

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
| `listeners` | `list` | The list of TURN listeners. | Yes |
| `addresses` | `list` | The list of manually hinted external IP addresses for the rendered service (only the first one is used). | No |

> [!WARNING]
>
> Gateway resources are *not* safe for modification. This means that certain changes to a Gateway will restart the underlying TURN server listener, causing all active client sessions to terminate.  The particular rules are as follows:
> - adding or removing a listener will start/stop *only* the TURN listener being created/removed, without affecting the rest of the listeners on the same Gateway;
> - changing the transport protocol, port or TLS keys/certs of an *existing* listener will restart the TURN listener but leave the rest of the listeners intact;
> - changing the TURN authentication realm will restart *all* TURN listeners.

### Listener configuration

Each TURN `listener` is defined by a unique name, a transport protocol and a port. In addition, a `tls` configuration is required for TURN-TLS and TURN-DTLS listeners. Per-listener configuration is as follows.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `name` | `string` | Name of the TURN listener. Must be unique per Gateway. | Yes |
| `port` | `int` | Network port for the TURN listener. | Yes |
| `protocol` | `string` | Transport protocol for the TURN listener. Either TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS. | Yes |
| `tls` | `object` | [TLS configuration](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig).| Yes (for TURN-TLS/TURN-DTLS) |
| `allowedRoutes.from` | `object` | [Route attachment policy](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.AllowedRoutes), either `All`, `Selector`, or `Same`. Default: `Same`. | No |

For TURN-TLS/TURN-DTLS listeners, `tls.mode` must be set to `Terminate` or omitted (`Passthrough` does not make sense for TURN), and `tls.certificateRefs` must be a [reference to a Kubernetes Secret](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig) of type `tls` or `opaque` with exactly two keys: `tls.crt` must hold the TLS PEM certificate and `tls.key` must hold the TLS PEM key.

### Load balancer configuration

STUNner will automatically generate a Kubernetes LoadBalancer service to expose each Gateway to clients. All TURN listeners specified in the Gateway are wrapped by a single Service and will be assigned a single externally reachable IP address. If you want multiple TURN listeners on different public IPs, create multiple Gateways. TURN over UDP and TURN over DTLS listeners are exposed as UDP services, TURN-TCP and TURN-TLS listeners are exposed as TCP.

STUNner implements two ways to customize the automatically created Service, both involving certain per-defined [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations) added to the Service.  This is useful to, e.g., specify health-check settings for the Kubernetes load-balancer controller. The special annotation `stunner.l7mp.io/service-type` can be used to customize the type of the Service created by STUNner. The value can be either `ClusterIP`, `NodePort`, or `LoadBalancer` (this is the default); for instance, setting `stunner.l7mp.io/service-type: ClusterIP` will prevent STUNner from exposing a Gateway publicly (useful for testing).

By default, each key-value pair set in the GatewayConfig `loadBalancerServiceAnnotations` field will be copied verbatim into the Service. Service annotations can be customized on a per-Gateway basis as well, by adding the corresponding annotations to a Gateway resource. STUNner copies all annotations from the Gateway into the Service, overwriting the annotations specified in the GatewayConfig on conflict.

Manually hinted external address describes an address that can be bound to a Gateway. It is defined by an address type and an address value. Note that only the first address is used. Setting the `spec.addresses` field in the Gateway will result in the rendered Service's [loadBalancerIP](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=non%20%27LoadBalancer%27%20type.-,loadBalancerIP,-string) and [externalIPs](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=and%2Dservice%2Dproxies-,externalIPs,-string%20array) fields to be set.

| Field   | Type     | Description                                                   | Required |
|:--------|:--------:|:--------------------------------------------------------------|:--------:|
| `type`  | `string` | Type of the address. Currently only `IPAddress` is supported. | Yes      |
| `value` | `string` | Address that should be bound to the Gateway's service.        | Yes      |

> [!WARNING]
>
> Be careful when using this feature. Since Kubernetes v1.24 the `loadBalancerIP` field is deprecated and it will be ignored if the cloud-provider or your Kubernetes install do not support the feature. In addition, the `externalIPs` field is denied by some cloud-providers.

Currently, STUNner limits each Gateway to a single transport protocol, e.g., UDP or TCP. This is intended to improve the consistency across the Kubernetes services of different cloud providers, which provide varying support for [mixed multi-protocol LoadBalancer Services](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types). If you still want to expose a UDP and a TCP port on the same IP using a single Gateway, add the annotation `stunner.l7mp.io/enable-mixed-protocol-lb: true` to the Gateway.

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

> [!WARNING]
>
> Since mixed-protocol LB support is not supported in many popular Kubernetes offerings, STUNner currently defaults to disabling this feature. You can enable mixed-protocol LBs by annotating a Gateway with the `stunner.l7mp.io/enable-mixed-protocol-lb: true` key-value pair.

Some use cases require maintaining the source IP address of the client for correct operation. For the intended use case of STUNner, as an ingress media gateway exposing the cluster's media services over the TURN protocol, this does not matter. However, some users wish to use STUNner as a STUN server, which requires the original source IP to be retained by the load balancer. This can be achieved by adding the annotation `stunner.l7mp.io/external-traffic-policy: local` to a Gateway. This will set the [`service.spec.externalTrafficPolicy`](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip) field in the Service created by STUNner for the Gateway to `local`, which will instruct Kubernetes to preserve the original source IP address in clients' packets.

> [!WARNING]
>
> This feature comes with fairly complex [limitations](https://kubernetes.io/docs/tutorials/services/source-ip), use it at your own [risk](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#caveats-and-limitations-when-preserving-source-ips).

### Manually provisioning the dataplane

In some cases it may be useful to manually provision a dataplane for a Gateway, e.g., to deploy `stunnerd` in a DeamonSet instead of a Deployment. Adding the annotation `stunner.l7mp.io/disable-managed-dataplane: true` to a Gateway will prevent STUNner from spawning a dataplane for the Gateway. This then allows one to manually create a `stunnerd` dataplane and connect it to the CDS server exposed by the operator to obtain the dataplane configuration. Remove the annotation to revert to the default mode and let STUNner to manage the dataplane for the Gateway.

> [!WARNING]
>
> Manually provisioning a dataplane for a Gateway requires intimate knowledge with the STUNner internals, use this feature only if you know what you are doing.

The below table summarizes the Gateway annotations supported by STUNner.

| Key/value                                           | Description                                                                                                                                                | Default        |
|:----------------------------------------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------|:--------------:|
| `stunner.l7mp.io/service-type: <svc-type>`          | [Type of the Service](https://kubernetes.io/docs/concepts/services-networking/service) per Gateway, either `ClusterIP`, `NodePort`, or `LoadBalancer`.     | `LoadBalancer` |
| `stunner.l7mp.io/enable-mixed-protocol-lb: <bool>`  | [Mixed protocol load balancer service](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types) support. | False          |
| `stunner.l7mp.io/disable-managed-dataplane: <bool>` | Switch managed-dataplane support off for a Gateway                                                                                                         | False          |

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
| `namespace` | `string` | Namespace of the backend Service or StaticService. | Yes |
| `port` | `int` | Port to use to reach the backend. If empty, make all ports available on the backend. Default: empty.| No |
| `endPort` | `int` | If port is also specified, then access to the backend is restricted to the port range [port, endPort] inclusive. If port and endPort are empty, make all ports available on the backend. If port is given but endPort is not, admit the singleton port range [port,port]. Default: empty.| No |

UDPRoute resources are safe for modification: `stunnerd` knows how to reconcile modified routes without restarting any listeners/TURN servers.

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

The StaticService `spec.prefixes` must be a list of proper IPv4 prefixes: any IP address in any of the listed prefixes will be whitelisted. Use the single prefix `0.0.0.0/0` to provide wildcard access via an UDPRoute.

> [!WARNING]
>
> Never use StaticServices to access Services running *inside* Kubernetes, this may open up an unintended backdoor to your cluster. Use StaticServices only with *external* target backends.

## Dataplane

The Dataplane resource is used as a template for provisioning [`stunnerd`](/cmd/stunnerd/README.md) dataplane pods that implement TURN media ingestion. This is useful to choose the `stunnerd` image origin and version, set custom command line arguments and environment variables, configure resource requests/limits, etc.

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
  - --udp-thread-num=16
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

| Field                           | Type       | Description                                                                                                                                                                                                   | Required |
|:--------------------------------|:----------:|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:--------:|
| `image`                         | `string`   | The container image.                                                                                                                                                                                          | Yes      |
| `imagePullPolicy`               | `string`   | Policy for if/when to pull the [`stunnerd`](/cmd/stunnerd/README.md), either `Always`, `Never`, or `IfNotPresent`. Default: `Always` if the `latest` tag is specified on the image, `IfNotPresent` otherwise. | No       |
| `command`                       | `list`     | Entrypoints for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container).                                                                                                                   | No       |
| `args`                          | `list`     | Command line arguments for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container).                                                                                                        | No       |
| `envFrom`                       | `list`     | List of sources to populate environment variables for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container). Default: empty.                                                             | No       |
| `env`                           | `list`     | List of environment variables for the [dataplane container](https://pkg.go.dev/k8s.io/api/core/v1#Container). Default: empty.                                                                                 | No       |
| `containerSecurityContext`      | `object`   | Container-level security attributes for the [dataplane](/cmd/stunnerd/README.md) pods. Default: none.                                                                                                         | No       |
| `replicas`                      | `int`      | Number of dataplane pods per Gateway to provision. Not enforced if the [dataplane](/cmd/stunnerd/README.md) Deployment replica count is overwritten manually or by an autoscaler. Default: 1.                 | No       |
| `imagePullSecrets`              | `list`     | List of Secret references to use for pulling the `stunnerd` image. Each ref is a secret name, namespace is the same as that of the Gateway on behalf of which the dataplane is deployed.                      | No       |
| `hostNetwork`                   | `bool`     | Deploy the [dataplane](/cmd/stunnerd/README.md) into the host network namespace of Kubernetes nodes. Useful for implementing headless TURN services. May require elevated privileges. Default: false.         | No       |
| `resources`                     | `object`   | Compute resources per [dataplane](/cmd/stunnerd/README.md) pod. Default: none.                                                                                                                                | No       |
| `affinity`                      | `object`   | Scheduling constraints for the [dataplane](/cmd/stunnerd/README.md) pods. Default: none.                                                                                                                      | No       |
| `tolerations`                   | `object`   | Tolerations for the [dataplane](/cmd/stunnerd/README.md) pods. Default: none.                                                                                                                                 | No       |
| `securityContext`               | `object`   | Pod-level security attributes for the [dataplane](/cmd/stunnerd/README.md) pods. Default: none.                                                                                                               | No       |
| `topologySpreadConstraints`     | `object`   | Description of how the [dataplane](/cmd/stunnerd/README.md) pods for a Gateway ought to spread across topology domains. Default: none.                                                                        | No       |
| `disableHealthCheck`            | `bool`     | Disable health-checking. If true, enable HTTP health-checks on port 8086: liveness probe responder will be exposed on path `/live` and readiness probe on path `/ready`. Default: true.                       | No       |
| `enableMetricsEndpoint`         | `bool`     | Enable Prometheus metrics scraping. If true, a metrics endpoint will be available at `http://0.0.0.0:8080`. Default: false.                                                                                   | No       |
| `terminationGracePeriodSeconds` | `duration` | Optional duration in seconds for `stunnerd` to terminate gracefully. Default: 30 seconds.                                                                                                                     | No       |

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
