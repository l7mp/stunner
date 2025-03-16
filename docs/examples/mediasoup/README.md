# STUNner demo: Video-conferencing with mediasoup

This document guides you through the installation of [mediasoup](https://mediasoup.org/) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the mediasoup server into Kubernetes, and
- configure STUNner to expose mediasoup to clients.

## Prerequisites

To run this example, you need:

- a [Kubernetes cluster](../../INSTALL.md#prerequisites),
- a [deployed STUNner](../../INSTALL.md#installation-1) (presumably the latest stable version),
- an [Ingress controller](../TLS.md#ingress) to ingest traffic into the cluster,
- a [Cert-manager](../TLS.md#cert-manager) to automate TLS certificate management.

> [!NOTE]
>
> If you have your own TLS certificate, put it in a `Secret` [resource](https://kubernetes.io/docs/concepts/configuration/secret/) and deploy it into the `default` namespace under the `mediasoup-demo-tls` name.


## Description

The recommended way to install mediasoup ([link](https://mediasoup.discourse.group/t/server-in-kubernetes-with-turn/3434),[link](https://www.reddit.com/r/kubernetes/comments/sdkhwn/deploying_mediasoup_webrtc_sfu_in_kubernetes/)) into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`). This deployment model, however, comes with a set of uncanny [operational limitations and security concerns](../../WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows mediasoup deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, mediasoup is deployed behind STUNner in the [*media-plane deployment model*](../../DEPLOYMENT.md), so that STUNner acts as a "local" STUN/TURN server for mediasoup, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

![STUNner mediasoup integration deployment architecture](../../img/stunner_mediasoup.svg)

In this tutorial we deploy a video room example using [mediasoup's demo application](https://github.com/versatica/mediasoup-demo/) with slight modifications (more on these below), the [mediasoup server](https://github.com/versatica/mediasoup/) for media exchange, a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the mediasoup server pool to clients.

### Modifications on the mediasoup demo

Below are the modification that has been done starting from [mediasoup-demo](https://github.com/versatica/mediasoup-demo/):

- Added a multistage Dockerfile

  - stage 0: run gulp dist to create the frontend app file (they will be served by nodejs from the backend)
  - stage 1: build the image for the mediasoup-server and copy the mediasoup-client file

- Added a simple script that gathers the internal/private IP of the running pod, this is not foolproof, however,
with an additional environment variable we can load the pod's private IP into the code

- Added the following in server.js in the function "async function createExpressApp()" to serve the mediasoup-client file

```
147:	expressApp.use(express.static('public'))
```

- Added the parsing of url parameters to configure TURN server and a simple if/else in server/app/lib/RoomClient.js. The mediasoup clients will use the configured TURN server to gather the ICE candidates. Example: https://mediasoup-demo.example.com/?enableIceServer=yes&iceServerHost=100.100.100.100&iceServerPort=3478&iceServerProto=udp&iceServerUser=user-1&iceServerPass=pass-1

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy (after some minor modifications) the [manifest](mediasoup-server.yaml) packaged with STUNner.

To install the stable version of STUNner, please follow the instructions in [this section](../../INSTALL.md#installation-1).

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the mediasoup server pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/mediasoup/mediasoup-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md), which takes care of routing media to the pods running behind the `mediasoup-server` service.

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
---
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: mediasoup-media-plane
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
      - group: ""
        kind: Service
        name: mediasoup-server
        namespace: mediasoup
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to
STUNner in an environment variable for later use.

```console
until [ -n "$(kubectl get svc udp-gateway -n stunner-system -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service udp-gateway -n stunner-system -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

### mediasoup

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. However, there is a slight issue. In this deployment it's not the server that instructs the clients to use STUNner but the user itself. Obviously, it is not the optimal way but for the sake of the demo purpose we keep it that way. In case anyone would want to create a production ready deployment, they would need to add extra capabilities to the mediasoup server:
- first to make sure turn servers can be configured in the server's config.js file
- second to make sure that clients can fetch (or get automatically) the configured turn servers from the mediasoup server

We need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/ingressserviceip/$INGRESSIP/g" docs/examples/mediasoup/mediasoup-server.yaml
```

Finally, fire up mediasoup.

```console
kubectl create ns mediasoup
kubectl apply -f docs/examples/mediasoup/mediasoup-server.yaml
```

The demo installation bundle includes a lot of resources to deploy mediasoup:

- a mediasoup-server,
- an application server serving the landing page using [mediasoup-demo](https://github.com/versatica/mediasoup-demo/)
- a cluster issuer for the TLS certificates,
- an Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster.

Wait until all pods become operational and jump right into testing!

## Test

After installing everything, execute the following command to retrieve the URL of your fresh mediasoup demo app:

```console
echo "https://mediasoup-$INGRESSIP.nip.io:443?enableIceServer=yes&iceServerHost=$STUNNERIP&iceServerPort=3478&iceServerProto=udp&iceServerUser=user-1&iceServerPass=pass-1"
```

Opening the output in a browser should get the mediasoup client demo app

In case you changed something additionally in the STUNner configuration during deployment watch out for the URL parameters:
  - `enableIceServer` must be `yes` in order to use STUNner as a TURN server
  - `iceServerHost` should point to the public IP that was allocated for the STUNner load balancer service
  - `iceServerPort` is the port of your TURN server configured in the Gateway resource
  - `iceServerProto` is the expected protocol on the port configured above
  - `iceServerUser` is the username used for authentication in STUNner
  - `iceServerPass` is the credential used for authentication in STUNner


## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## Acknowledgments

This demo is adopted from [damhau/mediasoup-demo-docker](https://github.com/damhau/mediasoup-demo-docker). Huge thanks to @damhau for the great demo!
