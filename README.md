# STUNner: A Kubernetes ingress gateway for WebRTC

WORK IN PROGRESS

STUNner is a cloud-native STUN/TURN server designed specifically for hosting scalable WebRTC
services over Kubernetes.

Ever wondered how you are going to deploy your WebRTC infrastructure into the cloud? Frightened
away by the complexities of Kubernetes networking, and the surprising ways in which it may interact
with your RTP/UDP media? Tried to read through the endless stream of [Stack
Overflow](https://stackoverflow.com/search?q=kubernetes+webrtc)
[questions](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[asking](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[how](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[to](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp)
[scale](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
WebRTC services with Kubernetes, just to get (mostly) insufficient answers?  Puzzled by the
security implications of the whole WebRTC industry relying on a handful of public STUN/TURN
servers? Worrying about the security implications of opening up thousands of public-facing UDP/TCP
ports on your media servers for the bad guys?

Fear no more! STUNner allows you to run, scale and manage your own STUN/TURN service _inside_ your
Kubernetes cluster, with _full_ browser-compatibility and no modifications to your existing WebRTC
codebase!

## Description

STUNner is a gateway for ingesting WebRTC media traffic into a Kubernetes cluster. STUNner let you
run your media server pool in ordinary Kubernetes containers/pods, taking advantage of Kubernetes's
excellent tooling to manage, scale, monitor and troubleshoot your WebRTC infrastructure like any
other cloud-bound workload.  Once exposed over a Kubernetes `LoadBalancer` service, STUNner
presents a standard pubic-facing STUN/TURN endpoint for WebRTC clients. Clients can ask STUNner to
open a TURN transport relay connection to a media server that runs _inside_ the Kubernetes cluster,
and everything should just work as expected.

Don't worry about the performance implications of processing your media over a TURN server: STUNner
is written entirely in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool into a singe cluster so you don't pay the round-trip time to a far-away public
STUN/TURN server, and STUNner can be scaled up if needed, just like any other "normal" Kubernetes
service.

## Features

Kubernetes has been designed and optimized for the typical HTTTP/TCP based Web workload, which
makes streaming workloads, and especially RTP/UDP based WebRTC media, feel like a foreign citizen in
the cloud-native world.  STUNner aims to change this state-of-the-art. STUNner can be deployed into
any Kubernetes cluster (even restricted ones like GKE Autopilot) using a single command, and it
exposes a single public STUN/TURN server port for ingesting all media traffic into a Kubernetes cluster.

You may wonder why not using a standard [STUN/TURN server](https://github.com/coturn/coturn)
instead of STUNner. Nothing stops you from doing that; however, STUNner comes with a set of unique
features that allow it to seamlessly fit into the Kubernetes ecosystem.

* **Expose a WebRTC media server on a single external UDP port.** No more Kubernetes
  [antipatterns](https://kubernetes.io/docs/concepts/configuration/overview), like
  `hostNetwork`/`hostPort`, just to run your WebRTC media plane! Optimize your //cloud load balancer
  expenses//: with STUNner you need only 2 public facing ports for a typical WebRTC setup, one IP
  public IP address and port HTTPS port for your application server, and another on for //all//
  media. STUNner implements //secure perimeter defense// for your WebRTC cluster: instead of exposing
  thousands of UDP/TCP ports for receiving media traffic, with STUNner all media is received
  through a single ingress gateway.
  
* **Complete browser and WebRTC tooling compatibility.** STUNner exposes a standard STUN/TURN
  server to browsers, so any WebRTC client and server should be able to use it without
  problems. The repository comes with a Kurento based demo that allows you to see STUNner in
  action.

* **Scale your WebRTC like any other cloud-native workload.** Tired of manually provisioning your
  WebRTC media servers? Can't get sufficient audio/voice quality because public TURN servers are a
  bottleneck? STUNner can be scaled up with a single `kubectl scale` command and, since STUNner
  lets you deploy your media servers into a standard Kubernetes `Deployment`, the same applies to
  the media plane.

* **Full Kubernetes and service mesh integration.** Manage your HTTP/HTTPS application servers with
  your favorite service mesh (like [Istio](https://istio.io)), STUNner will take care of the
  UDP/RTP based media. Store your STUN/TURN credentials and DTLS keys in secure Kubernetes vaults!
  Use the standard Kubernetes ACLs (`NetworkPolicy`) to lock down network access between your
  application servers and the media plane, and benefit from the zero-trust networking and
  microsegmentation best practices to secure your WebRTC deployment.
  
* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundreds of
  fully open-source code. STUNner is extremely lightweight, the typical STUNner container is only
  2.5 Mbytes.

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
