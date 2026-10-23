# STUNner documentation

<p align="center">
  <img alt="STUNner", src="img/stunner.svg" width="50%" height="50%"></br>
</p>

*Note: This page documents the latest development version of STUNner. See the documentation for the stable version [here](https://docs.l7mp.io/en/stable).*

> [!WARNING]
> Certain features require premium support: see [here](https://l7mp.io/pricing) for the limits and
> pricing.

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
* [WebRTC gateway](WebRTC.md)
* [SIP/RTP gateway (beta)](SIP.md)
* [SRT gateway (beta)](SRT.md)
* [RTSP streaming (beta)](RTSP.md)

## Reference

* [GatewayClass, GatewayConfig and Dataplane](GATEWAYCLASS.md)
* [Gateway](GATEWAY.md)
* [Routes](ROUTE.md)
* [Premium tiers](PREMIUM_REFERENCE.md)

## Tutorials

### WebRTC gateways

* [Direct one to one video call with headless STUNner](examples/direct-one2one-call)
* [Deploying a UDP echo service behind STUNner](examples/udp-echo)
* [Opening a UDP tunnel via STUNner](examples/simple-tunnel)
* [Video-conferencing with LiveKit](examples/livekit/README.md)
* [Video-conferencing with mediasoup](examples/mediasoup/README.md)
* [Video-conferencing with Janus](examples/janus/README.md)

### SIP/RTP gateways (beta)

* [Calling a SIP echo service in Kubernetes](examples/sip-echo/README.md)
* [Asterisk behind a STUNner SIP Gateway](examples/sip-asterisk/README.md)

### SRT gateways (beta)

* [Streaming SRT through STUNner with mediactl](examples/srt-echo/README.md)
* [Streaming SRT video into MediaMTX](examples/srt-mediamtx/README.md)

## Manuals

* [`stunnerd` manual](cmd/stunnerd.md)
* [`stunnerctl` manual](cmd/stunnerctl.md)
* [`mediactl` manual](cmd/mediactl.md)
