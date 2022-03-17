# STUNner: A Kubernetes ingress gateway for WebRTC

*WORK IN PROGRESS*

STUNner is a cloud-native STUN/TURN server designed for hosting scalable WebRTC services over
Kubernetes.

Ever wondered how you are going to deploy your WebRTC infrastructure into the cloud? Frightened
away by the complexities of Kubernetes container networking, and the surprising ways in which it
may interact with your RTP/UDP media? Tried to read through the endless stream of [Stack
Overflow](https://stackoverflow.com/search?q=kubernetes+webrtc)
[questions](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[asking](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[how](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[to](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp)
[scale](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
WebRTC services with Kubernetes, just to get (mostly) insufficient answers?  Puzzled by the
security implications of the whole WebRTC industry relying on a handful of public STUN/TURN
servers? 

Worry no more! STUNner allows you to run, scale and manage your own STUN/TURN service _inside_ your
Kubernetes cluster, with _full_ browser-compatibility and no modifications to your existing WebRTC
codebase!

## Description

STUNner is a gateway for ingesting WebRTC media traffic into a Kubernetes cluster. This makes it
possible to deploy WebRTC application servers and media servers into ordinary Kubernetes pods,
taking advantage of Kubernetes's excellent tooling to manage, scale, monitor and troubleshoot the
WebRTC infrastructure like any other cloud-bound workload.  STUNner presents a pubic-facing
STUN/TURN endpoint that WebRTC clients can use to open a transport relay connection to a media
server running *inside* the Kubernetes cluster.

Don't worry about the performance implications of processing all your media through a TURN server:
STUNner is written in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool so you don't pay the round-trip time to a far-away public STUN/TURN server, and
STUNner can be easily scaled up if needed, just like any other "normal" Kubernetes service.

## Features

Kubernetes has been designed and optimized for the typical HTTTP/TCP based Web workload, which
makes streaming workloads, and especially RTP/UDP based WebRTC media, feel like a foreign citizen
in the cloud-native world.  STUNner aims to change this state-of-the-art, by exposing a single
public STUN/TURN server port for ingesting *all* media traffic into a Kubernetes cluster in a
controlled and standards-compliant way.

You may wonder why not using a well-known [STUN/TURN server](https://github.com/coturn/coturn)
instead of STUNner. Nothing stops you from doing that; however, STUNner comes with a set of unique
features that allow it to seamlessly fit into the Kubernetes ecosystem.

* **Seamless integration with Kubernetes.** STUNner can be deployed into any Kubernetes cluster,
  even into restricted ones like GKE Autopilot, using a single command. Manage your HTTP/HTTPS
  application servers with your favorite [service mesh](https://istio.io), and STUNner will take
  care of all UDP/RTP based media.
  
* **Expose a WebRTC media server on a single external UDP port.** No more Kubernetes
  [anti-patterns](https://kubernetes.io/docs/concepts/configuration/overview) just to deploy your
  WebRTC media plane into the cloud! Using STUNner a typical WebRTC deployment needs only two
  public-facing ports, one HTTPS port for the application server and a *single* UDP port one for
  *all* your media.

* **Easily scale your WebRTC infrastructure.** Tired of manually provisioning your WebRTC media
  servers? Can't get sufficient audio/voice quality because public TURN servers are a bottleneck?
  STUNner can be scaled up with a single `kubectl scale` command and, since STUNner lets you deploy
  your media servers into a standard Kubernetes `Deployment`, the same applies to the entire media
  plane!

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media server;
  with STUNner all media is received through a single ingress port. STUNner stores all STUN/TURN
  credentials and DTLS keys in secure Kubernetes vaults, and uses standard Kubernetes ACLs
  (`NetworkPolicy`) to lock down network access between your application servers and the media
  plane.

* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundred
  lines of fully open-source code. STUNner is extremely lightweight, the typical STUNner container
  is only 2.5 Mbytes.

<!-- * **Dynamic long-term credentials (planned).**  -->

<!-- * **Transparent TURN kernel offloading using Linux/eBPF (planned).** -->

## Getting Started

### Install from Kubernetes Manifests

### Helm Install

## Testing

### Local tests

### Kurento Web Conferencing

## Architecture

## Security

## Milestones

## Help

TODO

## Authors

TODO

## License

MIT License - see [LICENSE](LICENSE) for full text

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
