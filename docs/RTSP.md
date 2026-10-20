# RTSP streaming

> [!WARNING]
> RTSP streaming through STUNner is in beta: the deployment model may change in any release, there
> is no support on any tier, and only RTSP interleaved over TCP is supported. Please test and report
> issues, but in any way, use it at your own risk.

RTSP streaming through STUNner is a beta deployment model: it may change in any release, it comes
with no support on any tier, and it supports RTSP interleaved over TCP only. Within these limits,
STUNner lets the viewers outside the cluster watch IP cameras through a media server running in
ordinary Kubernetes pods (e.g., [MediaMTX](https://github.com/bluenviron/mediamtx)): browsers over
WebRTC, RTSP clients like VLC or a video management system over TCP, with no host networking and no
public camera addresses.

## Table of Contents

1. [Concepts](#concepts)
1. [Watching cameras with MediaMTX](#watching-cameras-with-mediamtx)
1. [Testing](#testing)
1. [Limitations](#limitations)

## Concepts

IP cameras, NVRs and video management systems speak RTSP: HTTP-like requests on TCP port 554 set up
the session, and the media is RTP, either over UDP or interleaved in the TCP control connection. A
camera is an RTSP server: it calls nobody, it answers whoever pulls it, and it sends its UDP media
to the address the request came from. Two consequences shape the deployment:

- Something in the cluster pulls every camera, continuously, one session each: a restreamer like
  MediaMTX or go2rtc, an NVR like Frigate, or an AI pipeline. This is outgoing traffic, which
  reaches a camera without STUNner. Pull over TCP: the camera's UDP media would go to the egress
  address of the cluster, which forwards nothing back to the pod.
- The viewers watch what the media server re-serves, the regular direction of a gateway: requests
  and media come in from outside. This is where STUNner comes in.

![STUNner RTSP viewers model](img/stunner_rtsp.svg)

Viewers reach the media server through STUNner in two ways:

- *Browsers*: the media server serves the cameras over WebRTC (WHEP in MediaMTX and go2rtc), and
  STUNner is its TURN server, as for any WebRTC media server in the [media-plane deployment
  model](DEPLOYMENT.md#media-plane-deployment-model): a [TURN Gateway](TURN.md) with a UDPRoute to
  the media server, and the TURN URI and credentials of the Gateway in the ICE server
  configuration of the media server. The HTTP signaling goes through your usual HTTP ingress.
- *RTSP clients*: a plain `TCP` listener with a TCPRoute to the RTSP port of the media server: STUNner
  relays every client connection, the RTSP requests and the interleaved media together, to a pod
  of the media server.

## Watching cameras with MediaMTX

The below manifests deploy MediaMTX pulling one camera over TCP into the path `cam1` (MediaMTX takes
its settings from `MTX_` environment variables too), and a Gateway with a TCP listener on port 554
with a TCPRoute to the RTSP port of MediaMTX. Replace the camera URL with your own.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mediamtx
  namespace: cameras
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mediamtx
  template:
    metadata:
      labels:
        app: mediamtx
    spec:
      containers:
        - name: mediamtx
          image: bluenviron/mediamtx:latest
          env:
            - name: MTX_PATHS_CAM1_SOURCE
              value: rtsp://user:pass@camera-1.example.com:554/stream1
            - name: MTX_PATHS_CAM1_RTSPTRANSPORT
              value: tcp
          ports:
            - containerPort: 8554
              protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  name: mediamtx
  namespace: cameras
spec:
  selector:
    app: mediamtx
  ports:
    - name: rtsp
      port: 8554
      protocol: TCP
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: rtsp-gateway
  namespace: cameras
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: rtsp
      protocol: TCP
      port: 554
---
apiVersion: stunner.l7mp.io/v1
kind: TCPRoute
metadata:
  name: mediamtx
  namespace: cameras
spec:
  parentRefs:
    - name: rtsp-gateway
  rules:
    - backendRefs:
        - name: mediamtx
          port: 8554
```

Each pull is one session on the camera, so every camera is pulled by one MediaMTX pod: run one
replica, or split the cameras among several MediaMTX Deployments, each behind a Gateway of its own.
A plain listener authenticates nobody (see [plain listeners](SECURITY.md#plain-listeners)): turn on
the authentication of MediaMTX before exposing it, STUNner relays the RTSP requests unchanged.

## Testing

Watch the camera through the Gateway with FFmpeg or VLC, forcing RTSP over TCP:

```console
export RTSP_IP=$(kubectl -n cameras get gateway rtsp-gateway -o jsonpath='{.status.addresses[0].value}')
ffplay -rtsp_transport tcp rtsp://${RTSP_IP}:554/cam1
```

## Limitations

- RTSP interleaved over TCP only: a viewer asking for UDP media gets it from the media server's pod
  address, which it cannot reach.
- STUNner does not pull cameras: the pull leg is the media server's outgoing traffic.
- The backends of a TCP listener are picked per connection, round robin: a viewer reaches every
  stream only if every backend serves every stream.
