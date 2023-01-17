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

To recompile STUNner, at least Go v1.18 is required. Building the container images requires
[Docker](https://docker.io) or [Podman](https://podman.io).

## Basic installation 

The simplest way to deploy the full STUNner distro, with the dataplane and the controller
automatically installed, is through [Helm](https://helm.sh). STUNner configuration parameters are
available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files). We recommend deploying the STUNner
dataplane into a separate Kubernetes namespace and we usually name this namespace as `stunner`, in
order to isolate it from the rest of the workload. The control plane operator can be deployed into
any namespace.

First, register the STUNner repository with Helm.

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
```

Install the control plane:

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator
```

Install the dataplane:

```console
helm install stunner stunner/stunner --create-namespace --namespace=stunner
```

## Parallel deployments

You can install multiple STUNner dataplanes side-by-side, provided that the corresponding
namespaces are different. For instance, to create a `prod` dataplane installation for your
production workload and a `dev` installation for experimentation, the below commands will install
two dataplanes, one into the `pod` and another one into the `dev` namespace.

```console
helm install stunner-prod stunner/stunner --create-namespace --namespace=stunner-prod
helm install stunner-dev stunner/stunner --create-namespace --namespace=stunner-dev
```

Now, you can build a separate [gateway hierarchy](/doc/CONCEPTS.md) per each namespace to supply a
distinct ingress gateway configuration per dataplane.

For the list of available customizations, see the
[STUNner-helm](https://github.com/l7mp/stunner-helm) repository. For installing STUNner in the
standalone mode, consult the documentation [here](/doc/OBSOLETE.md).

## Development version

You can install the STUNner from the `dev` channel, which allows to track the latest development
version. Use this at your own risk: we do not promise any stability for the dev-channel version.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator-dev
helm install stunner stunner/stunner-dev --create-namespace --namespace=stunner-dev
```

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
