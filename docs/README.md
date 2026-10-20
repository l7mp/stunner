# STUNner documentation

<p align="center">
  <img alt="STUNner", src="img/stunner.svg" width="50%" height="50%"></br>
</p>

*Note: This page documents the latest development version of STUNner. See the documentation for the stable version [here](https://docs.l7mp.io/en/stable).*


## Overview

* [Why STUNner](WHY.md)
* [Deployment models](DEPLOYMENT.md)
* [Concepts](CONCEPTS.md)

## Getting started

* [Installation: Free tier](INSTALL.md)
* [Installation: Premium tiers](PREMIUM_INSTALL.md)

## User guides

* [Authentication](AUTH.md)
* [Monitoring](MONITORING.md)
* [Scaling](SCALING.md)
* [Security](SECURITY.md)
* [TURN gateway](TURN.md)
* [SIP gateway (beta)](SIP.md)
* [SRT gateway (beta)](SRT.md)
* [RTSP streaming (beta)](RTSP.md)

## Reference

* [GatewayClass, GatewayConfig and Dataplane](GATEWAYCLASS.md)
* [Gateway](GATEWAY.md)
* [Routes](ROUTE.md)
* [Premium tiers](PREMIUM_REFERENCE.md)

## Tutorials

### Basics

* [Deploying a UDP echo service behind STUNner](examples/udp-echo)
* [Opening a UDP tunnel via STUNner](examples/simple-tunnel)

### Headless deployment model

* [Direct one to one video call via STUNner](examples/direct-one2one-call)

### Media-plane deployment model

* [Video-conferencing with LiveKit](examples/livekit/README.md)
* [Video-conferencing with mediasoup](examples/mediasoup/README.md)
* [Video-conferencing with Janus](examples/janus/README.md)
* [Video-conferencing with Elixir WebRTC](examples/elixir-webrtc/README.md)
* [Video-conferencing with Jitsi](examples/jitsi/README.md)
* [Cloud-gaming with CloudRetro](examples/cloudretro/README.md)
* [Remote desktop access with Neko](examples/neko/README.md)
* [One to one video call with Kurento](examples/kurento-one2one-call)
* [Magic mirror with Kurento](examples/kurento-magic-mirror/README.md)

### SIP deployment models (beta)

* [Calling a SIP echo service in Kubernetes](examples/sip-echo/README.md)

## Manuals

* [`stunnerd` manual](cmd/stunnerd.md)
* [`stunnerctl` manual](cmd/stunnerctl.md)
* [`mediactl` manual](cmd/mediactl.md)
* [Benchmarking](examples/benchmark)
