# STUNner demo: Video-conferencing with Nexus

This document guides you through the installation of [Elixir WebRTC](https://elixir-webrtc.org/), called [Nexus](https://github.com/elixir-webrtc/apps/tree/master/nexus) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the Nexus app server into Kubernetes, and
- configure STUNner to expose Nexus to clients.

## Prerequisites

See prerequisites [here](../../INSTALL.md#prerequisites).

## Installation

> [!NOTE]
>
> Let's start with a disclaimer. The Nexus client-side application must work over a secure HTTPS connection, because [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This implies that the client-server signaling connection must be secure too. In this demo, we will aim to obtain a proper CA-signed certificate (self-signed certificates haven't been tested). Obtaining a valid TLS certificate is a challenge. Thus, the majority of the below installation guide will be about securing client connections to Nexus over TLS; as it turns out, once HTTPS is correctly working integrating Nexus with STUNner is very simple.

In the below example, STUNner will be installed into the identically named namespace (`stunner`), while Nexus and the Ingress gateway will live in the `default` namespace.

### TLS certificates

As mentioned above, the Nexus WebRTC server will need a valid TLS cert, which means it must run behind an existing DNS domain name backed by a CA signed TLS certificate. This is simple if you have your own domain, but if you don't then [nip.io](https://nip.io) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and obtain a CA signed certificate for Nexus. This will allow us to point the domain name `client-<ingress-IP>.nip.io` to an ingress HTTP gateway in our Kubernetes cluster, which will then use some automation (namely, cert-manager) to obtain a valid CA signed cert.

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

Add the Helm repository, which contains the cert-manager Helm chart, and install the charts:

```console
helm repo add cert-manager https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager --namespace cert-manager \
    --create-namespace --set global.leaderElection.namespace=cert-manager \
    --set crds.enabled=true --timeout 600s
```

At this point we have all the necessary boilerplate set up to automate TLS issuance for Nexus.

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy (after some minor modifications) the [manifest](nexus-server.yaml) packaged with STUNner.

Install the STUNner gateway operator and STUNner via [Helm](https://github.com/l7mp/stunner-helm):

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner
```

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the Nexus pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/elixir-webrtc/nexus-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md#gateway), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md#udproute), which takes care of routing media to the pods running the Nexus Gateway service.

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
  name: nexus
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - kind: Service
          name: nexus
          namespace: default
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to
STUNner in an environment variable for later use.

```console
until [ -n "$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get service udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```
### Nexus Docker images

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. 
Unfortunately, currently the [official Nexus Docker image](ghcr.io/elixir-webrtc/apps/nexus) does not support this configuration in runtime (by default Google's STUN server is hardcoded into it).
Therefore, we have to modify this setting to STUNner's IP and build a new Docker image.

In order to achieve this, first clone the Elixir WebRTC sample app repository:

```console
git clone https://github.com/elixir-webrtc/apps/
cd apps/nexus
```

You have to modify the ICE config to use STUNner with the given credentials in two files:

`assets/js/home.js`:
```
...
# const pcConfig = { iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] };

# change to:

  const pcConfig = { iceServers: [{ urls: 'turn:<STUNNERIP>:3478?transport=udp', username: 'user-1', credential: 'pass-1',iceTransportPolicy: 'relay'}] };
```

`lib/nexus/peer.ex`:
```
...
# ice_servers: [%{urls: "stun:stun.l.google.com:19302"}],

# change to:

  ice_servers: [%{urls: "turn:<STUNNERIP>:3478?transport=udp", "username": "user-1", "credential": "pass-1", "iceTransportPolicy": "relay"}],
```

Now rebuild the Docker image, and push it into your image repository:
```
export MYREPO=myrepo # use your own Docker repository name!
sudo docker build -t $MYREPO/nexus .
sudo docker push $MYREPO/nexus
```

After uploading the image, you also have to modify the Nexus image repo location in the Kubernetes deployment file.

```console
sed -i "s/l7mp/$MYREPO/g" docs/examples/elixir-webrtc/nexus-server.yaml
```

We also need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/ingressserviceip/$INGRESSIP/g" docs/examples/elixir-webrtc/nexus-server.yaml
```

Finally, fire up Nexus.

```console
kubectl apply -f docs/examples/elixir-webrtc/nexus-server.yaml
```

The demo installation bundle includes a few resources to deploy Nexus:

- Nexus deployment and service,
- a cluster issuer for the TLS certificates,
- an Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster.

Wait until all pods become operational and jump right into testing!

## Test

After installing everything, execute the following command to retrieve the URL of your freshly deployed Nexus demo app:

```console
echo INGRESSIP.nip.io
```

Copy the URL into your browser, and if everything is set up correctly, you should be able to connect to a video room. If you repeat the procedure in a separate browser tab you can enjoy a nice video-conferencing session with yourself, with the twist that all media between the browser tabs is flowing through STUNner and the Nexus server deployed in you Kubernetes cluster.

# Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).