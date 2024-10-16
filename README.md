<p align="center">
  <img alt="STUNner", src="docs/img/stunner.svg" width="50%" height="50%"></br>
  <a href="https://discord.gg/DyPgEsbwzc" alt="Discord">
    <img alt="Discord" src="https://img.shields.io/discord/945255818494902282" /></a>
  <a href="https://go.dev/doc/go1.17" alt="Go">
    <img src="https://img.shields.io/github/go-mod/go-version/l7mp/stunner" /></a>
  <a href="https://pkg.go.dev/github.com/l7mp/stunner">
    <img src="https://pkg.go.dev/badge/github.com/l7mp/stunner.svg" alt="Go Reference"></a>
  <a href="https://hub.docker.com/repository/docker/l7mp/stunnerd/tags?page=1&ordering=last_updated" alt="Docker version">
    <img src="https://img.shields.io/docker/v/l7mp/stunnerd" /></a>
  <a href="https://github.com/l7mp/stunner/stargazers" alt="Github stars">
    <img src="https://img.shields.io/github/stars/l7mp/stunner?style=social" /></a>
  <a href="https://github.com/l7mp/stunner/network/members" alt="Github Forks">
    <img src="https://img.shields.io/github/forks/l7mp/stunner?style=social" /></a>
  <a href="https://github.com/l7mp/stunner/blob/main/LICENSE" alt="MIT">
    <img src="https://img.shields.io/github/license/l7mp/stunner" /></a>
  <a href="https://github.com/l7mp/stunner/pulls?q=is%3Apr+is%3Aclosed" alt="PRs closed">
    <img src="https://img.shields.io/github/issues-pr-closed/l7mp/stunner" /></a>
  <a href="https://github.com/l7mp/stunner/pulls?q=is%3Aopen+is%3Apr" alt="PRs open">
    <img src="https://img.shields.io/github/issues-pr/l7mp/stunner" /></a>
  <a href="https://github.com/l7mp/stunner/issues?q=is%3Aissue+is%3Aclosed" alt="Issues closed">
    <img src="https://img.shields.io/github/issues-closed/l7mp/stunner" /></a>
  <a href="https://github.com/l7mp/stunner/issues?q=is%3Aopen+is%3Aissue" alt="Issues open">
    <img src="https://img.shields.io/github/issues/l7mp/stunner" /></a>
  <a href="https://hub.docker.com/repository/docker/l7mp/stunnerd" alt="Docker pulls">
    <img src="https://img.shields.io/docker/pulls/l7mp/stunnerd" /></a>
  <a href="https://stunner.readthedocs.io/en/latest/" alt="Read the Docs">
    <img src="https://readthedocs.org/projects/stunner/badge/?version=latest" /></a>
  <a href="https://github.com/l7mp/stunner/actions/workflows/test.yml" alt="Tests">
    <img src="https://github.com/l7mp/stunner/actions/workflows/test.yml/badge.svg" /></a>
  <a href="https://coveralls.io/github/l7mp/stunner" alt="coverage">
    <img src="https://img.shields.io/coveralls/github/l7mp/stunner" /></a>
</p>

*Note: This page documents the latest development version of STUNner. See the documentation for the stable version [here](https://docs.l7mp.io/en/stable).*

# STUNner: A Kubernetes media gateway for WebRTC

Ever wondered how to [deploy your WebRTC infrastructure into the
cloud](https://webrtchacks.com/webrtc-media-servers-in-the-cloud)? Frightened away by the
complexities of Kubernetes container networking, and the surprising ways in which it may interact
with your UDP/RTP media? Read through the endless stream of [Stack
Overflow](https://stackoverflow.com/search?q=kubernetes+webrtc)
[questions](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[asking](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[how](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[to](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp)
[scale](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
WebRTC services with Kubernetes, just to get (mostly) insufficient answers?  Want to safely connect
your users behind a NAT, without relying on expensive [third-party TURN
services](https://bloggeek.me/managed-webrtc-turn-speed)?

Worry no more! STUNner allows you to deploy *any* WebRTC service into Kubernetes, smoothly
integrating it into the [cloud-native ecosystem](https://landscape.cncf.io).  STUNner exposes a
standards-compliant STUN/TURN gateway for clients to access your virtualized WebRTC infrastructure
running in Kubernetes, maintaining full browser compatibility and requiring minimal or no
modification to your existing WebRTC codebase.  STUNner supports the [Kubernetes Gateway
API](https://gateway-api.sigs.k8s.io) so you can configure it in the familiar YAML-engineering
style via Kubernetes manifests.

## Table of Contents
1. [Description](#description)
1. [Features](#features)
1. [Getting started](#getting-started)
1. [Tutorials](#tutorials)
1. [Documentation](#documentation)
1. [Caveats](#caveats)
1. [Milestones](#milestones)

## Description

Currently [WebRTC](https://stackoverflow.com/search?q=kubernetes+webrtc)
[lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[a](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[virtualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[story](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp): there
is no easy way to deploy a WebRTC media service into Kubernetes to benefit from the
[resiliency](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce),
[scalability](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes),
and [high
availability](https://blog.cloudflare.com/announcing-our-real-time-communications-platform)
features we have come to expect from modern network services. Worse yet, the entire industry relies
on a handful of [public](https://bloggeek.me/google-free-turn-server/) [STUN
servers](https://www.npmjs.com/package/freeice) and [hosted TURN
services](https://bloggeek.me/managed-webrtc-turn-speed) to connect clients behind a NAT/firewall,
which may create a useless dependency on externally operated services, introduce a performance
bottleneck, raise security concerns, and come with a non-trivial price tag.

The main goal of STUNner is to allow *anyone* to deploy their own WebRTC infrastructure into
Kubernetes, without relying on any external service other than the cloud-provider's standard hosted
Kubernetes offering. STUNner can act as a standalone STUN/TURN server that WebRTC clients and media
servers can use as a scalable NAT traversal facility (headless model), or it can act as a gateway
for ingesting WebRTC media traffic into the Kubernetes cluster by exposing a public-facing
STUN/TURN server that WebRTC clients can connect to (media-plane model). This makes it possible to
deploy WebRTC application servers and media servers into ordinary Kubernetes pods, taking advantage
of the full cloud native feature set to manage, scale, monitor and troubleshoot the WebRTC
infrastructure like any other Kubernetes workload.

![STUNner media-plane deployment architecture](./docs/img/stunner_arch.svg)

Don't worry about the performance implications of processing all your media through a TURN server:
STUNner is written in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool so you don't pay the round-trip time to a far-away public STUN/TURN server, and
STUNner can be easily scaled up if needed just like any other "normal" Kubernetes service.

## Features

Kubernetes has been designed and optimized for the typical HTTP/TCP Web workload, which makes
streaming workloads, and especially UDP/RTP based WebRTC media, feel like a foreign citizen.
STUNner aims to change this state-of-the-art, by exposing a single public STUN/TURN server port for
ingesting *all* media traffic into a Kubernetes cluster in a controlled and standards-compliant
way.

* **Seamless integration with Kubernetes.** STUNner can be deployed into any Kubernetes cluster,
  even into restricted ones like GKE Autopilot, using a single command. Manage your HTTP/HTTPS
  application servers with your favorite [service mesh](https://istio.io), and STUNner takes care
  of all UDP/RTP media. STUNner implements the [Kubernetes Gateway
  API](https://gateway-api.sigs.k8s.io) so you configure it in exactly the same way as
  [the](https://doc.traefik.io/traefik/routing/providers/kubernetes-gateway)
  [rest](https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api)
  [of](https://projectcontour.io/guides/gateway-api)
  [your](https://docs.konghq.com/kubernetes-ingress-controller/latest/guides/using-gateway-api)
  [workload](https://github.com/nginxinc/nginx-kubernetes-gateway) through easy-to-use YAML
  manifests.

* **Expose a WebRTC media server on a single external UDP port.** Get rid of the Kubernetes
  [hacks](https://kubernetes.io/docs/concepts/configuration/overview), like privileged pods and
  `hostNetwork`/`hostPort` services, typically recommended as a prerequisite to containerizing your
  WebRTC media plane.  Using STUNner a WebRTC deployment needs only two public-facing ports, one
  HTTPS port for signaling and a *single* UDP port for *all* your media.

* **No reliance on external services for NAT traversal.** Can't afford a [hosted TURN
  service](https://bloggeek.me/webrtc-turn) for client-side NAT traversal? Can't get decent
  audio/video quality because the third-party TURN service poses a bottleneck? STUNner can be
  deployed into the same cluster as the rest of your WebRTC infrastructure, and any WebRTC client
  can connect to it directly without the use of *any* external STUN/TURN service whatsoever, apart
  from STUNner itself.

* **Easily scale your WebRTC infrastructure.** Tired of manually provisioning your WebRTC media
  servers?  STUNner lets you deploy the entire WebRTC infrastructure into ordinary Kubernetes pods,
  thus [scaling the media plane](docs/SCALING.md) is as easy as issuing a `kubectl scale`
  command. Or you can use the built in Kubernetes horizontal autoscaler to *automatically* resize
  your workload based on demand.

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media server for
  potentially malicious access; with STUNner *all* media is received through a single ingress port
  that you can tightly monitor and control.

* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundred
  lines of fully open-source code. The server is extremely lightweight: the typical STUNner
  container image size is only 15 Mbytes.

The main uses of STUNner are running a scalable STUN service in Kubernetes to make it possible for
clients to create peer-to-peer WebRTC media connections between themselves (the so called
[*headless deployment
model*](https://github.com/l7mp/stunner/blob/main/docs/DEPLOYMENT.md#headless-deployment-model)),
or as a scalable TURN gateway service for load-balancing clients' media connections across a pool
of WebRTC media servers hosted in Kubernetes pods (called the [*media-plane deployment
model*](https://github.com/l7mp/stunner/blob/main/docs/DEPLOYMENT.md#media-plane-deployment-model)). While
both use cases are fully supported, below we concentrate on the latter use case: deploying STUNner
as a media gateway (see [our blog
post](https://medium.com/l7mp-technologies/deploying-a-scalable-stun-service-in-kubernetes-c7b9726fa41d)
to learn how to deploy a STUN service with STUNner).

## Getting Started

STUNner comes with a [Helm](https://helm.sh) chart to fire up a fully functional STUNner-based
WebRTC media gateway in minutes. Note that the default installation does not contain an application
server and a media server: STUNner is not a WebRTC service, it is merely an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes. Once installed, STUNner makes sure that
your media servers are readily reachable to WebRTC clients, despite running with a private IP
address inside a Kubernetes pod. See the [tutorials](#tutorials) for some ideas on how to deploy an
actual WebRTC application behind STUNner.

With a minimal understanding of WebRTC and Kubernetes, deploying STUNner should take less than 5
minutes.

* [Customize STUNner and deploy it](#installation) into your Kubernetes cluster.
* Optionally [deploy a WebRTC media server](docs/examples/kurento-one2one-call).
* [Set STUNner as the ICE server](#configuring-webrtc-clients) in your WebRTC clients.
* ...
* Profit!!

### Installation

The simplest way to deploy STUNner is through [Helm](https://helm.sh). STUNner configuration
parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files).

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace \
    --namespace=stunner-system
```

Find out more about the charts in the [STUNner-helm repository](https://github.com/l7mp/stunner-helm).

### Configuration

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

### Check your config

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

### Testing

We have successfully configured STUNner to route client connections to the `media-plane` service
but at the moment there is no backend there that would respond. Below we use a simplistic UDP
greeter service for testing: every time you send some input, the greeter service will respond with
a heartwarming welcome message.

1. Fire up the UDP greeter service.

   The below manifest spawns the service in the `default` namespace and wraps it in a Kubernetes
   service called `media-plane`. Recall, this is the target service in our UDPRoute. Note that the
   type of the `media-plane` service is `ClusterIP`, which means that Kubernetes will *not* expose
   it to the outside world: the only way for clients to obtain a response is via STUNner.

   ```console
   kubectl apply -f deploy/manifests/udp-greeter.yaml
   ```

1. We also need the ClusterIP assigned by Kubernetes to the `media-plane` service.

   ```console
   export PEER_IP=$(kubectl get svc media-plane -o jsonpath='{.spec.clusterIP}')
   ```

1. We also need a STUN/TURN client to actually initiate a connection. STUNner comes with a handy
   STUN/TURN client called [`turncat`](cmd/turncat/README.md) for this purpose. Once
   [installed](cmd/turncat/README.md#installation), you can fire up `turncat` to listen on the
   standard input and send everything it receives to STUNner. Type any input and press Enter, and
   you should see a nice greeting from your cluster!

   ```console
   ./turncat - k8s://stunner/udp-gateway:udp-listener udp://${PEER_IP}:9001
   Hello STUNner
   Greetings from STUNner!
   ```

Note that we haven't specified the public IP address and port: `turncat` is clever enough to parse
the running [STUNner configuration](#check-your-config) from Kubernetes directly. Just specify the
special STUNner URI `k8s://stunner/udp-gateway:udp-listener`, identifying the namespace (`stunner`
here) and the name for the Gateway (`udp-gateway`), and the listener to connect to
(`udp-listener`), and `turncat` will do the heavy lifting.

Note that your actual WebRTC clients do *not* need to use `turncat` to reach the cluster: all
modern Web browsers and WebRTC clients come with a STUN/TURN client built in. Here, `turncat` is
used only to *simulate* what a real WebRTC client would do when trying to reach STUNner.

### Reconcile

Any time you see fit, you can update the STUNner configuration through the Gateway API: STUNner
will automatically reconcile the dataplane for the new configuration.

For instance, you may decide to open up your WebRTC infrastructure on TLS/TCP as well; say, because
an enterprise NAT on the client network path has gone berserk and actively filters anything except
TLS/443. The below steps will do just that: open another gateway on STUNner, this time on the
TLS/TCP port 443, and reattach the UDPRoute to both Gateways so that no matter which protocol a
client may choose the connection will be routed to the `media-plane` service (i.e., the UDP
greeter) by STUNner.

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

### Configuring WebRTC clients

Real WebRTC clients will need a valid ICE server configuration to use STUNner as the TURN
server. STUNner is compatible with all client-side [TURN auto-discovery
mechanisms](https://datatracker.ietf.org/doc/html/rfc8155). When no auto-discovery mechanism is
available, clients will need to be manually configured to stream audio/video media over STUNner.

The below JavaScript snippet will direct a WebRTC client to use STUNner as the TURN server.  Make
sure to substitute the placeholders (like `<STUNNER_PUBLIC_ADDR>`) with the correct configuration
from the running STUNner config; don't forget that `stunnerctl` is always there for you to help.

```js
var ICE_config = {
  iceServers: [
    {
      url: 'turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=udp',
      username: <STUNNER_USERNAME>,
      credential: <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

Note that STUNner comes with a built-in [authentication
service](https://github.com/l7mp/stunner-auth-service) that can be used to generate a complete ICE
configuration for reaching STUNner through a [standards
compliant](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) HTTP [REST
API](docs/AUTH.md).

## Tutorials

The below series of tutorials demonstrates how to leverage STUNner to deploy different WebRTC
applications into Kubernetes.

### Basics

* [Opening a UDP tunnel via STUNner](/docs/examples/simple-tunnel/README.md): This introductory tutorial
  shows how to tunnel an external connection via STUNner to a UDP service deployed into
  Kubernetes. The demo can be used to quickly check and benchmark a STUNner installation.

### Headless deployment mode

* [Direct one to one video call via STUNner](/docs/examples/direct-one2one-call/README.md): This
  tutorial showcases STUNner acting as a TURN server for two WebRTC clients to establish
  connections between themselves, without the mediation of a media server.

### Media-plane deployment model

* [Video-conferencing with LiveKit](/docs/examples/livekit/README.md): This tutorial helps you deploy
  the [LiveKit](https://livekit.io) WebRTC media server behind STUNner. The docs also show how to
  obtain a valid TLS certificate to secure your signaling connections, courtesy of the
  [cert-manager](https://cert-manager.io) project, [nip.io](https://nip.io) and [Let's
  Encrypt](https://letsencrypt.org).
* [Video-conferencing with Janus](/docs/examples/janus/README.md): This tutorial helps you deploy a
  fully fledged [Janus](https://janus.conf.meetecho.com/) video-conferencing service into Kubernetes
  behind STUNner. The docs also show how to obtain a valid TLS certificate to secure your signaling
  connections, using [cert-manager](https://cert-manager.io), [nip.io](https://nip.io) and [Let's
  Encrypt](https://letsencrypt.org).
* [Video-conferencing with Elixir WebRTC](/docs/examples/elixir-webrtc/README.md): This tutorial helps
  you deploy a fully fledged [Elixir WebRTC](https://elixir-webrtc.org/) video-conferencing room called
  [Nexus](https://github.com/elixir-webrtc/apps/tree/master/nexus) into Kubernetes
  behind STUNner. The docs also show how to obtain a valid TLS certificate to secure your signaling
  connections, using [cert-manager](https://cert-manager.io), [nip.io](https://nip.io) and [Let's
  Encrypt](https://letsencrypt.org).
* [Video-conferencing with Jitsi](/docs/examples/jitsi/README.md): This tutorial helps you deploy a
  fully fledged [Jitsi](https://jitsi.org) video-conferencing service into Kubernetes behind
  STUNner. The docs also show how to obtain a valid TLS certificate to secure your signaling
  connections, using [cert-manager](https://cert-manager.io), [nip.io](https://nip.io) and [Let's
  Encrypt](https://letsencrypt.org).
* [Video-conferencing with mediasoup](/docs/examples/mediasoup/README.md): This tutorial helps you
  deploy the [mediasoup](https://mediasoup.org/) WebRTC media server behind STUNner. The docs also
  show how to obtain a valid TLS certificate to secure your signaling connections, courtesy of the
  [cert-manager](https://cert-manager.io) project, [nip.io](https://nip.io) and [Let's
  Encrypt](https://letsencrypt.org).
* [Cloud-gaming with Cloudretro](/docs/examples/cloudretro/README.md): This tutorial lets you play Super
  Mario or Street Fighter in your browser, courtesy of the amazing
  [CloudRetro](https://cloudretro.io) project and, of course, STUNner. The demo also presents a
  simple multi-cluster setup, where clients can reach the game-servers in their geographical
  locality to minimize latency.
* [Remote desktop access with Neko](/docs/examples/neko/README.md): This demo showcases STUNner
  providing an ingress gateway service to a remote desktop application. We use
  [neko.io](https://neko.m1k1o.net) to run a browser in a secure container inside the Kubernetes
  cluster, and stream the desktop to clients via STUNner.
* [One to one video call with Kurento](/docs/examples/kurento-one2one-call/README.md): This tutorial
  shows how to use STUNner to connect WebRTC clients to a media server deployed into Kubernetes
  behind STUNner in the [media-plane deployment model](/docs/DEPLOYMENT.md). All this happens
  *without* modifying the media server code in any way, just by adding 5-10 lines of
  straightforward JavaScript to configure clients to use STUNner as the TURN server.
* [Magic mirror with Kurento](/docs/examples/kurento-magic-mirror/README.md): This tutorial has been
  adopted from the [Kurento](https://www.kurento.org) [magic
  mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
  demo, deploying a basic WebRTC loopback server behind STUNner with some media processing
  added. In particular, the application uses computer vision and augmented reality techniques to
  add a funny hat on top of faces.

## Documentation

The documentation of the stable release can be found [here](https://docs.l7mp.io/en/stable). The
documentation for the latest development release can be found [here](/docs/README.md).

## Caveats

STUNner is a work-in-progress. Some features are missing, others may not work as expected. The
notable limitations at this point are as follows.

* STUNner targets only a *partial implementation of the Kubernetes Gateway API.* In particular,
  only GatewayClass, Gateway and UDPRoute resources are supported. This is intended: STUNner
  deliberately ignores some complexity in the [Gateway API](https://gateway-api.sigs.k8s.io) and
  deviates from the prescribed behavior in some cases, all in the name of simplifying the
  configuration process. The [STUNner Kubernetes gateway
  operator](https://github.com/l7mp/stunner-gateway-operator) docs contain a [detailed
  list](https://github.com/l7mp/stunner-gateway-operator#caveats) on the differences.
* STUNner *lacks officially support for IPv6*. Clients and peers reachable only on IPv6 may or may
  not be able connect to STUNner depending on the version you're using. Please file a bug if you
  absolutely need IPv6 support.

## Milestones

* v0.9: Demo release: STUNner basic UDP/TURN connectivity + helm chart + tutorials.
* v0.10: Dataplane: Long-term STUN/TURN credentials and [STUN/TURN over
  TCP/TLS/DTLS](https://www.rfc-editor.org/rfc/rfc6062.txt) in standalone mode.
* v0.11: Control plane: Kubernetes gateway operator and dataplane reconciliation.
* v0.12: Security: Expose TLS/DTLS settings via the Gateway API.
* v0.13: Observability: Prometheus + Grafana dashboard.
* v0.15: Performance: Per-allocation CPU load-balancing for UDP
* v0.16: Management: Managed STUNner dataplane.
* v0.17: First release candidate: All Gateway and STUNner APIs move to v1.
* v0.18: Stabilization: Second release candidate.
* v0.19: The missing pieces: Third release candidate.
* v1.0: GA

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](AUTHORS).

MIT License - see [LICENSE](LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
