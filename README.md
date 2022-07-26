# STUNner: A Kubernetes ingress gateway for WebRTC

Ever wondered how to [deploy your WebRTC infrastructure into the
cloud](https://webrtchacks.com/webrtc-media-servers-in-the-cloud)? Frightened away by the
complexities of Kubernetes container networking, and the surprising ways in which it may interact
with your UDP/RTP media? Tried to read through the endless stream of [Stack
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
modification to your existing WebRTC codebase.

## Table of Contents
1. [Description](#description)
2. [Features](#features)
3. [Getting started](#getting-started)
4. [Tutorials](#tutorials)
5. [Documentation](#documentation)
6. [Caveats](#caveats)
7. [Milestones](#milestones)

## Description

Currently [WebRTC](https://stackoverflow.com/search?q=kubernetes+webrtc)
[lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[a](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[vitualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
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
which may create a useless dependency on externally operated services, introduce a bottleneck,
raise security concerns, and come with a non-trivial price tag.

The main goal of STUNner is to allow *anyone* to deploy their own WebRTC infrastructure into
Kubernetes, without relying on any external service other than the cloud-provider's standard hosted
Kubernetes offering. This is achieved by STUNner acting as a gateway for ingesting WebRTC media
traffic into the Kubernetes cluster, exposing a public-facing STUN/TURN server that WebRTC clients
can connect to.

In the *headless deployment model* STUNner acts as a simple scalable STUN/TURN server that WebRTC
clients can use as a NAT traversal facility for establishing a media connection. This is not that
much different from a standard public STUN/TURN server setup, but in this case the STUN/TURN
servers are deployed into Kubernetes, which makes lifecycle management, scaling and cost
optimization infinitely simpler.

![STUNner headless deployment architecture](./doc/stunner_standalone_arch.svg)

In the fully fledged *media-plane deployment model* STUNner implements a STUN/TURN ingress gateway
service that WebRTC clients can use to open a transport relay connection to the media servers
running *inside* the Kubernetes cluster. This makes it possible to deploy WebRTC application
servers and media servers into ordinary Kubernetes pods, taking advantage of Kubernetes's excellent
tooling to manage, scale, monitor and troubleshoot the WebRTC infrastructure like any other
cloud-bound workload.

![STUNner media-plane deployment architecture](./doc/stunner_arch.svg)

Don't worry about the performance implications of processing all your media through a TURN server:
STUNner is written in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool so you don't pay the round-trip time to a far-away public STUN/TURN server, and
STUNner can be easily scaled up if needed, just like any other "normal" Kubernetes service.

The recommended way to configure STUNner is via the standard [Kubernetes Gateway
API](https://gateway-api.sigs.k8s.io): you specify the way you want to expose your WebRTC services
in the familiar YAML-engineering style and the [STUNner gateway
operator](https://github.com/l7mp/stunner-gateway-operator) reconciles the dataplane, updates
STUN/TURN credentials, and exposes your STUNner
[Gateways](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1alpha2.Gateway)
in LoadBalancer services, and all this happens automatically.

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
  HTTPS port for the application server and a *single* UDP port for *all* your media.

* **No reliance on external services for NAT traversal.** Can't afford a decent [hosted TURN
  service](https://bloggeek.me/webrtc-turn) for client-side NAT traversal? Can't get a decent
  audio/video quality because the third-party TURN service poses a bottleneck? STUNner can be
  deployed into the same cluster as the rest of your WebRTC infrastructure, and any WebRTC client
  can connect to it directly without the use of *any* external STUN/TURN service whatsoever, apart
  from STUNner itself.

* **Easily scale your WebRTC infrastructure.** Tired of manually provisioning your WebRTC media
  servers?  STUNner lets you deploy the entire WebRTC infrastructure into ordinary Kubernetes pods,
  thus scaling the media plane is as easy as issuing a `kubectl scale` command. STUNner itself can
  be scaled with similar ease, completely separately from the media servers.

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media server for
  potentially malicious access; with STUNner *all* media is received through a single ingress port
  that you can tightly monitor and control. 

<!-- STUNner stores all STUN/TURN credentials and DTLS keys -->
<!--   in secure Kubernetes vaults. -->

* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundred
  lines of fully open-source code. The server is extremely lightweight: the typical STUNner
  container image size is only about 5 Mbytes.

## Getting Started

STUNner comes with a helm chart to fire up a fully functional STUNner-based WebRTC media gateway in
minutes. Note that the default installation does not contain an application server and a media
server: STUNner in itself is not a WebRTC media server, it is just an *enabler* for you to deploy
your *own* WebRTC infrastructure into Kubernetes. Once installed, STUNner makes sure that your
media servers are readily reachable to WebRTC clients, despite running with a private IP address
inside a Kubernetes pod.

With a minimal understanding of WebRTC and Kubernetes, deploying STUNner should take less than 5
minutes.

* [Customize STUNner and deploy it](#installation) into your Kubernetes cluster.
* Optionally [deploy a WebRTC media server](examples/kurento-one2one-call) into Kubernetes.
* [Set STUNner as the ICE server](#configuring-webrtc-clients) in your WebRTC clients.
* ...
* Profit!!

### Installation

The simplest way to deploy STUNner is through [Helm](https://helm.sh). STUNner configuration
parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files). We recommend deploying STUNner
into a separate `stunner` namespace to isolate it from the rest of the workload.

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner-gateway-operator --set stunner.namespace=stunner
helm install stunner-gateway-operator stunner/stunner-gateway-operator
```

### Configuration

The standard way to interact with STUNner is via Kubernetes, using the [Kubernetes Gateway
  API](https://gateway-api.sigs.k8s.io). This is much akin to the way you configure _all_
  Kubernetes workloads: specify your intents in YAML files and issue a `kubectl apply`, and the
  [STUNner gateway operator](https://github.com/l7mp/stunner-gateway-operator) will automatically
  reconcile the STUNner dataplane for the new configuration.

1. Given a fresh STUNner install, the first step is to register STUNner with the Kubernetes Gateway
   API. This amounts to creating a
   [GatewayClass](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1alpha2.GatewayClass),
   which serves as the root level configuration for your STUNner deployment. 
   
   Each GatewayClass must specify a controller that will manage the Gateway objects created under
   the class hierarchy. In our case this must be set to `stunner.l7mp.io/gateway-operator` for
   STUNner to pick up the GatewayClass. In addition, a GatewayClass can refer to another object
   using a `parametersRef`, which allows to add further implementation-specific configuration. The
   below example refers to a GatewayConfig (see next), which will define some general configuration
   for the STUNner dataplane.
   
   ``` console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1alpha2
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
     description: "STUNner is a WebRTC ingress gateway for Kubernetes"
   EOF
   ```

1. The next step is to set some general configuration for STUNner, especially the STUN/TURN
   authentication [credentials](https://github.com/l7mp/stunner/blob/main/doc/AUTH.md) clients can
   use to reach STUNner. This requires loading a GatewayConfig custom resource into
   Kubernetes. 
   
   In the below GatewayConfig, we instruct STUNner to use the [`plaintext`
   authentication](doc/AUTH.md) mechanism, using the username/password pair `user-1/pass-1`, and
   the authentication realm `stunner.l7mp.io`; see the package
   [docs](https://pkg.go.dev/github.com/l7mp/stunner-gateway-operator) on further configuration
   options.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: stunner.l7mp.io/v1alpha1
   kind: GatewayConfig
   metadata:
     name: stunner-gatewayconfig
     namespace: stunner
   spec:
     realm: stunner.l7mp.io
     authType: plaintext
     userName: "user-1"
     password: "pass-1"
   EOF
   ```

   Note that these two steps are required only once per STUNner deployment.

1. At this point, we are ready to expose STUNner to clients! This occurs by loading a
   [Gateway](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1alpha2.Gateway)
   into Kubernetes.
   
   In the below example, we open a STUN/TURN listener service on the UDP listener port 3478.
   STUnner will automatically expose this listener on a public IP address and port (by creating a
   LoadBalancer Kubernetes service for each Gateway), await clients to connect to this listener
   and, once authenticated, forward client connections to an arbitrary service backend *inside* the
   cluster. Note that we set the `gatewayClassName` to the name of the above GatewayClass; this is
   the way STUNner will know which STUN/TURN credentials to use with this listener.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1alpha2
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
   EOF
   ```

1. The finally step is to tell STUNner what to do with the client connections received on a Gateway
   listener. In the [media-plane deployment model](#description) we will want to route client
   connections to a [WebRTC media server](examples/kurento-one2one-call), but we may also let
   connections to [loop back to STUNner itself](examples//direct-one2one-call), realizing STUNner's
   [headless deployment model](#description). 
   
   Below we use the first model: we attach a
   [UDPRoute](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1alpha2.UDPROute)
   to our Gateway (this is done by setting the `parentRef` to the name of the Gateway,
   `udp-gateway`). This instructs STUNner to route client connections received on the Gateway to
   the WebRTC server pool specified in the `backendRef`; in our case, the target is the Kubernetes
   service `media-plane` that lives in the `default` namespace.

   ```console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1alpha2
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

And that's all: once configured, STUNner will make all this happen automatically, and you don't
need to worry about client-side NAT traversal and request routing because STUNner has you covered!
Even better, every time you change a Gateway API resource in Kubernetes, say, you update the
GatewayConfig to reset your STUN/TURN credentials, the [STUNner gateway
operator](https://github.com/l7mp/stunner-gateway-operator) automatically updates the underlying
dataplane in a matter of milliseconds. Kubernetes is beautiful, isn't it?

### Check your config

The current STUNner dataplane configuration is always made available in a convenient ConfigMap
called `stunnerd-config` (you can choose the name in the GatewayConfig). There is one rule: the
ConfigMap always lives in the same namespace as the GatewayConfig that belongs to it. The STUNner
dataplane pods themselves will also pick up the configuration from this ConfigMap, so you can
consider the content as the ground truth: whatever is in the ConfigMap is exactly the configuration
last reconciled by the STUNner dataplane.

STUNner comes with a small utility to dump the running configuration in human readable format (you must have
[`jq`](https://stedolan.github.io/jq) installed in your PATH to be able to use it). Chdir into the
main STUNner directory and issue:
```console
cmd/stunnerctl/stunnerctl get-config stunner/stunnerd-config
STUN/TURN authentication type:	plaintext
STUN/TURN username:		user-1
STUN/TURN password:		pass-1
Listener:	udp-listener
Protocol:	UDP
Public address:	34.118.36.108
Public port:	3478
```

As shown above, STUNner assigned a public IP and port for our Gateway listener and set the
STUN/TURN credentials based on the GatewayConfig.

The below will dump the entire running configuration; `jq` is there just to pretty-print JSON:
```console
kubectl get cm -n stunner stunnerd-config -o jsonpath="{.data.stunnerd\.conf}" | jq .
```

### Testing

To test STUNner, we will need to deploy an actual [WebRTC media
server](examples/kurento-one2one-call) behind STUNner. Below we use a a quick and dirty UDP greeter
service just to test STUNner, but see the [tutorials](#tutorials) on how to expose actual WebRTC
media servers via STUNner.

1. Fire up the UDP greeter service: every time you send some input the service will respond with a
   heartwarming greeting. The below manifest spawns the greeter service in the `default` namespace
   and wraps it with a Kubernetes service called `media-plane`.

   ```console
   kubectl apply -f examples/simple-tunnel/udp-greeter.yaml
   ```

1. We also need a STUN/TURN client to actually connect to STUNner. The installation comes with a
   handy STUN/TURN client called [`turncat`](/cmd/turncat) for this purpose. First, build
   `turncat`.

   ```console
   cd stunner
   go build -o turncat cmd/turncat/main.go
   ```

1. We will use `turncat` to connect to the UDP greeter service via STUNner, but for this we need to
   learn the ClusterIP assigned by Kubernetes to the `media-plane` service (recall, this service is
   the backend service we specified in the UDPRoute). 

   ```console
   export PEER_IP=$(kubectl get svc media-plane -o jsonpath='{.spec.clusterIP}')
   ```

1. Now we can start `turncat` at last! Send any input: pressing Enter you should see a nice
   greeting from your cluster!

   ```console
   ./turncat - k8s://stunner/stunnerd-config:udp-listener udp://${PEER_IP}:9001
   Hello STUNner
   Greetings from STUNner!
   ```
   
   Observe how we haven't specified the STUNner public IP address and port for `turncat` in any
   way; `turncat` is clever enough to read the [running configuration](#check-your-config) from
   Kubernetes directly; just specify the special STUNner URI
   `k8s://stunner/stunnerd-config:udp-listener`, identifying the namespace and the name for the
   STUNner ConfigMap and the name of the listener to connect to, and `turncat` will do the heavy
   lifting!

### Reconcile

Any time you see fit, you can update the STUNner configuration through the Gateway API: the STUNner
gateway operator will make sure to reconcile the underlying dataplane for the new config. 

For instance, you may want to expose STUNner on TCP as well; say, because an enterprise NAT went
berserk and started to filter clients' UDP traffic. The below will do just that: open another
listener on STUNner, this time on the TCP port 3478, and reattach the UDPRoute to both Gateways so
that no matter which protocol a client connection was received on it will be routed to the
`media-plane` service (i.e., the UDP greeter).

1. Add the new TCP Gateway.
   ```console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1alpha2
   kind: Gateway
   metadata:
     name: tcp-gateway
     namespace: stunner
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
       - name: tcp-listener
         port: 3478
         protocol: TCP
   EOF
   ```

1. Update the UDPRoute so that it attaches to both Gateways.
   ```console
   kubectl apply -f - <<EOF
   apiVersion: gateway.networking.k8s.io/v1alpha2
   kind: UDPRoute
   metadata:
     name: media-plane
     namespace: stunner
   spec:
     parentRefs:
       - name: udp-gateway
       - name: tcp-gateway
     rules:
       - backendRefs:
           - name: media-plane
             namespace: default
   EOF
   ```

1. Fire up `turncat` again, but this time let it connect through the TCP Gateway. This is achieved
   by specifying the name of the TCP listener (`tcp-listener`) in the STUNner URI.
   ```console
   export PEER_IP=$(kubectl get svc media-plane -o jsonpath='{.spec.clusterIP}')
   ./turncat -l all:INFO - k8s://stunner/stunnerd-config:tcp-listener udp://${PEER_IP}:9001
   [...] turncat INFO: Turncat client listening on -, TURN server: TCP://34.118.18.210:3478, peer: udp://10.120.0.127:9001
   [...]
   Hello STUNner
   Greetings from STUNner!
   ```
   
We have set the `turncat` loglevel to INFO so that we can learn that `turncat` has connected via
the TURN server `TCP://34.118.18.210:3478` this time. And that's it: STUNner automatically routes
the incoming TCP connection to the UDP greeter service, silently converting from TCP to UDP in the
background and back again on return.

### Configuring WebRTC clients

Real WebRTC clients will need a valid ICE server configuration to use STUNner as the TURN
server. STUNner is compatible with all client-side [TURN auto-discovery
mechanisms](https://datatracker.ietf.org/doc/html/rfc8155). When no auto-discovery mechanism is
available, clients will need to be manually configured to stream audio/video media over STUNner.

The below JavaScript snippet will direct a WebRTC client to use STUNner.  Make sure to substitute
the placeholders (like `<STUNNER_PUBLIC_ADDR>`) with the correct configuration from the running
STUNner config; don't forget that `stunnerctl` is always there for you if lost.

```js
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=udp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

Note that STUNner comes with a [small Node.js
library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib) that simplifies generating ICE
configurations and STUNner credentials in the application server.

## Tutorials

STUNner comes with several tutorials that show how to use it to deploy different WebRTC
applications into Kubernetes.

* [Opening a UDP tunnel via STUNner](examples/simple-tunnel): This introductory tutorial shows how
  to tunnel an external connection via STUNner to a UDP service deployed into Kubernetes. The demo
  can be used to quickly check a STUNner installation.
* [Direct one to one video call via STUNner](examples/direct-one2one-call): This tutorial showcases
  the [headless deployment model](#description) of STUNner, that is, when WebRTC clients connect to
  each other directly via STUNner using it as a TURN server but without the mediation of a WebRTC
  media server.
* [One to one video call with Kurento via STUNner](examples/kurento-one2one-call): This tutorial
  extends the previous demo to showcase the [media-plane deployment model](#description), that is,
  when WebRTC clients connect to each other via a media server deployed into Kubernetes. This time,
  the media server is provided by [Kurento](https://www.kurento.org), but you can easily substitute
  your favorite media server instead of Kurento. STUNner then acts an ingress gateway, conveniently
  ingesting WebRTC media into the Kubernetes cluster and routing it to Kurento, and all this
  happens *without* modifying the media server code in any way, just by adding 5-10 lines of
  straightforward JavaScript to the application server to configure clients to use STUNner as the
  TURN server.
* [Media-plane mode: Magic mirror via STUNner](examples/kurento-magic-mirror/README.md): This
  tutorial has been adopted from the [Kurento](https://www.kurento.org) [magic
  mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
  demo. The demo shows a basic WebRTC loopback server with some media processing added: the
  application uses computer vision and augmented reality techniques to add a funny hat on top of
  faces. The computer vision functionality is again provided by the [Kurento media
  server](https://www.kurento.org), being exposed to the clients via a STUNner gateway.
* [Cloud-gaming with STUNner](examples/cloudretro/README.md): If this was still not enough from the
  fun, this tutorial lets you play Super Mario or Street Fighter in your browser, courtesy of the
  amazing [CloudRetro](https://cloudretro.io) team and of course STUNner. The tutorial shows how to
  deploy CloudRetro into Kubernetes, expose the media port via STUnner, and have endless
  retro-gaming fun!

## Documentation

See further documentation [here](doc/README.md).

## Caveats

STUNner is a work-in-progress. Some features are missing, others may not work as expected. The
notable limitations at this point are as follows.

* *STUNner is not intended to be used as a public STUN/TURN server.* The intended use of STUNner is
  as a Kubernetes ingress gateway for WebRTC. Being deployed into a Kubernetes service, STUNner
  will not be able to identify the public IP address of a client sending a STUN binding request to
  it (without special
  [hacks](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip)),
  and the TURN transport relay connection opened by a WebRTC client via STUNner is reachable only
  to clients configured to use the same STUNner service (again, without further
  [hacks](https://kubernetes.io/docs/concepts/security/pod-security-policy/#host-namespaces)). This
  is intended: STUNner is a Kubernetes ingress gateway which happens to expose a STUN/TURN
  compatible service to WebRTC clients, and not a public TURN service.
* STUNner supports arbitrary scale-up without dropping active calls, but *scale-down might
  disconnect calls* established through the STUNner pods and/or media server replicas being removed
  from the load-balancing pool. Note that this problem is
  [universal](https://webrtchacks.com/webrtc-media-servers-in-the-cloud) in WebRTC, but we plan to
  do something about it in a later STUNner release so stay tuned.
* The WebRTC DataChannel API is not supported at the moment.

## Milestones

* v0.9: First public release: STUNner basic UDP/TURN connectivity + helm chart + tutorials
* v0.10: Onboarding: long-term STUN/TURN credentials and [STUN/TURN over
  TCP/TLS/DTLS](https://www.rfc-editor.org/rfc/rfc6062.txt).
* v0.11: Day-2 operations: STUNner Kubernetes operator.
* v0.12: Observability: Prometheus + Grafana dashboard.
- v0.13: Performance: eBPF acceleration
- v1.0: GA (this fall)
- v2.0: Service mesh: adaptive scaling & resiliency

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
