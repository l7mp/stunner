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
4. [Examples](#examples)
5. [Documentation](#documentation)
6. [Caveats](#caveats)
7. [Milestones](#milestones)

## Description

Currently [WebRTC](https://stackoverflow.com/search?q=kubernetes+webrtc)
[lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[a](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[vitualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[story](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp): there
is no easy way to deploy a WebRTC backend service into Kubernetes to benefit from the
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

In the *standalone deployment model* STUNner acts as a simple scalable STUN/TURN server that WebRTC
clients can use as a NAT traversal facility for establishing a media connection. This is not that
much different from a standard public STUN/TURN server setup, but in this case the STUN/TURN
servers are deployed into Kubernetes, which makes lifecycle management, scaling and cost
optimization infinitely simpler.

![STUNner standalone deployment architecture](./doc/stunner_standalone_arch.svg)

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

## Features

Kubernetes has been designed and optimized for the typical HTTP/TCP Web workload, which makes
streaming workloads, and especially UDP/RTP based WebRTC media, feel like a foreign citizen.
STUNner aims to change this state-of-the-art, by exposing a single public STUN/TURN server port for
ingesting *all* media traffic into a Kubernetes cluster in a controlled and standards-compliant
way.

* **Seamless integration with Kubernetes.** STUNner can be deployed into any Kubernetes cluster,
  even into restricted ones like GKE Autopilot, using a single command. Manage your HTTP/HTTPS
  application servers with your favorite [service mesh](https://istio.io), and STUNner takes care
  of all UDP/RTP media.

* **Expose a WebRTC media server on a single external UDP port.** Get rid of the Kubernetes
  [hacks](https://kubernetes.io/docs/concepts/configuration/overview), like privileged pods and
  `hostNetwork`/`hostPort` services, typically recommended as a prerequisite to containerizing your
  WebRTC media plane.  Using STUNner a WebRTC deployment needs only two public-facing ports, one
  HTTPS port for the application server and a *single* UDP port for *all* your media.

* **No reliance on external services for NAT traversal.** Can't afford a decent [hosted TURN
  service](https://bloggeek.me/webrtc-turn) for client-side NAT traversal? Can't get good
  audio/video quality because the TURN service poses a bottleneck? STUNner can be deployed into the
  same cluster as the rest of your WebRTC infrastructure, and any WebRTC client can connect to it
  directly, without the use of *any* external STUN/TURN service apart from STUNner itself.

* **Easily scale your WebRTC infrastructure.** Tired of manually provisioning your WebRTC media
  servers?  STUNner lets you deploy the entire WebRTC infrastructure into ordinary Kubernetes pods,
  thus scaling the media plane is as easy as issuing a `kubectl scale` command. STUNner itself can
  be scaled with similar ease, completely separately from the media servers.

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media server for
  potentially malicious access; with STUNner *all* media is received through a single ingress port
  that you can tightly monitor and control. STUNner stores all STUN/TURN credentials and DTLS keys
  in secure Kubernetes vaults, and uses standard Kubernetes Access Control Lists (ACLs) to lock
  down network access between your application servers and the media plane.

* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundred
  lines of fully open-source code. The server is extremely lightweight: the typical STUNner
  container image size is only about 5 Mbytes.

## Getting Started

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server: STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes. Once installed, STUNner makes sure that
your media servers are readily reachable to WebRTC clients, despite running with a private IP
address inside a Kubernetes pod.

With a minimal understanding of WebRTC and Kubernetes, deploying STUNner should not take more than
5 minutes.

* [Customize STUNner and deploy it](#installation) into your Kubernetes cluster and expose it over
  a public IP address and port.
* Optionally [deploy a WebRTC media server](examples/kurento-one2one-call) into Kubernetes as well.
* [Set STUNner as the ICE server](#configuring-webbrtc-clients-to-reach-stunner) in your WebRTC
  clients.
* ...
* Profit!!

### Installation

The simplest way to deploy STUNner is through [Helm](https://helm.sh). In this case, all STUNner
configuration parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files).

```console
$ helm repo add stunner https://l7mp.io/stunner
$ helm repo update
$ helm install stunner stunner/stunner
```

And that's all: a standalone deployment of STUNner is up and running, waiting for WebRTC clients to
connect to it. The last step to do is to direct clients to indeed connect to STUNner.

See the complete STUNner installation guide [here](/doc/INSTALL.md). 

### Configuration

Wait until Kubernetes assigns a public IP address for STUNner; this should not take more than a minute.

```console
$ until [ -n "$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
```

Query the actual STUNner configuration, in particular, the public IP address and port assigned by
Kubernetes for the STUNner service.

```console
$ kubectl get cm stunner-config -o yaml
```

The result should be something like the below.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: stunner-config
  namespace: default
data:
  STUNNER_AUTH_TYPE: plaintext
  STUNNER_PUBLIC_ADDR: A.B.C.D
  STUNNER_PUBLIC_PORT: 3478
  STUNNER_USERNAME: user1
  STUNNER_PASSWORD: passwd1
  ...
```

Note that any change to the STUNner `ConfigMap` will take effect only once STUNner is restarted.

``` console
kubectl rollout restart deployment/stunner
```

## Configuring WebRTC clients to reach STUNner

The last step is to configure your WebRTC clients to use STUNner as the TURN server. STUNner is
compatible with all client-side [TURN auto-discovery
mechanisms](https://datatracker.ietf.org/doc/html/rfc8155). When no auto-discovery mechanism is
available, clients will need to be manually configured to stream audio/video media over STUNner.

The below JavaScript snippet will direct a WebRTC client to use STUNner; make sure to substitute
the placeholders (like `<STUNNER_PUBLIC_ADDR>`) with the correct configuration from the above.

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
library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib) that makes it simpler dealing with
ICE configurations and STUNner credentials in the application server.

## Examples

STUNner comes with several demos to show how to use it to deploy a WebRTC application into
Kubernetes.

* [Opening a UDP tunnel via STUNner](examples/simple-tunnel): This introductory demo shows how to
  tunnel an external connection via STUNner to a UDP service deployed into Kubernetes. The demo can
  be used to quickly check a STUNner installation.
* [Standalone mode: Direct one to one video call via STUNner](examples/direct-one2one-call): This
  introductory tutorial showcases the *standalone deployment model* of STUNner, that is, when
  WebRTC clients connect to each other directly via STUNner, without a media server. The tutorial
  has been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
  tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), but
  this time the clients connect to each other via STUNner, without the assistance of a media
  server.  The demo contains a [Node.js](https://nodejs.org) application server for creating a
  browser-based two-party WebRTC video-call, plus a STUNner service that clients use as a TURN
  server to connect to each other.  Note that no transcoding/transsizing option is available in
  this demo, since there is no media server in the media pipeline.
* [Media-plane mode: One to one video call with Kurento via
  STUNner](examples/kurento-one2one-call): This tutorial extends the previous demo to showcase the
  fully fledged *media-plane deployment model* of STUNner, that is, when WebRTC clients connect to
  each other via a media server deployed into Kubernetes, this time provided by
  [Kurento](https://www.kurento.org). The media servers in turn are exposed to the clients via a
  STUNner gateway.  The demo has been adopted from the [Kurento](https://www.kurento.org)
  [one-to-one video call
  tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html),
  with minimal
  [modifications](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call)
  to deploy it into Kubernetes and integrate it with STUNner. The demo contains a
  [Node.js](https://nodejs.org) application server for creating a browser-based two-party WebRTC
  video-call, plus the Kurento media server deployed behind STUNner for media exchange and,
  potentially, automatic audio/video transcoding.
* [Media-plane mode: Magic mirror via STUNner](examples/kurento-magic-mirror/README.md): This
  example has been adopted from the [Kurento](https://www.kurento.org) [magic
  mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
  demo. The demo shows a basic WebRTC loopback server with some media processing added: the
  application uses computer vision and augmented reality techniques to add a funny hat on top of
  faces. The computer vision functionality is again provided by the [Kurento media
  server](https://www.kurento.org), being exposed to the clients via a STUNner gateway.

## Documentation

See further documentation [here](doc/README.md).

## Caveats

STUNner is a work-in-progress. Some features are missing, others may not work as expected. The
notable limitations at this point are as follows.

* *STUNner is not intended to be used as a public STUN/TURN server*; the intended use is as a
  Kubernetes ingress gateway for WebRTC. (For implementing a public TURN service, see
  [alternatives](https://github.com/coturn/coturn)).  Being deployed into a Kubernetes service,
  STUNner will not be able to identify the public IP address of a client sending a STUN binding
  request to it (without special
  [hacks](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip)),
  and the TURN transport relay connection opened by a WebRTC client via STUNner is reachable only
  to clients that likewise open a TURN transport via STUNner (again, without further
  [hacks](https://kubernetes.io/docs/concepts/security/pod-security-policy/#host-namespaces)). This
  is intended: STUNner is a Kubernetes ingress gateway which happens to expose a STUN/TURN
  compatible service to WebRTC clients, and not a public TURN service.
* Access through STUNner to the rest of the cluster *must* be locked down with a Kubernetes
  `NetworkPolicy`. Otherwise, certain internal Kubernetes services would become available
  externally; see the [notes on access control](/doc/SECURITY.md#access-control).
* STUNner supports arbitrary scale-up without dropping active calls, but *scale-down might
  disconnect calls* established through the STUNner pods and/or media server replicas being removed
  from the load-balancing pool. Note that this problem is
  [universal](https://webrtchacks.com/webrtc-media-servers-in-the-cloud) in WebRTC, but we plan to
  do something about it in a later STUNner release so stay tuned.
* The WebRTC DataChannel API is not supported at the moment.

## Milestones

* v0.9.2: Day-2 operations: STUNner basic UDP/TURN connectivity + helm chart + simple use cases (Kurento
  demos).
* v0.9.3: Onboarding: long-term STUN/TURN credentials and [STUN/TURN over
  TCP/TLS/DTLS](https://www.rfc-editor.org/rfc/rfc6062.txt).
* v0.9.4: Day-2 operations: STUNner Kubernetes operator.
* v0.9.5: Performance: eBPF STUN/TURN acceleration.
* v0.9.6: Observability: Prometheus + Grafana dashboard.
* v0.9.7: Ubiquity: make STUNner work with Jitsi, Janus, mediasoup and pion-SFU.
* v1.0: GA
* v2.0: Service mesh: adaptive scaling & resiliency

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
