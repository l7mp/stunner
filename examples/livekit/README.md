# Using Livekit with STUNner

This guide shows how you can use [LiveKit](https://livekit.io/)
inside a Kubernetes cluster, using STUNner as a WebRTC gateway.

In this demo you will learn the following steps to:
- integrate a typical WebRTC application server with STUNner
- deploy the LiveKit server into Kubernetes,
- configure STUNner to expose LiveKit to clients

## Prerequisites

The below installation instructions require an operational cluster running a supported version of Kubernetes (>1.22). You can use any supported platform, any hosted or private Kubernetes cluster, but make sure that the cluster comes with a functional load-balancer integration (all major hosted Kubernetes services should support this). Otherwise, STUNner will not be able to allocate a public IP address for clients to reach your WebRTC infra.

Minikube is not a supported platform unfortunately. It [cannot get a Let's Encrypt cerfiticate](https://medium.com/@EmiiKhaos/there-is-no-possibility-that-you-can-get-lets-encrypt-certificate-with-nip-io-7483663e0c1b) with nip.io using it which is essential for this demo. Later on you will learn more about this certificate.

## Ingress

In order to make this demo work you must have an ingress controller installed in your system.

```
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx
```
## Installation guide

Let's start with a disclaimer. The liveKit client example(browser) must have secure HTTP connection in order to work because, [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This induces that the client-server(LiveKit-server) connection must be secure too. According to the [docs](https://docs.livekit.io/deploy/#domain,-ssl-certificates,-and-load-balancer) and our experiences, self-signed certs do not work.
Due to these contstraints we must deploy some resources that can handle certificates. 

Install cert-manager's CRDs.
```
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.0/cert-manager.crds.yaml
```

Create a cert-manager namespace.
```
kubectl create namespace cert-manager
```

Add the Helm repository which contains the cert-manager Helm chart.
```
helm repo add cert-manager https://charts.jetstack.io
helm repo update
```

Install the cert-manager Helm chart. 
```
helm install \
my-cert-manager cert-manager/cert-manager \
--namespace cert-manager \
--version v1.8.0
```



And now let's start the fun part. The simplest way to deploy the demo is to clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the [manifest](livekit-server.yaml) packaged with STUNner.

Install the STUNner gateway operator and STUNner ([more info](https://github.com/l7mp/stunner-helm)):
```console
helm repo add stunner https://l7mp.io/stunner
helm repo update

helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=stunner

helm install stunner stunner/stunner --create-namespace --namespace=stunner

#Namespace can be changed, if you change it, change it everywhere later on 
```

Configure STUNner to act as a STUN server towards clients, and to let media reach the media server.

```
git clone https://github.com/l7mp/stunner
cd stunner/examples/livekit
kubectl apply -f livekit-call-stunner.yaml
```

In the next step we will set the STUNner LoadBalancer service IP as a TURN server for LiveKit.
It can take up to a minute to allocate a public external IP for the service. With the first command you can check whether you got the IP or not. If the output looks correct then you can issue the second and third command. 
```
kubectl get service stunner-gateway-udp-gateway-svc -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

STUNNERIP=$(kubectl get service stunner-gateway-udp-gateway-svc -n stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
sed -i "s/stunner_ip/$STUNNERIP/g" livekit-server.yaml
```

The LiveKit bundle includes a lot of resources:
- a LiveKit-server
- a client which is the [LiveKit react example](https://github.com/livekit/livekit-react)
- a cluster issuer which is for the certificates
- an Ingress resource to terminate the secure connections between your browser and the Kubernetes cluster

But before we apply the listed resources we must set one more thing. As it was mentioned earlier the LiveKit server must have a valid cert. It means you must have a domain that has CA signed certificate. If you have your own domain you can create two subdomains that points to the `ingress-nginx-controller` service's IP. If you don't have your own domain don't be upset there's a solution for you as well.  
[nip.io](nip.io) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and have a CA signed certificate. 

```
kubectl get service ingress-nginx-controller -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
INGRESSIP=$(kubectl get service ingress-nginx-controller -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
INGRESSIP=$(echo $INGRESSIP | sed 's/\./-/g')
sed -i "s/ingressserviceip/$INGRESSIP/g" livekit-server.yaml
```

Now apply the manifests:
```
kubectl apply -f livekit-server.yaml
kubectl get pods
```

## Test

After installing everything successfully you should be able to open the following command's output URL in your browser:
```
echo client-$INGRESSIP.nip.io
```

If it opened properly and you are greeted with the 'LiveKit Video' title you're doing great.
As you can see you must set the LiveKit URL. It is the other subdomain we had to set earlier but to make sure you type the right URL in:
```
echo wss://mediaserver-$INGRESSIP.nip.io:443
```

As for the token you must install the [livekit-cli](https://github.com/livekit/livekit-cli#installation) on your computer.

```
livekit-cli create-token \
    --api-key access_token --api-secret secret \
    --join --room room --identity user1 \
    --valid-for 24h
```
Copy the access token into the token field and hit the Connect button. If everything is set up correctly you should be able to connect to a room. If you repeat the procedure in a seperate browser tab you can see yourself twice with the twist that the other client's media is flowing through STUNner and the LiveKit-server deployed in you cluster.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.