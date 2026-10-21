# GatewayClass, GatewayConfig and Dataplane

These resources set up a STUNner installation: the GatewayClass is the root of the configuration
hierarchy, the GatewayConfig holds the settings shared by every Gateway of the class (most
importantly the TURN authentication credentials), and the Dataplane is the template of the
`stunnerd` pods. The [Gateway](GATEWAY.md) and the [routes](ROUTE.md) have pages of their own.

1. [GatewayClass](#gatewayclass)
1. [GatewayConfig](#gatewayconfig)
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
| `userQuota`                      | `int`               | Limit the number of allocations per username (**not supported in the free tier**). Default is 0, which means no quota.                                                                                                                         | No       |
| `stunMode`                       | `bool`              | Toggle STUN-only mode. In this mode STUNner responds only to STUN requests; any TURN allocation request is rejected. Default is false.                                                                                                        | No       |

At least a valid username/password pair *must* be supplied for `static` authentication, or a `sharedSecret` for the `ephemeral` mode, either via an external Secret or inline in the GatewayConfig. External authentication settings override inline settings. Missing both is an error.

Except the TURN authentication realm, all GatewayConfig resources are safe for modification. That is, the `stunnerd` daemon knows how to reconcile a change in the GatewayConfig without restarting listeners/TURN servers. Changing the realm, however, restarts the TURN servers that use the auth config, terminating all active client sessions.

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
| `priorityClassName`             | `string`   | The [PriorityClass](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/) of the [dataplane](/docs/cmd/stunnerd.md) pods to avoid them being evicted under capacity pressure. The PriorityClass must exist. Default: none. | No       |
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
