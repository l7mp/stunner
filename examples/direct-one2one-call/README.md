# Standalone mode: Direct one to one video call via STUNner

This introductory tutorial showcases the *standalone deployment model* of STUNner, that is, when
WebRTC clients connect to each other directly via STUNner, without a media server. The tutorial has
been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), but
this time the clients connect to each other via STUNner, without the assistance of a media server.
The demo contains a [Node.js](https://nodejs.org) application server for creating a browser-based
two-party WebRTC video-call, plus a STUNner service that clients use as a TURN server to connect to
each other.  Note that no transcoding/transsizing option is available in this demo, since there is
no media server in the media pipeline.

![STUNner standalone deployment architecture](../../doc/stunner_standalone_arch.svg)

In this demo you will learn how to:
* integrate a WebRTC application server with STUNner,
* deploy the modified WebRTC application server into a Kubernetes,
* secure a STUNner deployment, and
* scale-up STUNner with load.

For more information, consult the documentation for the STUNner [One to one video
call](/examples/kurento-one2one-call/README.md) documentation.

## Installation

### Prerequisites

You need to have a Kubernetes cluster (>1.22), and the `kubectl` command-line tool must be
configured to communicate with your cluster. If you do not already have a cluster, you can create
one by using [minikube](https://minikube.sigs.k8s.io/docs/start). Furthermore, make sure that
STUNner is deployed into the cluster (see the [STUNner configuration
guide](/README.md#configuration) and the [STUNner installation guide](/README.md#installation)) and
follow the steps in the [STUNner testing guide](/examples/simple-tunnel/README.md) to make sure
that STUNner is fully operational. The below examples assume that STUNner has been installed into a
default namespace with simple plain text authentication. The demo requires a solid understanding of
the basic concepts in [Kubernetes](https://kubernetes.io/docs/home) and
[WebRTC](https://webrtc.org/getting-started/overview).

### Quick installation

Clone the [STUNner git repository](https://github.com/l7mp/stunner) and deploy the
[manifest](/examples/direct-one2one-call) packaged with STUNner.

```console
$ git clone https://github.com/l7mp/stunner
$ cd stunner
$ kubectl apply -f examples/direct-one2one-call/direct-one2one-call.yaml
```

The demo exposes a publicly available HTTPS web service on port 8443. Kubernetes assigns an
ephemeral public IP address to the web service, so first we need to learn the external IP.

```console
$ kubectl get service webrtc-server -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

The result should be a valid IP address in the form `A.B.C.D`. If no IP address is returned, wait a
bit more until Kubernetes successfully assigns the external IP. Then, direct your browser to the
URL `https://<A.B.C.D>:8443` (of course, make sure substitute the previous IP address), accept the
self-signed certificate, register some user name, and you can immediately start to video-chat with
anyone registered at the service. To try it out, open another browser tab, repeat the above
registration steps and enjoy a nice video-call with yourself.

### Porting a WebRTC application to STUNner

The tutorial has been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), after
dropping the parts related to setting up the media pipeline in the Kurento media server (recall,
this demo lacks a media server).  The [Kurento
docs](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html) cover all
the WebRTC related information, below we only summarize the changes we have made to make the demo
usable with STUNner and Kubernetes.  The full source code of the application server can be found
[here](https://github.com/l7mp/kurento-tutorial-node/tree/master/direct-one2one-call).

1. Store the WebRTC application server's TLS certificate in a Kubernetes `Secret` (called
   `webrtc-server-secret`) and map the keys into the file system of the application server
   pod. This allows easy and dynamic customization of the TLS certificates used with the demo.
2. Modify the WebRTC [application server
   logic](https://github.com/l7mp/kurento-tutorial-node/blob/master/direct-one2one-call/server.js)
   to (1) store the STUNner configuration parameters available in the environment variables
   `STUNNER_PUBLIC_ADDR`, `STUNNER_PUBLIC_PORT`, `STUNNER_USERNAME`, and `STUNNER_PASSWORD` (see
   below) in an appropriate WebRTC [ICE server
   configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer), (2) return the
   ICE configuration to the WebRTC clients in the `registerResponse` messages indicating a
   successful user registration, (3) pass on the caller's SDP Offer to the callee in the
   `incomingCall` message, and (4) return the callee's SDP answer to the caller in the
   `callResponse` message.
3. Modify the [JavaScript
   code](https://github.com/l7mp/kurento-tutorial-node/blob/master/direct-one2one-call/static/js/index.js)
   served to clients to (1) store the ICE server configuration returned from the application
   server, (2) set up the WebRTC `PeerConnection` at both the caller and the callee so that the ICE
   handshake will use STUNner's public address and port with the correct STUN/TURN credentials for
   establishing the media connection, and (3) on the callee side take the SDP Offer from the
   `incomingCall` message, generate an Answer and return it to the server in the
   `incomingCallResponse` message.
4. Use the
   [Dockerfile](https://github.com/l7mp/kurento-tutorial-node/blob/master/direct-one2one-call/Dockerfile)
   packaged to build the modified WebRTC application server container image or deploy the [prebuilt
   image](https://hub.docker.com/repository/docker/l7mp/direct-one2one-call-server).
5. Start the modified WebRTC application server in a Kubernetes `Deployment` (called
   `webrtc-server`). Note that the STUNner configuration is made available to the application
   server in environment variables taken from STUNner's default Kubernetes `ConfigMap` and the TLS
   keys are taken from the Kubernetes `Secret` configured above.
6. Expose the application server on a Kubernetes `LoadBalancer` service so that external clients
   can reach it via the TCP port 8443.

And that's all. Note that, unlike in the other demos there is no need to modify the "default-deny"
ACL (i.e., the Kubernetes `NetworkPolicy`) in this case, since STUNner will never reach any
internal service in Kubernetes (see the below [security notice](/#security) on access control).

### Scaling

Suppose that the single STUNner instance fired up by the default installation script is no longer
sufficient; e.g., due to concerns related to performance or availability.  In a "conventional"
privately hosted setup, you would need to provision a new physical STUN/TURN server instance,
allocate a public IP, add the new server to your STUN/TURN server pool, and monitor the liveliness
of the new server continuously. This takes a lot of manual effort and considerable time. In
Kubernetes, however, you can use a single command to *scale STUNner to an arbitrary number of
replicas*. Kubernetes will potentially add new nodes to the cluster if needed, and the new replicas
will be *automatically* added to the STUN/TURN server pool, accessible behind the (single) public IP
address/port pair (`<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>`), with UDP/RTP streams
conveniently being load-balanced across STUNner replicas.

For instance, the below command will fire up 15 STUNner replicas, usually in a matter of seconds.

```console
$ kubectl scale deployment stunner --replicas=15
```
You can even use Kubernetes
[autoscaling](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) to adapt
the size of the STUNner pool to the actual demand.

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Demo adopted from [Kurento](https://www.kurento.org).
