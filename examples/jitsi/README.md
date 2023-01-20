# STUNner demo: Video-conferencing with Jitsi

This document guides you through the installation of [Jitsi](https://jitsi.org) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application server with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the Jitsi server into Kubernetes, and
- configure STUNner to expose Jitsi to clients.

## Prerequisites

The tutorial assumes a fresh STUNner installation; see the STUNner installation and configuration guide. Create a namespace called stunner if there is none. You need a WebRTC-compatible browser to run this tutorial. Basically any modern browser will do; we usually test our WebRTC applications with Firefox and Chrome.

As a regrettable exception, Minikube is unfortunately not supported for this demo. The reason is that [Let's Encrypt certificate issuance is not available with nip.io](https://medium.com/@EmiiKhaos/there-is-no-possibility-that-you-can-get-lets-encrypt-certificate-with-nip-io-7483663e0c1b); late on you will learn more about why this is crucial above.

## Setup

The recommended way to install Jitsi into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`), or using a NodePort service or a dedicated Ingress to ingest WebRTC media traffic into the network. However, these options allow only one JVB instance per Kubernetes node and the host-network deployment model also comes with a set of uncanny [operational limitations and security concerns](https://github.com/l7mp/stunner/blob/jitsi/doc/WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows Jitsi deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, Jitsi is deployed behind STUNner in the media-plane deployment model, so that STUNner acts as a "local" STUN/TURN server for Jitsi, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

![STUNner Jitsi integration deployment architecture](../../doc/stunner_jitsi.svg)

In this tutorial we deploy a video room example using the [Jitsi framework](https://jitsi.github.io/handbook/docs/architecture) for media exchange, a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the Jitsi JVB to clients.

## Installation

Let's start with a disclaimer. The Jitsi client example browser must work over a secure HTTPS connection, because [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This implies that the client-server signaling connection must be secure too. Unfortunately, self-signed TLS certs will not work, so we have to come up with a way to provide our clients with a valid TLS cert.

In the below example, STUNner will be installed into the identically named namespace, while Jitsi and the Ingress gateway will live in the default namespace.

### TLS certificates

As mentioned above, the Jitsi server will need a valid TLS cert, which means it must run behind an existing DNS domain name backed by a CA signed TLS certificate. This is simple if you have your own domain, but if you don't then [nip.io](https://nip.io/) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and obtain a CA signed certificate for Jitsi. This will allow us to point the domain name `client-<ingress-IP>.nip.io` to an ingress HTTP gateway in our Kubernetes cluster, which will then use some automation (namely, cert-manager) to obtain a valid CA signed cert.

Note that public wildcard DNS domains might run into [rate limiting](https://letsencrypt.org/docs/rate-limits/) issues. If this occurs you can try [alternative services](https://moss.sh/free-wildcard-dns-services/) instead of nip.io.

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

### Cert manger

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

At this point we have all the necessary boilerplate set up to automate TLS issuance for Jitsi.

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the [manifest](/examples/jitsi-server.yaml) packaged with STUNner.

Install the STUNner gateway operator and STUNner via [Helm](https://github.com/l7mp/stunner-helm):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner-system
helm install stunner stunner/stunner --create-namespace --namespace=stunner
```

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the Jitsi server pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f examples/jitsi/jitsi-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](/doc/GATEWAY.md), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](/doc/GATEWAY.md), which takes care of routing media to the pods running the Jitsi service. Also, with the GatewayConfig object we set the `authType: longterm` parameter because Prosody can't use Plaintext authentication only long term.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  authType: longterm
  sharedSecret: "my-shared-secret"
---
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
  name: jitsi-media-plane
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: jitsi-jvb
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to STUNner in an environment variable for later use

```console
until [ -n "$(kubectl get svc stunner-gateway-udp-gateway-svc -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service stunner-gateway-udp-gateway-svc -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

### Jitsi

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. In order to achieve this, first we patch the public IP address of the STUNner STUN/TURN server we have learned above into our Jitsi deployment manifest:

```console
sed -i "s/<stunner-public-ip>/$STUNNERIP/g" examples/jitsi/jitsi-server.yaml
```

This will make sure that Jitsi is started with STUNner as the STUN/TURN server. Note that Jitsi itself will not use STUNner (that would amount to a less efficient [symmetric ICE mode](/doc/DEPLOYMENT.md)); with the above configuration we are just telling Jitsi to instruct its clients to use STUNner to reach the Jitsi JVB.

We also need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/<public-ingress-ip>/$INGRESSIP/g" examples/jitsi/jitsi-server.yaml
```

To use the web server, the corresponding Nginx `resolver` parameter must be the Kubernetes’ DNS address.

```console
export KUBEDNS=$(kubectl get svc kube-dns -n kube-system -o jsonpath='{.spec.clusterIP}')
sed -i "s/<kube-dns>/$KUBEDNS/g" examples/jitsi/jitsi-server.yaml
```

Finally, fire up Jitsi.

```console
kubectl apply -f examples/jitsi/jitsi-server.yaml
```

The demo installation bundle includes a lot of resources to deploy Jitsi:
- Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster.
- Jitsi web server serving the landing page
- Cluster issuer for TLS certificates
- Prosody XMPP server to manage the WebRTC session signaling within the cluster
- Jicofo load balancer and connection broker to setup rooms on JVB
- JVB videobridge

Wait until all pods become operational and jump right into testing!

## Test

After installing everything, execute the following command to retrieve the URL of your fresh Jitsi demo app:

```console
echo $INGRESSIP.nip.io
```

Copy the URL into your browser, and now you should be greeted with the Jitsi webpage. In the landing page you should create a room first. After you created a room you can set your username and join the room. On another page you have to open this page again and you should see the previously created room in the list. You only have to connect this room with another user.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.
