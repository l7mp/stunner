# STUNner demo: Video-conferencing with Jitsi

This document guides you through the installation of [Jitsi](https://jitsi.org) into Kubernetes, when it is used together with the STUNner WebRTC media gateway.

In this demo you will learn to:

- integrate a typical WebRTC application server with STUNner,
- obtain a valid TLS certificate to secure the signaling plane,
- deploy the Jitsi server into Kubernetes, and
- configure STUNner to expose Jitsi to clients.

## Prerequisites

See prerequisites [here](../../INSTALL.md#prerequisites).

> [!NOTE]
>
> As a regrettable exception, Minikube is unfortunately not supported for this demo. The reason is that [Let's Encrypt certificate issuance is not available with nip.io](https://medium.com/@EmiiKhaos/there-is-no-possibility-that-you-can-get-lets-encrypt-certificate-with-nip-io-7483663e0c1b); later on you will learn more about why this is crucial above.

## Setup

The recommended way to install Jitsi into Kubernetes is deploying the media servers into the host-network namespace of the Kubernetes nodes (`hostNetwork: true`), or using a NodePort service or a dedicated Ingress to ingest WebRTC media traffic into the network. However, these options allow only one JVB instance per Kubernetes node and the host-network deployment model also comes with a set of uncanny [operational limitations and security concerns](../../WHY.md). Using STUNner, however, media servers can be deployed into ordinary Kubernetes pods and run over a private IP network, like any "normal" Kubernetes workload.

The figure below shows Jitsi deployed into regular Kubernetes pods behind STUNner without the host-networking hack. Here, Jitsi is deployed behind STUNner in the media-plane deployment model, so that STUNner acts as a "local" STUN/TURN server for Jitsi, saving the overhead of using public a 3rd party STUN/TURN server for NAT traversal.

![STUNner Jitsi integration deployment architecture](../../img/stunner_jitsi.svg)

In this tutorial we deploy a video room example using the [Jitsi framework](https://jitsi.github.io/handbook/docs/architecture) for media exchange, a Kubernetes Ingress gateway to secure signaling connections and handle TLS, and STUNner as a media gateway to expose the Jitsi JVB to clients.

## Installation

> [!NOTE]
>
> Let's start with a disclaimer. Securing connection between the user and the server is a must. Read more about TLS [here](../TLS.md).

In the below example, STUNner will be installed into the identically named namespace (`stunner`), while Jitsi and the Ingress gateway will live in the `default` namespace.

### Ingress and Cert manager installation

To ingest secured traffic into the cluster, you need to install some resources. Please follow the instructions in [this section](../TLS.md#installation) to install Ingress and Cert manager.

### STUNner

Now comes the fun part. The simplest way to run this demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy (after some minor modifications) the [manifest](jitsi-server.yaml) packaged with STUNner.

To install the stable version of STUNner, please follow the instructions in [this section](../../INSTALL.md#installation-1).

Configure STUNner to act as a STUN/TURN server to clients, and route all received media to the Jitsi server pods.

```console
git clone https://github.com/l7mp/stunner
cd stunner
kubectl apply -f docs/examples/jitsi/jitsi-call-stunner.yaml
```

The relevant parts here are the STUNner [Gateway definition](../../GATEWAY.md), which exposes the STUNner STUN/TURN server over UDP:3478 to the Internet, and the [UDPRoute definition](../../GATEWAY.md), which takes care of routing media to the pods running the Jitsi service. Also, with the GatewayConfig object we set the `authType: longterm` parameter because Prosody can't use Plaintext authentication only long term.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  authType: ephemeral
  sharedSecret: "my-shared-secret"
---
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
  name: jitsi-media-plane
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: jitsi-jvb
          namespace: default
```

Once the Gateway resource is installed into Kubernetes, STUNner will create a Kubernetes LoadBalancer for the Gateway to expose the TURN server on UDP:3478 to clients. It can take up to a minute for Kubernetes to allocate a public external IP for the service.

Wait until Kubernetes assigns an external IP and store the external IP assigned by Kubernetes to STUNner in an environment variable for later use

```console
until [ -n "$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNERIP=$(kubectl get svc udp-gateway -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

### Jitsi

The crucial step of integrating *any* WebRTC media server with STUNner is to ensure that the server instructs the clients to use STUNner as the STUN/TURN server. In order to achieve this, first we patch the public IP address of the STUNner STUN/TURN server we have learned above into our Jitsi deployment manifest:

```console
sed -i "s/<stunner-public-ip>/$STUNNERIP/g" docs/examples/jitsi/jitsi-server.yaml
```

This will make sure that Jitsi is started with STUNner as the STUN/TURN server. Note that Jitsi itself will not use STUNner (that would amount to a less efficient [symmetric ICE mode](../../DEPLOYMENT.md)); with the above configuration we are just telling Jitsi to instruct its clients to use STUNner to reach the Jitsi JVB.

We also need the Ingress external IP address we have stored previously: this will make sure that the TLS certificate created by cert-manager will be bound to the proper `nip.io` domain and IP address.

```console
sed -i "s/<public-ingress-ip>/$INGRESSIP/g" docs/examples/jitsi/jitsi-server.yaml
```

To use the web server, the corresponding Nginx `resolver` parameter must be the Kubernetes’ DNS address.

```console
export KUBEDNS=$(kubectl get svc kube-dns -n kube-system -o jsonpath='{.spec.clusterIP}')
sed -i "s/<kube-dns>/$KUBEDNS/g" docs/examples/jitsi/jitsi-server.yaml
```

Finally, fire up Jitsi.

```console
kubectl apply -f docs/examples/jitsi/jitsi-server.yaml
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
