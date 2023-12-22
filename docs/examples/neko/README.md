# Using Neko with STUNner

This guide shows how you can use [Neko](https://github.com/m1k1o/neko/)
inside a Kubernetes cluster, using STUNner as a WebRTC gateway.

Neko uses WebRTC to stream a desktop inside of a docker container to your browser.
However, integrating Neko into Kubernetes is far from trivial.

In this demo you will learn the following steps to:

- integrate a typical WebRTC application server to be used with STUNner,
- deploy Neko into Kubernetes behind STUNner,

## Installation

### Prerequisites

Consult the [STUNner installation and configuration guide](/docs/INSTALL.md) to set up STUNner.

### Quick installation

The simplest way to deploy the demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the [manifest](neko.yaml) packaged with STUNner.

Install the STUNner gateway operator and STUNner ([more info](https://github.com/l7mp/stunner-helm)):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner-system
helm install stunner stunner/stunner
```

Configure STUNner to act as a STUN server towards clients, and to let media reach the media server.

```console
git clone https://github.com/l7mp/stunner
cd stunner/docs/examples/neko
kubectl apply -f stunner.yaml
```

> [!WARNING]
> 
> In case of [managed mode](/docs/INSTALL.md), update the `neko-plane` UDPRoute by replacing `stunner` in backendRefs with the generated deployment, e.g., `udp-gateway`.

This will expose STUNner on a public IP on UDP port 3478. A Kubernetes `LoadBalancer` assigns an
ephemeral public IP address to the service, so first we need to learn the external IP.

```console
kubectl get service udp-gateway -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
STUNNERIP=$(kubectl get service udp-gateway -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

> **Note**
> This IP should be accessible from your browser. If that "public IP" is behind a NAT, you can overwrite it with the actual public IP that routes to the service by hand (e.g. `STUNNERIP=<your public IP>`).

We need to give this public IP the Neko configuration in the `NEKO_ICESERVERS` environment variable, inside the `json` content (basically this will tell you browser to use STUNner as a STUN/TURN server).
You can do that by hand, or by this fancy `sed` command:
```console
sed -i "s/1.1.1.1/$STUNNERIP/g" neko.yaml
```

Now apply the Neko manifests:
```console
kubectl apply -f neko.yaml
kubectl get pods
```

In this setup we use `ingress` to expose the Neko UI. Feel free to customize the `ingress` resource to your setup.
If you don't have an ingress controller, you can use the `neko-tcp` service with a `LoadBalancer` type.

Ideally, by opening your ingress controller in your browser, you should see the Neko UI. You can log in with the `admin`:`admin` credentials. The WebRTC stream then should be relayed through STUNner.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).
