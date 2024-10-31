# STUNner Tutorial: Deploy a UDP echo service behind STUNner

This tutorial shows how to tunnel an external connection via STUNner to a UDP service deployed into Kubernetes. The tutorial can also be used as an introduction to the main concepts in STUNner and to quickly check a STUNner installation.

In this tutorial you will learn how to:
* configure a UDP service in Kubernetes,
* configure STUNner to expose the service to clients,
* use [`turncat`](../../cmd/turncat.md) to connect to the UDP service via STUNner,
* benchmark your cloud-setup with [`iperfv2`](https://iperf.fr).

## Prerequisites

The tutorial assumes a fresh STUNner installation; see the [STUNner installation and configuration
guide](../../INSTALL.md).

## Configuration

The standard way to interact with STUNner is via the standard Kubernetes [Gateway
API](https://gateway-api.sigs.k8s.io). This is much akin to the way you configure *all* Kubernetes
workloads: specify your intents in YAML files and issue a `kubectl apply`, and the [STUNner gateway
operator](https://github.com/l7mp/stunner-gateway-operator) will automatically create the STUNner
dataplane (that is, the `stunnerd` pods that implement the STUN/TURN service) and downloads the new
configuration to the dataplane pods.

It is generally a good idea to maintain STUNner configuration into a separate Kubernetes
namespace. Below we will use the `stunner` namespace; create it with `kubectl create namespace
stunner` if it does not exist.

1. Given a fresh STUNner install, the first step is to register STUNner with the Kubernetes Gateway
   API. This amounts to creating a
   [GatewayClass](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.GatewayClass),
   which serves as the [root level configuration](/docs/GATEWAY.md#gatewayclass) for your STUNner
   deployment.

   Each GatewayClass must specify a controller that will manage the Gateway objects created under
   the class hierarchy. This must be set to `stunner.l7mp.io/gateway-operator` in order for STUNner
   to pick up the GatewayClass. In addition, a GatewayClass can refer to further
   implementation-specific configuration via a reference called `parametersRef`; in our case, this
   will be a GatewayConfig object to be specified next.

   ``` console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1
   kind: GatewayClass
   metadata:
     name: stunner-gatewayclass
   spec:
     controllerName: "stunner.l7mp.io/gateway-operator"
     parametersRef:
       group: "stunner.l7mp.io"
       kind: GatewayConfig
       name: stunner-gatewayconfig
       namespace: stunner
     description: "STUNner is a WebRTC media gateway for Kubernetes"
   EOF
   ```

1. The next step is to set some [general configuration](/docs/GATEWAY.md#gatewayconfig) for STUNner,
   most importantly the STUN/TURN authentication [credentials](/docs/AUTH.md). This requires loading
   a GatewayConfig custom resource into Kubernetes.

   Below example will set the authentication realm `stunner.l7mp.io` and refer STUNner to take the
   TURN authentication credentials from the Kubernetes Secret called `stunner-auth-secret` in the
   `stunner` namespace.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: stunner.l7mp.io/v1
   kind: GatewayConfig
   metadata:
     name: stunner-gatewayconfig
     namespace: stunner
   spec:
     realm: stunner.l7mp.io
     logLevel: "all:DEBUG"
     authRef:
       name: stunner-auth-secret
       namespace: stunner
   EOF
   ```

   Setting the Secret as below will set the [`static` authentication](/docs/AUTH.md) mechanism for
   STUNner using the username/password pair `user-1/pass-1`.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: v1
   kind: Secret
   metadata:
     name: stunner-auth-secret
     namespace: stunner
   type: Opaque
   stringData:
     type: static
     username: user-1
     password: pass-1
   EOF
   ```

   Note that these steps are required only once per STUNner installation.

1. At this point, we are ready to [expose STUNner](/docs/GATEWAY.md#gateway) to clients! This occurs
   by loading a
   [Gateway](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.Gateway)
   resource into Kubernetes.

   In the below example, we open a STUN/TURN listener service on the UDP port 3478.  STUNner will
   automatically create the STUN/TURN server that will run the Gateway and expose it on a public IP
   address and port. Then clients can connect to this listener and, once authenticated, STUNner
   will forward client connections to an arbitrary service backend *inside* the cluster. Make sure
   to set the `gatewayClassName` to the name of the above GatewayClass; this is the way STUNner
   will know how to assign the Gateway with the settings from the GatewayConfig (e.g., the
   STUN/TURN credentials).

   ```console
   kubectl apply -f - <<EOF
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
   EOF
   ```

1. The final step is to tell STUNner what to do with the client connections received on the
   Gateway. This occurs by attaching a
   [UDPRoute](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1alpha2.UDPRoute)
   resource to the Gateway by setting the `parentRef` to the Gateway's name and specifying the
   target service in the `backendRef`.

   The below UDPRoute will configure STUNner to [route client
   connections](/docs/GATEWAY.md#udproute) received on the Gateway called `udp-gateway` to the
   WebRTC media server pool identified by the Kubernetes service `media-plane` in the `default`
   namespace.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: stunner.l7mp.io/v1
   kind: UDPRoute
   metadata:
     name: media-plane
     namespace: stunner
   spec:
     parentRefs:
       - name: udp-gateway
     rules:
       - backendRefs:
           - name: media-plane
             namespace: default
   EOF
   ```

Note that STUNner deviates somewhat from the way Kubernetes handles ports in Services. In
Kubernetes each Service is associated with one or more protocol-port pairs and connections via the
Service can be made to only these specific protocol-port pairs. WebRTC media servers, however,
usually open lots of different ports, typically one per each client connection, and it would be
cumbersome to create a separate backend Service and UDPRoute per each port. In order to simplify
this, STUNner **ignores the protocol and port specified in the backend service** and allows
connections to the backend pods via *any* protocol-port pair. STUNner can therefore use only a
*single* backend Service to reach any port exposed on a WebRTC media server.

And that's all. You don't need to worry about client-side NAT traversal and WebRTC media routing
because STUNner has you covered!  Even better, every time you change a Gateway API resource in
Kubernetes, say, you update the GatewayConfig to reset the STUN/TURN credentials or change the
protocol or port in a Gateway, the [STUNner gateway
operator](https://github.com/l7mp/stunner-gateway-operator) will automatically pick up your
modifications and update the underlying dataplane. Kubernetes is beautiful, isn't it?

## Fire up a backend service

We have successfully configured STUNner to route client connections to the `media-plane` service
but at the moment there is no backend there that would respond. Below we use a simplistic UDP
greeter service for testing: every time you send some input, the greeter service will respond with
a heartwarming welcome message.

The below manifest spawns the service in the `default` namespace and wraps it in a Kubernetes
service called `media-plane`. Recall, this is the target service in our UDPRoute. Note that the
type of the `media-plane` service is `ClusterIP`, which means that Kubernetes will *not* expose it
to the outside world: the only way for clients to obtain a response is via STUNner.

```console
kubectl apply -f https://raw.githubusercontent.com/l7mp/stunner/refs/heads/main/docs/examples/udp-echo/udp-greeter.yaml
```

## Check your config

The current STUNner dataplane configuration is always made available via the convenient
[`stunnerctl`](/cmd/stunnerctl/README.md) CLI utility. The below will dump the config of the UDP
gateway in human readable format.

```console
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

As it turns out, STUNner has successfully assigned a public IP and port to our Gateway and set the
STUN/TURN credentials based on the GatewayConfig.

## Testing

Now we are ready to test our UDP greeter service.

First we need the ClusterIP assigned by Kubernetes to the `media-plane` service.

```console
export PEER_IP=$(kubectl get svc media-plane -o jsonpath='{.spec.clusterIP}')
```

We also need a STUN/TURN client to actually initiate a connection. STUNner comes with a handy
STUN/TURN client called [`turncat`](/docs/cmd/turncat.md) for this purpose. Once
[installed](/cmd/turncat/README.md#installation), you can fire up `turncat` to listen on the
standard input and send everything it receives to STUNner. Type any input and press Enter, and you
should see a nice greeting from your cluster!

```console
./turncat - k8s://stunner/udp-gateway:udp-listener udp://${PEER_IP}:9001
Hello STUNner
Greetings from STUNner!
```

Note that we haven't specified the public IP address and port: `turncat` is clever enough to parse
the running [STUNner configuration](#check-your-config) from Kubernetes directly. Just specify the
special STUNner URI `k8s://stunner/udp-gateway:udp-listener`, using the namespace (`stunner` here)
and the name of the Gateway (`udp-gateway`) plus the listener you want to connect to
(`udp-listener`), and `turncat` will do the heavy lifting.

Note that your actual WebRTC clients will *not* need to use `turncat` to reach the cluster: all
modern Web browsers and WebRTC clients come with a STUN/TURN client built in. Here, `turncat` is
used only to *simulate* what a real WebRTC client would do when trying to reach STUNner.

## Reconcile

Any time you see fit, you can update the STUNner configuration through the Gateway API: STUNner
will automatically reconcile the dataplane for the new configuration.

For instance, you may decide to open up your WebRTC infrastructure on TLS/TCP as well; say, because
an enterprise NAT on the client network path has gone berserk and actively filters anything except
TLS/443. The below steps will do just that: open another gateway on STUNner, this time on the
TLS/TCP port 443, and reattach the UDPRoute to both Gateways so that no matter which protocol a
client may choose the connection will be routed to the `media-plane` service (i.e., the UDP
greeter) by STUNner. (Note that you could also add the TLS listener to the *existing* Gateway
instead of creating a *new* one, here we just use the simpler approach for brevity.)

1. Store your TLS certificate in a Kubernetes Secret. Below we create a self-signed certificate for
   testing, make sure to substitute this with a valid certificate.

   ```console
   openssl genrsa -out ca.key 2048
   openssl req -x509 -new -nodes -days 365 -key ca.key -out ca.crt -subj "/CN=yourdomain.com"
   kubectl -n stunner create secret tls tls-secret --key ca.key --cert ca.crt
   ```

1. Add the new TLS Gateway. Notice how the `tls-listener` now contains a `tls` object that refers
   the above Secret, this way assigning the TLS certificate to use with our TURN-TLS listener.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1beta1
   kind: Gateway
   metadata:
     name: tls-gateway
     namespace: stunner
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
       - name: tls-listener
         port: 443
         protocol: TURN-TLS
         tls:
           mode: Terminate
           certificateRefs:
             - kind: Secret
               namespace: stunner
               name: tls-secret
   EOF
   ```

1. Update the UDPRoute to attach it to both Gateways.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: stunner.l7mp.io/v1
   kind: UDPRoute
   metadata:
     name: media-plane
     namespace: stunner
   spec:
     parentRefs:
       - name: udp-gateway
       - name: tls-gateway
     rules:
       - backendRefs:
           - name: media-plane
             namespace: default
   EOF
   ```

1. Fire up `turncat` again, but this time let it connect through TLS. This is achieved by
   specifying the name of the TLS listener (`tls-listener`) in the STUNner URI. The `-i` command
   line argument (`--insecure`) is added to prevent `turncat` from rejecting our insecure
   self-signed TLS certificate; this will not be needed when using a real signed certificate.

   ```console
   ./turncat -i -l all:INFO - k8s://stunner/tls-gateway:tls-listener udp://${PEER_IP}:9001
   [...] turncat INFO: Turncat client listening on -, TURN server: tls://10.96.55.200:443, peer: udp://10.104.175.57:9001
   [...]
   Hello STUNner
   Greetings from STUNner!
   ```

   We have set the `turncat` loglevel to INFO to learn that this time `turncat` has connected via
   the TURN server `tls://10.96.55.200:443`. And that's it: STUNner automatically routes the
   incoming TLS/TCP connection to the UDP greeter service, silently converting from TLS/TCP to UDP
   in the background and back again on return.

## Cleaning up

Stop `turncat` and wipe all Kubernetes configuration.

```console
kubectl delete -f https://raw.githubusercontent.com/l7mp/stunner/refs/heads/main/docs/examples/udp-echo/udp-greeter.yaml
kubectl delete gatewayclass stunner-gatewayclass
kubectl -n stunner delete secret stunner-auth-secret
kubectl -n stunner delete gatewayconfig stunner-gatewayconfig
kubectl -n stunner delete gateway udp-gateway
kubectl -n stunner delete gateway tls-gateway
kubectl -n stunner delete secret tls-secret
kubectl -n stunner delete udproutes.stunner.l7mp.io media-plane
```
