# TLS

This documentation sums up the the TLS and certificate issues you will encounter deploying the examples.

## The issue

Some client-side application must work over a secure HTTPS connection, because [getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia#browser_compatibility) is available only in secure contexts. This implies that the client-server signaling connection must be secure too. In the demos, we will aim to obtain a proper CA-signed certificate (self-signed certificates haven't been tested). Obtaining a valid TLS certificate is a challenge. Thus, the majority of the installation guides will be about securing client connections to the client-side apps and the WebRTC mediaservers over TLS. Once HTTPS is correctly working, integrating the mediaservers with STUNner is very simple.

## TLS certificates

Some WebRTC servers will need a valid TLS cert, which means it must run behind an existing DNS domain name backed by a CA signed TLS certificate. This is simple if you have your own domain, but if you don't, we still have a solution for that.

> [!NOTE]
>
> By default, the examples and the commands included are set up in case you don't have your domain.

### If you don't have your own domain

[nip.io](https://nip.io) provides a dead simple wildcard DNS for any IP address. We will use this to "own a domain" and obtain a CA signed certificate for the mediaserver. This will allow us to point the domain name `client-<ingress-IP>.nip.io` to an ingress HTTP gateway in our Kubernetes cluster, which will then use some automation (namely, cert-manager) to obtain a valid CA signed cert.

### If you have your own domain

> [!NOTE]
>
> Do not forget to create a new DNS record pointing to your ingress' IP address!

## Installation

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

At this point we have all the necessary boilerplate set up to automate TLS issuance for the demo.

## Troubleshooting

#### Wildcard DNS domain rate limiting

Note that public wildcard DNS domains might run into [rate limiting](https://letsencrypt.org/docs/rate-limits/) issues. If this occurs you can try [alternative services](https://moss.sh/free-wildcard-dns-services/) instead of `nip.io`.

#### Certificate issuance

If you work with certificates you must be aware that signing a certificate request takes some time and it differs for every CA (certificate authority). If you sense there is a problem with the certificate being signed or issued, you can check it directly and see what is going on.

First, you'll need to find the certificate and its related resources in your cluster.
```console
kubectl get certificate -A

kubectl get certificaterequests.cert-manager.io -A

kubectl get certificatesigningrequests.certificates.k8s.io
```

To find more information about them
```console
kubectl describe certificate <certificate> -A

kubectl describe certificaterequests.cert-manager.io <cert-request> -A

kubectl describe certificatesigningrequests.certificates.k8s.io <cert-signing-request>
```