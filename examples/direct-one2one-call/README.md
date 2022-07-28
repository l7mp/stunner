# STUNner tutorial: Headless deployment: Direct one to one video call via STUNner

This tutorial showcases the *headless deployment model* of STUNner, that is, when WebRTC clients
connect to each other directly via STUNner, without going through a media server. 

In this demo you will learn how to:
* integrate a typical WebRTC application server with STUNner,
* deploy the modified application server into a Kubernetes,
* test STUNner from a browser with live video traffic,
* scale a WebRTC service with STUNner.

## Installation

### Prerequisites

The tutorial assumes a fresh STUNner installation; see the [STUNner installation and configuration
guide](/doc/INSTALL.md). Create a namespace called `stunner` if there is none. You need a
WebRTC-compatible browser to run this tutorial. Basically any modern browser will do; we usually
test our WebRTC applications with Firefox.

### Setup

The tutorial has been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), but
we have dropped the media server so that clients will connect to each other directly via
STUNner. We will deploy a [Node.js](https://nodejs.org) application server into Kubernetes: this
will serve the main HTML page with the embedded video viewports and download the client-side
JavaScript code to the browsers.

![STUNner standalone deployment architecture](../../doc/stunner_standalone_arch.svg)

Note that transcoding/transsizing and other media server goodies are not available in this setup:
the clients must be fully compatible to be able to establish an audio/video session in this
case. Consult the [one to one video call with Kurento](examples/kurento-one2one-call) tutorial to
learn how to set up a fully-fledged media server behind STUNner.

### Application server

The application server implements a simple JSON/WebSocket API two browser clients can call to
establish a two-party call.  The caller and the callee will connect to each other via STUNner as
the TURN server, without the mediation of a media server.

As the first step, each client registers a unique username with the application server by sending a
`register` message, which the server acknowledges in a `registerResponse` message. To start a call,
the caller sets up a [WebRTC
PeerConnection](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection), generates an
SDP Offer, and sends it along to the application server in a `call` message. The application server
forwards the SDP Offer to the callee in an `incomingCall` message. If accepting the call, the
callee sets up a WebRTC PeerConnection, generates an SDP Answer, and returns it in an
`incomingCallResponse` message to the application server. The SDP Answer is then forwarded back to
the caller in an `incomingCallResponse` message. Meanwhile, the caller and the callee exchange ICE
candidates in the background. Once the ICE process connects, the caller and the callee start to
exchange audio/video frames via STUNner until one of the parties sends a `stop` message to the
application server.

In order start the ICE conversation using STUNner as the STUN/TURN server, the browsers will need
to learn an ICE server configuration from the application server with STUNner's external IP
addresses/ports and the required STUN/TURN credentials. This must happen *before* the PeerConnection
is created: once the PeerConnection is running we can no longer change ICE configuration. 

We solve this problem by (1) generating a new ICE configuration every time a new client registers
with the application server and (2) sending the ICE configuration back to the client in the
`regiterResponse` message. Note that this choice is suboptimal for time-locked STUNner
authentication modes (i.e., the `longterm` mode), because the client's STUN/TURN credentials might
expire by the time they decide to connect. It is up to the application server developer to make
sure that clients' ICE server configuration is periodically updated.

We will use [`stunner-auth-lib`](https://www.npmjs.com/package/@l7mp/stunner-auth-lib), a small
Node.js helper library that simplifies generating ICE configurations and STUNner credentials in the
application server. The library will automatically parse the running STUNner configuration from the
cluster and generate STUN/TURN credentials and ICE server configuration. In addition, it includes a
file watcher that will reread the configuration as it changes and the new settings will immediately
take effect: client requests will always receive an up-to-date ICE configuration.

The full application server code can be found
[here](https://github.com/l7mp/kurento-tutorial-node/tree/master/direct-one2one-call), below we
summarize the most important steps needed to integrate `stunner-auth-lib` with the application
server.

1. Map the STUNner configuration, which by default exists in the `stunner` namespace under the name
   `stunnerd-config`, into the filesystem of the application server pod. This makes it possible for the
   library to pick up the most recent running config.
   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   [...]
   spec:
     [...]
     template:
       spec:
         containers:
         - name: webrtc-server
           image: l7mp/direct-one2one-call-server
           command: ["npm"]
           args: ["start", "--", "--as_uri=https://0.0.0.0:8443"]
           # TURN server config to return to the user
           env:
             - name: STUNNER_CONFIG_FILENAME
               value: "/etc/stunnerd/stunnerd.conf"
           volumeMounts:
             - name: stunnerd-config-volume
               mountPath: /etc/stunnerd
               readOnly: true
             [...]
         volumes:
           - name: stunnerd-config-volume
             configMap:
               name: stunnerd-config
               optional: true
   [...]
   ```

1. Include the library at the top of the server-side JavaScript (this can be found in
   `direct-one2one-call/server.js`).
   ```js
   const auth = require('@l7mp/stunner-auth-lib');
   ```

1. Call the library's `getIceConfig()` method every time you want a new valid ICE configuration. In
   our case, this happens before returning a `registerResponse` to the client, so that the
   generated ICE configuration can be piggy-backed on the response message.
   ```js
   function register(id, name, ws, callback) {
    [...]
     try {
       let iceConfiguration = auth.getIceConfig();
       ws.send(JSON.stringify({id: 'registerResponse', response: 'accepted', iceConfiguration: iceConfiguration}));
     } catch(exception) {
       onError(exception);
     }
   }
   ```

1. Modify the client-side JavaScript (this can be found in
   `direct-one2one-call/static/js/index.js`) to parse the ICE configuration received from the
   application server from the `registerResponse` message.
   ```js
   var iceConfiguration;

   function resgisterResponse(message) {
     if (message.response == 'accepted') {
       iceConfiguration = message.iceConfiguration;
     }
     [...]
   }
   ```

1. Every time the client calls the [PeerConnection
   constructor](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection),
   pass in the stored [ICE
   configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer).
   ```js
   var options = {
     [...]
     configuration: iceConfiguration,
   }
                   
   var peerconnection = PeerConnection(options);
   ```

You can build the application server container locally from the tutorial
[repo](https://github.com/l7mp/kurento-tutorial-node/tree/master/direct-one2one-call), or you can
use the below manifest to fire up the prebuilt container image in a single step. This will deploy
the application server into the `stunner` namespace and exposes it in the Kubernetes LoadBalancer
service called `webrtc-server`.
```console
kubectl apply -f examples/direct-one2one-call/direct-one2one-call-server.yaml
```

Note: due to a limitation of `stunner-auth-lib` currently the application server must run in the
main STUNner namespace (usually called `stunner`), otherwise it will not be able to read the
running config. This limitation will be removed in a later release. If in doubt, deploy everything
into the default namespace and you should be good to go.

### STUNner configuration

Next, we deploy STUNner into the Kubernetes. The manifest below will set up a minimal STUNner
gateway hierarchy to do just that: the setup includes two Gateway listeners, one at UDP:3478 and
another one at TCP:3478, plus and a UDPRoute.
```console
kubectl apply -f examples/simple-tunnel/iperf-stunner.yaml
```

In order to realize the headless deployment model with STUNner, we set STUNner's own service as the
backend in the UDPRoute. This way, STUNner will loop back client connections to itself. The rest,
that is, cross-connecting the clients' media streams between each other, is just pure TURN magic.

Here is the corresponding UDPRoute.
```yaml 
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: stunner-headless
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
    - name: tcp-gateway
  rules:
    - backendRefs:
        - name: stunner
          namespace: stunner
```

Note that the `stunner/stunner` service should exist for this to work. The manifest conveniently
creates it, but if you're doing things manually here is how to create the target service.

```console
kubectl expose deployment -n stunner stunner --port 3478 --protocol UDP
```

### Check your configuration

Check whether you have all the necessary objects installed into the `stunner` namespace.
```console 
kubectl get gatewayconfigs,gateways,udproutes -n stunner
NAME                                                  REALM             AUTH        AGE
gatewayconfig.stunner.l7mp.io/stunner-gatewayconfig   stunner.l7mp.io   plaintext   4s

NAME                                            CLASS                  ADDRESS   READY   AGE
gateway.gateway.networking.k8s.io/tcp-gateway   stunner-gatewayclass             True    4s
gateway.gateway.networking.k8s.io/udp-gateway   stunner-gatewayclass             True    4s

NAME                                                  AGE
udproute.gateway.networking.k8s.io/stunner-headless   3s
```

You can also use the handy `stunnerctl` CLI tool to dump the running STUNner configuration.

``` console
cmd/stunnerctl/stunnerctl running-config stunner/stunnerd-config
STUN/TURN authentication type:  plaintext
STUN/TURN username:             user-1
STUN/TURN password:             pass-1
Listener 1
        Name:   udp-listener
        Listener:       udp-listener
        Protocol:       UDP
        Public address: 34.118.82.225
        Public port:    3478
Listener 2
        Name:   tcp-listener
        Listener:       tcp-listener
        Protocol:       TCP
        Public address: 34.118.89.139
        Public port:    3478
```

### Run the test

At this point, everything should be set up to make a video-call from your browser via
STUNner. Learn the external IP address Kubernetes assigned to the LoadBalancer service of the
application server.
``` console
export WEBRTC_SERVER_IP=$(kubectl get service -n stunner webrtc-server -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

Then, open `https://${WEBRTC_SERVER_IP}:8443` in your browser, accept the self-signed TLS certificate,
register a user, repeat this process in an another browser window using a different user name, then
call one user from the other and enjoy a nice video-conference with yourself.

### What is going on here?

The HTML page served by the application server contains a handy console port, which allows to track
the call setup process. We use the logs from one of the clients to demonstrate call establishment
with STUNner.

1. After registering with the application server, the console should show the content of the
   `registerResponse` message. If all goes well, the response should show the ICE configuration
   returned by the application server. The configuration should contain two TURN URIs for the two
   gateways we have created above: one with UDP transport and another one with TCP. In addition,
   the authentication credentials and the public IP addresses and ports should match those in the
   output of `stunnerctl`. This ICE configuration will be passed in to the `PeerConnection`
   constructor during call setup.
   ```js
   {
       "id": "registerResponse",
       "response": "accepted",
       "iceConfiguration": {
           "iceServers": [
               {
                   "url": "turn:34.118.82.225:3478?transport=UDP",
                   "username": "user-1",
                   "credential": "pass-1"
               },
               {
                   "url": "turn:34.118.8.3:3478?transport=TCP",
                   "username": "user-1",
                   "credential": "pass-1"
               }
           ],
           "iceTransportPolicy": "relay"
       }
   }
   ```

1. Once configured with the above ICE server configuration, the browser will ask STUNner to open a
   TURN transport relay connection for the sending/receiving the video stream and generates a local
   ICE candidate for each relay connection it creates. Note that only TURN-relay candidates are
   generated: host and server-reflexive candidates would not work with STUNner anyway. (This is why
   we set the `iceTransportPolicy` to type `relay` in the ICE server configuration above.) Observe
   further that the ICE candidate contains a private IP address (`10.116.1.21` in the below case)
   as the TURN relay connection address: this just happens to be the IP address of the STUNner pod
   that receives the TURN allocation request from the browser.

   ```console
   Sending message: {[...] "candidate:0 1 UDP 91889663 10.116.1.21 36930 typ relay raddr 10.116.1.21 rport 36930" [...]}
   ```

1. Each locally generated ICE candidate is sent by the browser over to the application server. The
   server in turn passes the received ICE candidates over verbatim to the other client, which
   considers each as a remote ICE candidate.
   ```console
    Received message: { [...] "candidate:0 1 UDP 91889663 10.116.1.21 36930 typ relay raddr 10.116.1.21 rport 36930" [...]}
   ```
1. Once ICE candidates are exchanged, the browsers start to a connectivity check on potential
   candidate pairs. With STUNner this usually succeeds with the first pair. After connecting, video
   starts to flow between the two clients via the UDP/TURN connection opened on STUNner. Note that
   browsers can be behind any type of NAT: STUNner makes sure that whatever aggressive middlebox
   exists between itself and a client media traffic will still be able to flow seamlessly.

### Troubleshooting

Like in any sufficiently complex application, there are lots of moving parts in a Kubernetes-based
WebRTC service and many things can go wrong. Below is a list of steps to help debugging STUNner.

* Cannot reach the application server: Make sure that the LoadBalancer IP is reachable and the TCP
  port 8443 is available from your client.
* No ICE candidate appears: Most probably this happen because the browser's ICE configuration does
  not match the running STUNner config. Check that the ICE configuration returned by the
  application server in the `registerResponse` message matches the output of `stunnerctl
  running-config`.
* No video-connection: This is most probably due to a communication issue between your client and
  STUNner. Try disabling STUNner's UDP Gateway and force the browser to use TCP. 
* Still no connection: follow the excellent [TURN troubleshooting
  guide](https://www.giacomovacca.com/2022/05/troubleshooting-turn.html) to track down the
  issue. Remember: your ultimate friends `tcpdump` and `Wireshark` are always there for you to
  help!

## Scaling

Suppose that the single STUNner instance is no longer sufficient; e.g., due to concerns related to
performance or availability.  In a "conventional" privately hosted setup, you would need to
provision a new physical STUN/TURN server, allocate a public IP, add the new server to the
STUN/TURN server pool, and monitor the liveliness of the new server continuously. This takes a lot
of manual effort and considerable time. 

In Kubernetes, however, you can use a single command to *scale STUNner to an arbitrary number of
replicas*. Kubernetes will potentially add new nodes to the cluster if needed, add the new replicas
*automatically* to STUNNer's STUN/TURN server pool, and make them accessible behind a (single)
public IP address/port pair, automatically load-balancing client media connections across the
active STUNner replicas.

The below command will fire up 15 STUNner replicas; this usually succeeds in a matter of seconds.
```console kubectl scale deployment stunner --replicas=15 ``` You can even use Kubernetes
[autoscaling](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) to adapt
the size of the STUNner pool to the actual demand.

## Clean up

Delete the demo deployment using the below command:

```console
kubectl delete -f examples/direct-one2one-call/direct-one2one-call-server.yaml
kubectl delete -f examples/direct-one2one-call/direct-one2one-call-stunner.yaml
```

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Demo adopted from [Kurento](https://www.kurento.org).
