# Installation

## Prerequisites

You need a Kubernetes cluster (>1.22), and the `kubectl` command-line tool must be installed and
configured to communicate with your cluster. STUNner should be compatible with *any* major hosted
Kubernetes service or any on-prem Kubernetes cluster; if not, please file an issue.

The simplest way to expose STUNner to clients is through Kubernetes [LoadBalancer
services](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer);
these are automatically managed by STUNner. This depends on a functional LoadBalancer integration
in your cluster (if using Minikube, try `minikube tunnel` to get an idea of how this
works). STUNner automatically detects if LoadBalancer service integration is functional and falls
back to using NodePorts when it is not; however, this may require manual tweaking of the firewall
rules to admit the UDP NodePort range into the cluster.

To recompile STUNner, at least Go v1.19 is required. Building the container images requires
[Docker](https://docker.io) or [Podman](https://podman.io).

## Basic installation

The simplest way to deploy the full STUNner distro, with the dataplane and the controller
automatically installed, is through [Helm](https://helm.sh). STUNner configuration parameters are
available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files). We recommend deploying each
STUNner dataplane into a separate Kubernetes namespace (e.g., `stunner`), while the gateway
operator should go into the `stunner-system` namespace (but effectively any namespace would work).

First, register the STUNner repository with Helm.

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
```

Install the control plane:

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner-system
```

Install the dataplane:

```console
helm install stunner stunner/stunner --create-namespace --namespace=stunner
```

## Parallel deployments

You can install multiple STUNner dataplanes side-by-side, provided that the corresponding
namespaces are different. For instance, to create a `prod` dataplane installation for your
production workload and a `dev` installation for experimentation, the below commands will install
two dataplanes, one into the `stunner-prod` and another one into the `stunner-dev` namespace.

```console
helm install stunner-prod stunner/stunner --create-namespace --namespace=stunner-prod
helm install stunner-dev stunner/stunner --create-namespace --namespace=stunner-dev
```

Now, you can build a separate [gateway hierarchy](CONCEPTS.md) per each namespace to supply a
distinct ingress gateway configuration per dataplane.

For the list of available customizations, see the
[STUNner-helm](https://github.com/l7mp/stunner-helm) repository. For installing STUNner in the
standalone mode, consult the documentation [here](OBSOLETE.md).

## Development version

STUNner provides a `dev` release channel, which allows to track the latest development version. Use
it at your own risk: we do not promise any stability for STUNner installed from the dev-channel.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator-dev --create-namespace --namespace=stunner-system
helm install stunner stunner/stunner-dev --create-namespace --namespace=stunner
```

## Managed mode

From v0.16.0 STUNner provides a new way to provision dataplane pods that is called the *managed mode*. In the traditional operational model (called the *legacy mode*), the user was responsible for provisioning both the control plane, by installing the `stunner-gateway-operator` Helm chart, and the dataplane(s), by helm-installing the `stunner` chart [possibly multiple times](#parallel-deployments). In the managed mode the operator *automatically* provisions the necessary dataplanes by creating a separate `stunnerd` Deployment per each Gateway, plus the usual LoadBalancer service to expose it. This substantially simplifies operations and removes lot of manual and repetitive work.

To install the gateway operator using the new manged mode, start with a clean Kubernetes cluster and install the `stunner-gateway-operator` Helm chart, setting the flag `stunnerGatewayOperator.dataplane.mode` to `managed`. Observe that we do not install the `stunner` Helm chart separately; the operator will readily create the `stunnerd` pods as needed.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system --set stunnerGatewayOperator.dataplane.mode=managed
```

The `stunnerd` pods created by the operator can be customized using the Dataplane CR: for instance you can specify the `stunnerd` container image version to be used as the dataplane, provision resources for each `stunenrd` pod, deploy into the host network namespace, etc.; see the documentation [here](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator/api/v1alpha1#DataplaneSpec). All gateways will use the `default` Dataplane; you can override this by creating a new Dataplane CR and setting the name in the [`spec.dataplane` field](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator@v0.15.2/api/v1alpha1#GatewayConfigSpec) of the corresponding GatewayConfig.

```console
kubectl get dataplanes.stunner.l7mp.io default -o yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: Dataplane
metadata:
  name: default
spec:
  image: l7mp/stunnerd:latest
  imagePullPolicy: Always
  command:
  - stunnerd
  args:
  - -w
  - --udp-thread-num=16
  hostNetwork: false
  resources:
    limits:
      cpu: 2
      memory: 512Mi
    requests:
      cpu: 500m
      memory: 128Mi
  terminationGracePeriodSeconds: 3600
```
