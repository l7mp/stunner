# RTSP streaming

> [!WARNING]
> RTSP streaming through STUNner is in beta: the deployment model may change in any release, there
> is no support on any tier, and only RTSP interleaved over TCP is supported. Certain features
> require premium support: see [here](https://l7mp.io/pricing) for limits and pricing. Please test
> and report issues, but in any way, use it at your own risk.

STUNner lets the viewers outside the cluster watch IP cameras through a media server running in
ordinary Kubernetes pods (e.g., [MediaMTX](https://github.com/bluenviron/mediamtx)). This allows
browsers over WebRTC, RTSP clients like VLC or a video management system over TCP, to obtain a feed
video with no host networking and no public camera addresses.

## Table of Contents

1. [Concepts](#concepts)
1. [Watching cameras with MediaMTX](#watching-cameras-with-mediamtx)
1. [Testing](#testing)

## Concepts

IP cameras, NVRs and video management systems speak RTSP: HTTP-like requests on TCP port 554 set up
the session, and the media is RTP, either over UDP or interleaved in the TCP control connection. A
camera is an RTSP server: it either streams via TCP in-band or sends UDP media to the address the
request came from. A media server deployed into the cluster (e.g., MediaMTX or go2rtc, an NVR like
Frigate) pulls camera streams continuously; this is outgoing traffic, which reaches a camera
without STUNner. Viewers then watch the feeds the media server re-serves via STUNner.

![STUNner RTSP viewers model](img/stunner_rtsp.svg)

Viewers may reach the media server through STUNner in two ways:

- *Browsers*: the media server serves the cameras over WebRTC (WHEP in MediaMTX and go2rtc) and
  STUNner acts as a WebRTC/TURN gateway in [media-plane deployment
  model](DEPLOYMENT.md#media-plane-deployment-model). See the [WebRTC gateway](WebRTC.md) tutorial
  for how to configure STUNner as a TURN server.
- *RTSP clients*: a plain STUNner `TCP` listener with a TCPRoute to the RTSP port of the media
  server relays client connections, the RTSP requests and the interleaved media, to a pod of the
  media server.

## Watching cameras with MediaMTX

The below manifests deploy MediaMTX pulling one camera over TCP into the path `cam1` (MediaMTX
takes its settings from `MTX_` environment variables), and a Gateway with a TCP listener on port
554 with a TCPRoute to the RTSP port of MediaMTX. Replace the camera URL with your own.

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

IP cameras are pulled by one MediaMTX pod and served to clients over TCP. The STUNner data plane
can be scaled to any number of pods. To scale MediaMTX deployment behind STUNner, all media servers
have to pull every camera so that viewers can reach every stream over every backend. This is
because the backends of the TCP listener are picked per connection in round robin.  Also note that
a plain listener does not perform authentication (see the [authentication
guide](SECURITY.md#authentication)), so turn on the authentication of MediaMTX before exposing
it. STUNner relays the RTSP requests unchanged.

## Testing

Watch the camera through the Gateway with FFmpeg or VLC, forcing RTSP over TCP:

```console
export RTSP_IP=$(kubectl -n cameras get gateway rtsp-gateway -o jsonpath='{.status.addresses[0].value}')
ffplay -rtsp_transport tcp rtsp://${RTSP_IP}:554/cam1
```
