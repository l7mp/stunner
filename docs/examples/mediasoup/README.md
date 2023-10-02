# STUNner demo: Video-conferencing with mediasoup

This document guides you through the installation of [mediasoup](https://mediasoup.org/) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the mediasoup server into Kubernetes, and
- configure STUNner to expose mediasoup to clients.

## Prerequisites

The below installation instructions require an operational cluster running a supported version of Kubernetes (>1.22). Most hosted or private Kubernetes cluster services will work, but make sure that the cluster comes with a functional load-balancer integration (all major hosted Kubernetes services should support this). Otherwise, STUNner will not be able to allocate a public IP address for clients to reach your WebRTC infra. As a regrettable exception, Minikube is unfortunately not supported for this demo. The reason is that [Let's Encrypt certificate issuance is not available with nip.io when using local/private IPs](https://medium.com/@EmiiKhaos/there-is-no-possibility-that-you-can-get-lets-encrypt-certificate-with-nip-io-7483663e0c1b); later on, you will learn more about why this is crucial above.

## Setup

The recommended way to install mediasoup ([link](https://mediasoup.discourse.group/t/server-in-kubernetes-with-turn/3434),[link](https://www.reddit.com/r/kubernetes/comments/sdkhwn/deploying_mediasoup_webrtc_sfu_in_kubernetes/)) into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`). This deployment model, however, comes with a set of uncanny [operational limitations and security concerns](../../WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows mediasoup deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, mediasoup is deployed behind STUNner in the [*media-plane deployment model*](../../DEPLOYMENT.md), so that STUNner acts as a "local" STUN/TURN server for mediasoup, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

In this tutorial we deploy a video room example using [mediasoup's demo application](https://github.com/versatica/mediasoup-demo/) with slight modifications (more on these below), the [mediasoup server](https://github.com/versatica/mediasoup/) for media exchange, a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the LiveKit server pool to clients.

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

## Installation

Let's start with a disclaimer. The mediasoup demo example must work over a secure HTTPS connection, because [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This implies that the client-server signaling connection must be secure too. According to the [documentation](https://github.com/versatica/mediasoup-demo/blob/a59c6ab8e50fb950c3df54f4b85167a4e3f8497a/README.md?plain=1#L96) mediasoup should work with self-signed certs, however this haven't been tested. In the following we will deploy mediasoup configured with a valid signed TLS certificate. This will have the unfortunate consequence that the majority of the below installation guide will be about securing client connections to mediasoup over TLS; as it turns out, once HTTPS is correctly working integrating mediasoup with STUNner is very simple.

In the below example, STUNner will be installed into the identically named namespace, while mediasoup and the Ingress gateway will live in the default namespace.

### TLS certificates

As mentioned above, the mediasoup server will need a valid TLS cert, which means it must run behind an existing DNS domain name backed by a CA signed TLS certificate. This is simple if you have your own domain, but if you don't then [nip.io](https://nip.io) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and obtain a CA signed certificate for mediasoup. This will allow us to point the domain name `client-<ingress-IP>.nip.io` to an ingress HTTP gateway in our Kubernetes cluster, which will then use some automation (namely, cert-manager) to obtain a valid CA signed cert.

Note that public wildcard DNS domains might run into [rate limiting](https://letsencrypt.org/docs/rate-limits/) issues. If this occurs you can try [alternative services](https://moss.sh/free-wildcard-dns-services/) instead of `nip.io`.

### Ingress

The first step of obtaining a valid cert is to install a Kubernetes Ingress: this will be used during the validation of our certificates and to terminate client TLS encrypted contexts.

Install an ingress controller into your cluster. We used the official [nginx ingress](https://github.com/kubernetes/ingress-nginx), but this is not required.

```console
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx
```

Wait until Kubernetes assigns an external IP to the Ingress.

```console
until [ -n "$(kubectl get service ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
```

Store the Ingress IP address Kubernetes assigned to our Ingress; this will be needed later when we configure the validation pipeline for our TLS certs.

```console
kubectl get service ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
export INGRESSIP=$(kubectl get service ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESSIP=$(echo $INGRESSIP | sed 's/\./-/g')
```

### Cert manager

We use the official [cert-manager](https://cert-manager.io) to automate TLS certificate management.

First, install cert-manager's CRDs.

```console
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.0/cert-manager.crds.yaml
```

Then add the Helm repository, which contains the cert-manager Helm chart, and install the charts:

```console
helm repo add cert-manager https://charts.jetstack.io
helm repo update
helm install my-cert-manager cert-manager/cert-manager \
    --create-namespace \
    --namespace cert-manager \
    --version v1.8.0
```

At this point we have all the necessary boilerplate set up to automate TLS issuance for mediasoup.

### STUNner

Now comes the fun part.

Install the STUNner gateway operator using the [managed dataplane mode](https://github.com/l7mp/stunner/blob/main/docs/INSTALL.md#managed-mode) via [Helm](https://github.com/l7mp/stunner-helm):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator-dev --create-namespace
 --namespace=stunner --set stunnerGatewayOperator.dataplane.mode=managed --set stunnerGatewayOperator.dataplane.spec.image=l7mp/stunnerd:latest
```

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the mediasoup server pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/mediasoup/mediasoup-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md), which takes care of routing media to the pods running behind the `mediasoup-server` service.

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
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
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: livekit-media-plane
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
until [ -n "$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
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
