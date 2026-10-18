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


# STUNner: The Kubernetes real-time media gateway

Ever wondered how to [deploy your real-time communications infrastructure into the
cloud](https://webrtchacks.com/webrtc-media-servers-in-the-cloud)? Frightened away by the
complexities of Kubernetes container networking, and the surprising ways in which it may interact
with your UDP/RTP media? Indeed, Kubernetes was built for HTTP/TCP, while WebRTC and VoIP media is
UDP/RTP that simply does not fit into the cloud-native model. The usual answer is host networking,
thousands of open UDP ports, just to get a per-pod public address.

STUNner is a media gateway that makes real-time media just another Kubernetes workload. It exposes
only a single external port (two for VoIP) to ingests *all* real-time media into the cluster in a
standards-compliant way. This allows STUNner to relay your media service running in ordinary,
unprivileged Kubernetes pods, removing all operational friction that hinders real-time services
today.

STUNner speaks the protocols of both worlds:
* **WebRTC:** a STUN/TURN gateway for browsers and WebRTC clients to reach your media servers
  ([LiveKit](https://livekit.io), [Janus](https://janus.conf.meetecho.com),
  [Jitsi](https://jitsi.org), [mediasoup](https://mediasoup.org), [Elixir
  WebRTC](https://elixir-webrtc.org), and the like), with full browser compatibility and no change
  to your existing WebRTC codebase.
* **SIP and RTP (experimental):** a SIP and RTP gateway for SIP trunks, so carriers can deliver
  phone calls to SIP servers like [LiveKit SIP](https://docs.livekit.io/sip) running in the
  cluster, with no host networking and no public RTP port range.

STUNner implements the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io), so you configure
it with the same YAML manifests as the rest of your workload.

## Table of Contents
1. [Description](#description)
1. [Features](#features)
1. [Getting started](#getting-started)
1. [Usage](#usage)
1. [Documentation](#documentation)

## Description

Real-time media [lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
a [virtualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[story](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp): there
is no easy way to deploy a WebRTC media server or a SIP gateway into Kubernetes and benefit from
the [resiliency](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce),
[scalability](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes),
and [high
availability](https://blog.cloudflare.com/announcing-our-real-time-communications-platform)
features we have come to expect from modern network services. WebRTC deployments end up relying on
[public](https://bloggeek.me/google-free-turn-server/) [STUN servers](https://www.npmjs.com/package/freeice)
and [hosted TURN services](https://bloggeek.me/managed-webrtc-turn-speed) to connect clients behind
a NAT, and SIP deployments end up on dedicated hosts with public addresses and wide exposed RTP port ranges.

The main goal of STUNner is to allow *anyone* to deploy their own real-time media infrastructure
into Kubernetes, without relying on any external service other than the cloud provider's standard
hosted Kubernetes offering. STUNner can act as a standalone STUN/TURN server that WebRTC clients
and media servers use for NAT traversal (headless model), or as a gateway that ingests WebRTC media
into the cluster through a public STUN/TURN server (media-plane model), or as a gateway that
terminates SIP trunks and relays their RTP media to SIP servers in the cluster (SIP model,
experimental). Your application servers, media servers and SIP servers then run in ordinary
Kubernetes pods, managed, scaled, monitored and troubleshooted in exactly the same way as any other
Kubernetes workload.

![STUNner deployment architecture](./docs/img/stunner_arch.svg)

Don't worry about the performance implications of processing all your media through a gateway:
STUNner is fast, it is co-located with your media servers so you don't pay the round-trip time to
a far-away relay, and it scales like any other Kubernetes service.

[STUNner](https://github.com/l7mp/stunner) is open source: the dataplane, the gateway operator, the
authentication service, the Helm chart and the tools are all public on GitHub. The official STUNner
images and the `stunner/stunner` Helm chart are built by [l7mp.io](https://l7mp.io) from the same
code, plus a few premium additions. Without a customer key you get the **free tier**: unlimited
TURN Gateways and connections, an SIP/RTP with at most 5 parallel calls. The **member tier**, aimed
at small and medium-scale operations, unlocks a number of premium features; while the fully
featured **enterprise tier** is optimized for large-scale deployments. See
[here](https://l7mp.io/pricing) for up-to-date info on how to purchase a license.

## Features

Kubernetes has been designed and optimized for the typical HTTP/TCP Web workload, which makes
streaming workloads, and especially UDP/RTP media, feel like a foreign citizen. STUNner aims to
change this, by a single public gateway port for ingesting *all* media traffic into a Kubernetes
cluster in a controlled and standards-compliant way. An additional external port let's STUNner
ingest SIP signaling traffic as well in a similar way, opening the door to deploy SIP/RTP services
into Kubernetes.

* **Seamless integration with Kubernetes.** STUNner can be deployed into any Kubernetes cluster,
  even into restricted ones like GKE Autopilot, using a single command. Manage your HTTP/HTTPS
  application servers with your favorite [service mesh](https://istio.io), and STUNner takes care
  of all UDP/RTP media, WebRTC or SIP. STUNner implements the [Kubernetes Gateway
  API](https://gateway-api.sigs.k8s.io) so you configure it in exactly the same way as
  [the](https://doc.traefik.io/traefik/routing/providers/kubernetes-gateway)
  [rest](https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api)
  [of](https://projectcontour.io/guides/gateway-api)
  [your](https://docs.konghq.com/kubernetes-ingress-controller/latest/guides/using-gateway-api)
  [workload](https://github.com/nginxinc/nginx-kubernetes-gateway) through easy-to-use YAML
  manifests.

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media servers;
  with STUNner *all media is received through a single gateway port*. Get rid of the Kubernetes
  [security monstrosities](https://kubernetes.io/docs/concepts/configuration/overview) like
  privileged pods and `hostNetwork`/`hostPort` services typically recommended as a prerequisite to
  containerizing a WebRTC media plane or a SIP gateway: with STUNner your media servers run in
  ordinary unprivileged Kubernetes pods behind a single gateway Service.

* **No reliance on external services for NAT traversal.** Can't afford a [hosted TURN
  service](https://bloggeek.me/webrtc-turn) for client-side NAT traversal? STUNner runs in the same
  cluster as the rest of your WebRTC infrastructure, and any WebRTC client can connect to it
  directly without the use of *any* external STUN/TURN service whatsoever.

* **SIP trunks into the cluster (experimental).** Connect a carrier's SIP trunk to SIP servers
  running in ordinary pods: STUNner proxies the SIP signaling and relays the RTP media of every
  call, for IP-authenticated trunks as well as for registration-based ones, where STUNner registers
  with the carrier itself. See the [SIP gateway guide](/docs/SIP.md) for details.

* **Scale your media infrastructure.** Tired of manually provisioning your media servers?
  STUNner lets you deploy the entire media infrastructure into ordinary Kubernetes pods, thus
  [scaling the media plane](docs/SCALING.md) is as easy as issuing a `kubectl scale`
  command. Better yet, you can use the built in Kubernetes horizontal autoscaler to *automatically*
  resize your workload based on demand.

* **Minimal client-side configuration.** STUNner comes with a built-in [authentication
  service](https://github.com/l7mp/stunner-auth-service) that can be used to generate time-windowed
  per-user TURN credentials through a [standards
  compliant](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) HTTP [REST
  API](/docs/AUTH.md). Just set the generated [ICE
  configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection#configuration)
  in the [`PeerConnection` JavaScript
  API](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection) and
  your clients will readily start using your Kubernetes-based WebRTC service via STUNner.

* **Carrier-grade performance.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, even STUNner's open-source tier is
  already extremely fast and scalable. But with STUNner's industry-leading [Linux/eBPF TURN
  acceleration engine](/docs/PREMIUM_REFERENCE.md#turn-offload) enabled (available in the
  enterprise tier), you can get up to 10 Gbps TURN/UDP performance per CPU core(!) at
  microsecond-scale latency.

## Getting Started

With a minimal understanding of WebRTC or SIP and Kubernetes, deploying STUNner should take less
than 5 minutes.

* [Customize STUNner and deploy it](/docs/INSTALL.md) into your Kubernetes cluster.
* [Deploy a WebRTC media server](/docs/DEPLOYMENT.md#media-plane-deployment-model) or a [SIP
server](https://docs.livekit.io/sip) in ordinary Kubernetes pod.
* [Set STUNner as the ICE server](/docs/AUTH.md) in your WebRTC clients, or [connect your SIP
trunk](/docs/SIP.md) through a STUNner Gateway.
* ...
* Profit!!

Note that the default installation does not contain an application server, a media server or a SIP
server: STUNner is not a media service in itself, it is merely an *enabler* for you to deploy your
*own* media infrastructure into Kubernetes.

The simplest way to deploy STUNner is through [Helm](https://helm.sh). STUNner configuration
parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files).

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner stunner/stunner --create-namespace --namespace=stunner-system
```

Find out more about the charts in the [STUNner-helm repository](https://github.com/l7mp/stunner-helm).

## Usage

STUNner comes with a wide selection of tutorials and demos that teach you how to deploy all kinds
of WebRTC and SIP services into Kubernetes. The first couple of tutorials present the basic concepts,
especially the use of the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io) to configure
STUNner and the [tunnel mode of `stunnerd`](/docs/cmd/stunnerd.md#tunnel-mode) to test it. Each subsequent demo
showcases a specific WebRTC application, from desktop streaming and video-conferencing to
cloud-gaming, and goes from a clean Kubernetes cluster to a working and usable publicly available
WebRTC service in 5-10 minutes using a purely declarative configuration.

* [Deploying a UDP echo service behind STUNner](/docs/examples/udp-echo/README.md): This
  introductory tutorial shows how to deploy a simple UDP echo service into Kubernetes and expose it
  via STUNner. If you read just one STUNner tutorial, this should be it.
* [Calling a SIP echo service in Kubernetes](/docs/examples/sip-echo/README.md): This tutorial
  connects a softphone through a STUNner SIP Gateway to a SIP echo service in the cluster, the
  smallest end-to-end test of the experimental SIP gateway.
* [Opening a UDP tunnel via STUNner](/docs/examples/simple-tunnel/README.md): This tutorial shows
  how to tunnel an external UDP client via STUNner to a standard iperf server deployed into
  Kubernetes. The demo can be used to benchmark your STUNner installation.
* [Direct one to one video call via STUNner](/docs/examples/direct-one2one-call/README.md): This
  tutorial showcases STUNner acting as a TURN server for two WebRTC clients to establish
  connections between themselves, without the mediation of a media server.
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
* [Cloud-gaming with Cloudretro](/docs/examples/cloudretro/README.md): This tutorial lets you play
  Super Mario or Street Fighter in your browser, courtesy of the amazing
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

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2026 by its authors. Some rights reserved. See [AUTHORS](AUTHORS).

MIT License - see [LICENSE](LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
