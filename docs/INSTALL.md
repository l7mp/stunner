# Installation

## Prerequisites

You need a Kubernetes cluster (>1.22), and the `kubectl` command-line tool must be installed and configured to communicate with your cluster. STUNner should be compatible with *any* major hosted Kubernetes service or any on-prem Kubernetes cluster; if not, please file an issue.

The simplest way to expose STUNner to clients is through Kubernetes [LoadBalancer services](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer); these are automatically managed by STUNner. This depends on a functional LoadBalancer integration in your cluster (if using Minikube, try `minikube tunnel` to get an idea of how this works). STUNner automatically detects if LoadBalancer service integration is functional and falls back to using NodePorts when it is not; however, this may require manual tweaking of the firewall rules to admit the UDP NodePort range into the cluster.

To compile STUNner, at least Go v1.19 is required. Building the container images requires [Docker](https://docker.io) or [Podman](https://podman.io).

## Basic installation

The simplest way to deploy STUNner is through [Helm](https://helm.sh). STUNner configuration parameters are available for customization as [Helm Values](https://helm.sh/docs/chart_template_guide/values_files); see the [STUNner-helm](https://github.com/l7mp/stunner-helm) repository for a list of the available customizations.

First, register the STUNner repository with Helm.

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
```

Next, install the STUNner gateway operator. We recommend to install into the `stunner-system` namespace to keep the full STUNner control plane in a single scope.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system
```

And that's all: you don't need to install the dataplane separately as provisioning this is handled automatically by the operator. The `stunnerd` pods created by the operator can be customized using the Dataplane custom resource: you can specify the `stunnerd` container image version, provision resources per each `stunenrd` pod, deploy into the host network namespace, etc.; see the documentation [here](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator/api/v1alpha1#DataplaneSpec). All gateways will use the `default` Dataplane custom resource for provisioning `stunnerd` pods; you can override this by creating a new Dataplane CR and setting the name in the [`spec.dataplane` field](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator@v0.15.2/api/v1alpha1#GatewayConfigSpec) of the corresponding GatewayConfig.

```console
kubectl get dataplanes.stunner.l7mp.io default -o yaml
apiVersion: stunner.l7mp.io/v1
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

Modifications to the `default `Dataplane template are immediately enforced in the cluster, which usually induces a full restart of the entire dataplane. A misconfigured `default `Dataplane template can therefore easily break an entire STUNner installation. It is best to avoid changing (or deleting!) the `default` Dataplane unless you really know what you are doing.

## Development version

By default, the Helm chart installs the stable version of STUNner. To track the bleeding edge, STUNner provides a `dev` release channel that tracks the latest development version. Use it at your own risk: we do not promise any stability for the dev-channel.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator-dev --create-namespace \
    --namespace=stunner-system
```

## Legacy mode

In the default *managed dataplane mode*, the STUNner gateway operator automatically provisions the necessary dataplanes by creating a separate `stunnerd` Deployment per each Gateway, plus a LoadBalancer service to expose it. This substantially simplifies operations and removes lot of manual and repetitive work. For compatibility reasons the traditional operational model, called the *legacy mode*, is still available. In tnis more the user was responsible for provisioning both the control plane, by installing the `stunner-gateway-operator` Helm chart, and the dataplane(s), by helm-installing the `stunner` chart possibly multiple times. 

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
