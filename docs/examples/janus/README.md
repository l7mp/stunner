# STUNner demo: Video-conferencing with Janus

This document guides you through the installation of [Janus](https://janus.conf.meetecho.com/) by [Meetecho](https://www.meetecho.com/en/) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the Janus WebRTC server into Kubernetes, and
- configure STUNner to expose Janus to clients.

## Prerequisites

To run this example, you need:
* a [Kubernetes cluster](../../INSTALL.md#prerequisites),
* a [deployed STUNner](../../INSTALL.md#installation-1) (presumably the latest stable version),
* an [Ingress controller](../TLS.md#ingress) to ingest traffic into the cluster,
* a [Cert-manager](../TLS.md#cert-manager) to automate TLS certificate management.

> [!NOTE]
>
> If you have your own TLS certificate, put it in a `Secret` [resource](https://kubernetes.io/docs/concepts/configuration/secret/) and deploy it into the `default` namespace under the `janus-web-secret-tls` name.

## Description

The recommended way (or at least the possible way, [link](https://janus.discourse.group/t/janus-with-kubernetes-demystifying-the-myths/938), [link](https://bugraoz93.medium.com/active-passive-highly-availability-janus-gateway-on-kubernetes-2189256e5525)) to install Janus into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`). This deployment model, however, comes with a set of uncanny [operational limitations and security concerns](../../WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows Janus deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, Janus is deployed behind STUNner in the [*media-plane deployment model*](../../DEPLOYMENT.md), so that STUNner acts as a "local" STUN/TURN server for Janus, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

![STUNner Janus integration deployment architecture](../../img/stunner_janus_arch.svg)

In this tutorial we deploy [Janus Gateway](https://github.com/meetecho/janus-gateway/tree/master) with a set of [preimplemented and packaged server plugins](https://janus.conf.meetecho.com/docs/pluginslist.html) for media exchange, a [Janus Web Demo](https://github.com/meetecho/janus-gateway/tree/master/html), a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the Janus server pool to clients.

### Docker images

Janus does not come with an official Docker image; thus, we built one using a self-made Dockerfile based on the available documents in the official [Janus repository](https://github.com/meetecho/janus-gateway). Actually, we've made two Dockerfiles. One for the Janus Gateway server and one for the Janus Web Demos. The [Janus Gateway server Dockerfile](./DOCKERFILE-janus-gateway) should be ran in the root directory of the [Janus repository](https://github.com/meetecho/janus-gateway). The Janus Web Demos Dockerfile should be used in the `/html` directory of the [same repository](https://github.com/meetecho/janus-gateway/tree/master/html). The images (`l7mp/janus-gateway:v1.2.4` and `l7mp/janus-web:latest`) used in the following demo are hosted on Docker Hub under the L7MP organization.

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy (after some minor modifications) the [manifest](janus-server.yaml) packaged with STUNner.

To install the stable version of STUNner, please follow the instructions in [this section](../../INSTALL.md#installation-1).

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the Janus Gateway pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/janus/janus-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md#gateway), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md#udproute), which takes care of routing media to the pods running the Janus Gateway service.

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
      protocol: UDP
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: janus
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - kind: Service
          name: janus-gateway
          namespace: default
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to
STUNner in an environment variable for later use.

```console
until [ -n "$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

### Janus

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. In order to achieve this, first we patch the public IP address of the STUNner STUN/TURN server we have learned above into our Janus Web and Gateway deployment manifests:

```console
sed -i "s/stunner_ip/$STUNNERIP/g" docs/examples/janus/janus-server.yaml
```

Janus Web tells the connected clients where to look for the Janus Gateway server and which ICE servers should be used for ICE negotiation. Assuming that Kubernetes assigns the IP address 1.2.3.4 to STUNner (i.e., `STUNNERIP=1.2.3.4`), the relevant part of the Janus Web config would be something like the below:

```yaml
...
  settings.js: |
    var server = "wss://server-$INGRESSIP.nip.io"
    var iceServers = [{urls: "turn:1.2.3.4:3478?transport=udp", username: "user-1", credential: "pass-1"}]
```

This will make sure that Janus Web tells the clients to use STUNner as the STUN/TURN server. If unsure about the STUNner settings to use, you can always use the handy [`stunnerctl` CLI tool](/cmd/stunnerctl/README.md) to dump the running STUNner configuration.

``` console
stunnerctl -n stunner config udp-gateway
Gateway: stunner/udp-gateway (loglevel: "all:INFO")
Authentication type: static, username/password: user-1/pass-1
Listeners:
  - Name: stunner/udp-gateway/udp-listener
    Protocol: TURN-UDP
    Public address:port: 34.118.88.91:3478
    Routes: [stunner/iperf-server]
    Endpoints: [10.76.1.4, 10.80.4.47]
```

Note that Janus itself will not use STUNner as a TURN server (that would amount to a less efficient [symmetric ICE mode](../../DEPLOYMENT.md)); with the above configuration we are just telling Janus Web to instruct its clients to use STUNner to reach the Janus Gateway server.

We also need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/ingressserviceip/$INGRESSIP/g" docs/examples/janus/janus-server.yaml
```

Finally, fire up Janus.

```console
kubectl apply -f docs/examples/janus/janus-server.yaml
```

The demo installation bundle includes a lot of resources to deploy Janus:

- a Janus Gateway server,
- a web server serving the landing page using [Janus Web Demos](https://github.com/meetecho/janus-gateway/tree/master/html)
- a cluster issuer for the TLS certificates,
- an Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster.

Wait until all pods become operational and jump right into testing!

## Test

After installing everything, execute the following command to retrieve the URL of your freshly deployed Janus demo app:

```console
echo client-$INGRESSIP.nip.io
```

Copy the URL into your browser, and now you should be greeted with the About page. On the landing page navigate to the Video call plugin demo (`/demos/videocall.html`). Duplicate the tab and register two users in the system and make a call. If everything is set up correctly, you should be able to connect to a room. If you repeat the procedure in a separate browser tab you can enjoy a nice video-conferencing session with yourself, with the twist that all media between the browser tabs is flowing through STUNner and the Janus Gateway server deployed in you Kubernetes cluster.

# Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).