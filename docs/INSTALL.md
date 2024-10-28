# Installation

## Prerequisites

You need a Kubernetes cluster (>1.22), and the `kubectl` command-line tool must be installed and configured to communicate with your cluster. STUNner should be compatible with *any* major hosted Kubernetes service or any on-prem Kubernetes cluster; if not, please file an issue.

The simplest way to expose STUNner to clients is through Kubernetes [LoadBalancer services](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer); these are automatically managed by STUNner. This depends on a functional LoadBalancer integration in your cluster (if using Minikube, try `minikube tunnel` to get an idea of how this works). STUNner automatically detects if LoadBalancer service integration is functional and falls back to using NodePorts when it is not; however, this may require manual tweaking of the firewall rules to admit the UDP NodePort range into the cluster.

To compile STUNner, at least Go v1.19 is required. Building the container images requires [Docker](https://docker.io) or [Podman](https://podman.io).

## Installation

The simplest way to deploy STUNner is through [Helm](https://helm.sh). STUNner configuration parameters are available for customization as [Helm Values](https://helm.sh/docs/chart_template_guide/values_files); see the [STUNner-helm](https://github.com/l7mp/stunner-helm) repository for a list of the available customizations.

The first step is to register the STUNner repository with Helm.

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
```

### Stable version

The below will install the stable version of STUNner. In particular, the this will install only the STUNner control plane, i.e., the gateway operator and the authentication service, the dataplane will be automatically provisioned by the operator when needed (but see below). We recommend to use the `stunner-system` namespace to keep the full STUNner control plane in a single scope.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system
```

And that's all: you don't need to install the dataplane separately, this is handled automatically by the operator.  The `stunnerd` pods created by the operator can be customized using the Dataplane custom resource: you can specify the `stunnerd` container image version, provision resources per each `stunnerd` pod, deploy into the host network namespace, etc.; see the documentation [here](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator/api/v1alpha1#DataplaneSpec).

### Development version

By default, the Helm chart installs the stable version of STUNner. To track the bleeding edge, STUNner provides a `dev` release channel that tracks the latest development version. Use it at your own risk: we do not promise any stability for the dev-channel.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator-dev --create-namespace \
    --namespace=stunner-system
```

After upgrading the operator from the dev channel you may need to manually restart the dataplane
for each of your Gateways:

```console
kubectl -n <gateway-namespace> rollout restart deployment <gateway-name>
```

### Legacy mode

In the default *managed dataplane mode*, the STUNner gateway operator automatically provisions the dataplane, which substantially simplifies operations and removes lot of manual and repetitive work. For compatibility reasons the traditional operational model, called the *legacy mode*, is still available. In this mode the user is responsible for provisioning both the control plane, by installing the `stunner-gateway-operator` Helm chart, and the dataplane(s), by helm-installing the `stunner` chart possibly multiple times.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system --set stunnerGatewayOperator.dataplane.mode=legacy
helm install stunner stunner/stunner --create-namespace --namespace=stunner
```

You can install multiple legacy STUNner dataplanes side-by-side, provided that the corresponding namespaces are different. For instance, to create a `prod` dataplane installation for your production workload and a `dev` installation for experimentation, the below commands will install two dataplanes, one into the `stunner-prod` and another one into the `stunner-dev` namespace.

```console
helm install stunner-prod stunner/stunner --create-namespace --namespace=stunner-prod
helm install stunner-dev stunner/stunner --create-namespace --namespace=stunner-dev
```

### Skip install CRDs

You can install the STUNner chart without the Gateway API CRDs and STUNner CRDs with the `--skip-crds` flag. However, ensure that the CRDs are already present in the cluster, as the STUNner Gateway Operator will fail to start without them.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system --skip-crds
```

To manually install the CRDs:

```console
kubectl apply -k github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.0.0
kubectl apply -f https://raw.githubusercontent.com/l7mp/stunner-helm/refs/heads/main/helm/stunner-gateway-operator/crds/stunner-crd.yaml
```

## Customization

The Helm charts let you fine-tune STUNner features, including [compute resources](#resources) provisioned for `stunnerd` pods, [UDP multithreading](#udp-multithreading), and[graceful shutdown](#graceful-shutdown).

### Resources requests/limits

it is important to manage the [amount of CPU and memory resources](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers) available for each `stunnerd` pod.  The [default](https://github.com/l7mp/stunner-helm/blob/main/helm/stunner-gateway-operator/values.yaml) resource request and limit is set as follows: 

```yaml
resources:
  limits:
    cpu: 2
    memory: 512Mi
  requests:
    cpu: 500m
    memory: 128Mi
``` 

This means that every `stunnerd` pod will request 0.5 CPU cores and 128 Mibytes of memory. Note that the pods will start only if Kubernetes can successfully allocate the given amount of resources. In order to avoid stressing the Kubernetes scheduler, it is advised to keep the limits at the bare minimum and scale out by [increasing the number of running `stunnerd` pods](SCALING.md) if needed.

### UDP multithreading

STUNner can run multiple UDP listeners over multiple parallel readloops for loadbalancing. Namely, ech `stunnerd` pod can create a configurable number of UDP server sockets using `SO_REUSEPORT` and then spawn a separate goroutine to run a parallel readloop per each. The kernel will load-balance allocations across the sockets/readloops per the IP 5-tuple, so the same allocation will always stay at the same CPU. This allows UDP listeners to scale to multiple CPUs, improving performance. Note that this is required only for UDP: TCP, TLS and DTLS listeners spawn a per-client readloop anyway. Also note that `SO_REUSEPORT` is not portable, so currently we enable this only for UNIX architectures.

The feature is exposed via the command line flag `--udp-thread-num=<THREAD_NUMBER>` in `stunnerd`. In the Helm chart, it can be enabled or disabled with the `--set stunner.deployment.container.stunnerd.udpMultithreading.enabled=true` flag. By default, UDP multithreading is enabled with 16 separate readloops per each UDP listener.

```yaml
udpMultithreading:
  enabled: true
  readLoopsPerUDPListener: 16
```

### Graceful shutdown

STUNner has full support for [graceful shutdown](SCALING.md). This means that `stunner` pods will remain alive as long as there are active allocations in the embedded TURN server, and a pod will automatically remove itself once all allocations are deleted or time out. This enables the full support for graceful scale-down: the user can scale the number of `stunner` instances up and down and no harm should be made to active client connections meanwhile. 

The default termination period is set to 3600 seconds (1 hour). To modify, use the `--set stunner.deployment.container.terminationGracePeriodSeconds=<NEW_PERIOD_IN_SECONDS>` flag.

