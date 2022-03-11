# STUNner: A Kubernetes ingress gateway for WebRTC

WORK IN PROGRESS

STUNner is a cloud-native STUN/TURN server designed specifically for hosting scalable WebRTC
services over Kubernetes.

Ever wondered how you are going to deploy your WebRTC infrastructure into the cloud? Frightened
away by the complexities of Kubernetes networking, and the surprising ways in which it may interact
with your RTP/UDP media? Tried to read through the endless stream of [Stack
Overflow](https://stackoverflow.com/search?q=kubernetes+webrtc)
[questions](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[with](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp)
[people](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
[asking](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[how](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
to make Kubernetes and WebRTC friends and not foes, just to get (mostly) inadequate answers?
Puzzled by the security implications of the whole WebRTC industry relying on a handful of public
STUN/TURN servers, just to get basic UDP/RTP media connectivity for clients behind a NAT?  Worrying
about the security implications of opening up thousands of public-facing UDP/TCP ports on your
media servers for the bad guys?

Fear no more! STUNner allows you to run, scale and manage your own STUN/TURN service _inside_ your
Kubernetes cluster, just like every other service you already do, with _full_ browser-compatibility
and no modifications to your existing WebRTC codebase! 

## Description

STUNner is an ingress gateway for ingesting WebRTC media traffic into a Kubernetes cluster. This
allows to run your media server pool in ordinary Kubernetes containers/pods and take advantage of
Kubernetes's excellent tooling to manage, scale, monitor and troubleshoot your WebRTC
infrastructure like any other cloud-bound workload. Once exposed over a Kubernetes `LoadBalancer`
service, STUNner presents a standard pubic-facing STUN/TURN access point that WebRTC clients can
ask for a TURN transport relay connection to the media server and everything just works as
expected.

Don't worry about the performance implications of processing your media over a TURN server: STUNner
is written entirely in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool into a singe cluster so you don't pay the round-trip time to a, possibly far-away
public STUN/TURN server, and STUNner can be scaled up if needed just like any other "normal"
Kubernetes service.

## Features

You may wonder why not using a standard [STUN/TURN server](https://github.com/coturn/coturn)
instead of STUNner. In fact, nothing stops you from doing that; however, STUNner comes with a set
of unique features that allow it to seamlessly fit into the Kubernetes ecosystem.

* **Expose a Kubernetes WebRTC media server on a single external UDP port.** No more Kubernetes
  [antipatterns](https://kubernetes.io/docs/concepts/configuration/overview) just to run your
  WebRTC media plane! Secure perimeter defense for the WebRTC cluster: only 2 public facing ports
  for a typical WebRTC setup.

* **Full Kubernetes integration.** The Web runs on TCP/HTTP and Kubernetes has been designed and
  optimized for the typical Web workload. This makes streaming workloads, and especially RTP/UDP
  based WebRTC media, a foreign citizen in the cloud-native world. The aim of STUNner is to change
  this state-of-the-art. STUNner comes with a full-blown Kubernetes integration, which allows you
  to, among others, deploy it into any Kubernetes cluster (even into restricted ones like GKE
  Autopilot) using a single command or store your secrets in a Kubernetes vault.

* **Complete browser and WebRTC tooling compatibility.** Under thr hood, STUNner is a standard
  STUN/TURN server so any WebRTC clients and servers should be able to use it without
  problems. STUNner comes with a Kurento based demo that allows you to see it in action.

* **Scale your WebRTC like any other cloud-native workload.**

* **Service mesh integration.** Manage your HTTP/HTTPS application servers with your favorite
  service mesh (like [Istio](https://istio.io)), STUNner will take care of the UDP/RTP based
  media.
  
* **Simple code and extremely small size.** Written in pure Go using the fantastic battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundreds of
  fully open-source code. STUNner is extremely lightweight, the typical STUNner container is only
  2.5 Mbytes.

* **Dynamic long-term credentials (planned).** 

* **Transparent TURN kernel offloading using Linux/eBPF (planned).**

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
