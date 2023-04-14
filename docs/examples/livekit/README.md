# STUNner demo: Video-conferencing with LiveKit

This document guides you through the installation of [LiveKit](https://livekit.io/) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the LiveKit server into Kubernetes, and
- configure STUNner to expose LiveKit to clients.

## Prerequisites

The below installation instructions require an operational cluster running a supported version of Kubernetes (>1.22). Most hosted or private Kubernetes cluster services will work, but make sure that the cluster comes with a functional load-balancer integration (all major hosted Kubernetes services should support this). Otherwise, STUNner will not be able to allocate a public IP address for clients to reach your WebRTC infra. As a regrettable exception, Minikube is unfortunately not supported for this demo. The reason is that [Let's Encrypt certificate issuance is not available with nip.io](https://medium.com/@EmiiKhaos/there-is-no-possibility-that-you-can-get-lets-encrypt-certificate-with-nip-io-7483663e0c1b); late on you will learn more about why this is crucial above.

## Setup

The recommended way to install LiveKit into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`). This deployment model, however, comes with a set of uncanny [operational limitations and security concerns](../../WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows LiveKit deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, LiveKit is deployed behind STUNner in the [*media-plane deployment model*](../../DEPLOYMENT.md), so that STUNner acts as a "local" STUN/TURN server for LiveKit, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

![STUNner LiveKit intergration deployment architecture](../../img/stunner_livekit.svg)

In this tutorial we deploy a video room example using [LiveKit's React SDK](https://github.com/livekit/livekit-react/tree/master/example), the [LiveKit server](https://github.com/livekit/livekit) for media exchange, a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the LiveKit server pool to clients.

## Installation

Let's start with a disclaimer. The LiveKit client example browser must work over a secure HTTPS connection, because [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This implies that the client-server signaling connection must be secure too. Unfottunately, self-signed TLS certs [will not work](https://docs.livekit.io/deploy/#domain,-ssl-certificates,-and-load-balancer), so we have to come up with a way to provide our clients with a valid TLS cert. This will have the unfortunate consequence that the majority of the below installation guide will be about securing client connections to LiveKit over TLS; as it turns out, once HTTPS is correctly working integrating LiveKit with STUNner is very simple.

In the below example, STUNner will be installed into the identically named namespace, while LiveKit and the Ingress gateway will live in the default namespace.

### TLS certificates

As mentioned above, the LiveKit server will need a valid TLS cert, which means it must run behind an existing DNS domain name backed by a CA signed TLS certificate. This is simple if you have your own domain, but if you don't then [nip.io](https://nip.io) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and obtain a CA signed certificate for LiveKit. This will allow us to point the domain name `client-<ingress-IP>.nip.io` to an ingress HTTP gateway in our Kubernetes cluster, which will then use some automation (namely, cert-manager) to obtain a valid CA signed cert.

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

At this point we have all the necessary boilerplate set up to automate TLS issuance for LiveKit.

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the [manifest](livekit-server.yaml) packaged with STUNner.

Install the STUNner gateway operator and STUNner via [Helm](https://github.com/l7mp/stunner-helm):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner-system
helm install stunner stunner/stunner --create-namespace --namespace=stunner
```

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the LiveKit server pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/livekit/livekit-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md), which takes care of routing media to the pods running the LiveKit service.

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
      protocol: UDP
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
        - name: livekit-server
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to
STUNner in an environment variable for later use.

```console
until [ -n "$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

### LiveKit

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. In order to achieve this, first we path the public IP address of the STUNner STUN/TURN server we have learned above into our LiveKit deployment manifest:

```console
sed -i "s/stunner_ip/$STUNNERIP/g" docs/examples/livekit/livekit-server.yaml
```

Assuming that Kubernetes assigns the IP address 1.2.3.4 to STUNner (i.e., `STUNNERIP=1.2.3.4`), the relevant part of the LiveKit config would be something like the below:

```yaml
...
rtc:
  ...
  turn_servers:
  - host: 1.2.3.4
    username: user-1
    credential: pass-1
    protocol: udp
    port: 3478
```

This will make sure that LiveKit is started with STUNner as the STUN/TURN server. If unsure about the STUNner settings to use, you can always use the handy `stunnerctl` CLI tool to dump the running STUNner configuration.

``` console
cd stunner
cmd/stunnerctl/stunnerctl running-config default/stunnerd-config
STUN/TURN authentication type:  plaintext
STUN/TURN username:             user-1
STUN/TURN password:             pass-1
Listener 1
        Name:   udp-listener
        Listener:       udp-listener
        Protocol:       UDP
        Public address: 1.2.3.4
        Public port:    3478
```

Note that LiveKit itself will not use STUNner (that would amount to a less efficient [symmetric ICE mode](../../DEPLOYMENT.md)); with the above configuration we are just telling LiveKit to instruct its clients to use STUNner to reach the LiveKit media servers.

We also need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/ingressserviceip/$INGRESSIP/g" docs/examples/livekit/livekit-server.yaml
```

Finally, fire up LiveKit.

```console
kubectl apply -f docs/examples/livekit/livekit-server.yaml
```

The demo installation bundle includes a lot of resources to deploy LiveKit:

- a LiveKit-server,
- a web server serving the landing page using [LiveKit react example](https://github.com/livekit/livekit-react)
- a cluster issuer for the TLS certificates,
- an Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster.

Wait until all pods become operational and jump right into testing!

## Test

After installing everything, execute the following command to retrieve the URL of your fresh LiveKit demo app:

```console
echo client-$INGRESSIP.nip.io
```

Copy the URL into your browser, and now you should be greeted with the LiveKit Video title. On the landing page you must set the LiveKit URL, which is the LiveKit server's IP address, or in our case the other subdomain we set earlier in the Ingress manifest.

Executing the following command shall get you the required URL:

```console
echo wss://mediaserver-$INGRESSIP.nip.io:443
```

To obtain a valid token, install the [livekit-cli](https://github.com/livekit/livekit-cli#installation) and issue the below command.

```console
livekit-cli create-token \
    --api-key access_token --api-secret secret \
    --join --room room --identity user1 \
    --valid-for 24h
```

Copy the access token into the token field and hit the Connect button. If everything is set up correctly, you should be able to connect to a room. If you repeat the procedure in a separate browser tab you can enjoy a nice video-conferencing session with yourself, with the twist that all media between the browser tabs is flowing through STUNner and the LiveKit-server deployed in you Kubernetes cluster.