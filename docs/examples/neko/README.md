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

See prerequisites [here](../../INSTALL.md#prerequisites).

### Quick installation

The simplest way to deploy the demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the [manifest](neko.yaml) packaged with STUNner.

To install the stable version of STUNner, please follow the instructions in [this section](../../INSTALL.md#installation-1).

Configure STUNner to act as a STUN server towards clients, and to let media reach the media server.

```console
git clone https://github.com/l7mp/stunner
cd stunner/docs/examples/neko
kubectl apply -f stunner.yaml
```

This will expose STUNner on a public IP on UDP port 3478. A Kubernetes `LoadBalancer` assigns an
ephemeral public IP address to the service, so first we need to learn the external IP.

```console
kubectl get service udp-gateway -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
STUNNERIP=$(kubectl get service udp-gateway -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

> [!NOTE]
> This IP should be accessible from your browser. If that "public IP" is behind a NAT, you can overwrite it with the actual public IP that routes to the service by hand (e.g. `STUNNERIP=<your public IP>`).

We need to give this public IP the Neko configuration in the `NEKO_ICESERVERS` environment variable, inside the `json` content (basically this will tell you browser to use STUNner as a STUN/TURN server).
You can do that by hand, or by this fancy `sed` command:
```console
sed -i "s/turn:[\.0-9]*:3478/turn:$STUNNERIP:3478/g" neko.yaml
```

Now apply the Neko manifests and wait for the `neko` deployment to be available (should take a couple of seconds):
```console
kubectl apply -f neko.yaml
kubectl wait --for=condition=Available deployment neko --timeout 5m
```

In this setup we use `ingress` to expose the Neko UI. Feel free to customize the `ingress` resource to your setup.
If you don't have an ingress controller, you can use the `neko-tcp` service with a `LoadBalancer` type.

Ideally, by opening your ingress controller in your browser, you should see the Neko UI. You can log in with the `admin`:`admin` credentials. The WebRTC stream then should be relayed through STUNner.

> [!NOTE]
> Tested with Chromium/Google Chrome.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).
