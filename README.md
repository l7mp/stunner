# STUNner: A Kubernetes ingress gateway for WebRTC

Ever wondered how to [deploy your WebRTC infrastructure into the
cloud](https://webrtchacks.com/webrtc-media-servers-in-the-cloud)? Frightened away by the
complexities of Kubernetes container networking, and the surprising ways in which it may interact
with your UDP/RTP media? Tried to read through the endless stream of [Stack
Overflow](https://stackoverflow.com/search?q=kubernetes+webrtc)
[questions](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[asking](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[how](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[to](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp)
[scale](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
WebRTC services with Kubernetes, just to get (mostly) insufficient answers?  Want to safely connect
your users behind a NAT, without relying on expensive [third-party TURN
services](https://bloggeek.me/managed-webrtc-turn-speed)?

Worry no more! STUNner allows you to deploy *any* WebRTC service into Kubernetes, smoothly
integrating it into the [cloud-native ecosystem](https://landscape.cncf.io).  STUNner exposes a
standards-compliant STUN/TURN gateway for clients to access your virtualized WebRTC infrastructure
running in Kubernetes, maintaining full browser compatibility and requiring minimal or no
modification to your existing WebRTC codebase.

## Table of Contents
1. [Description](#description)
2. [Features](#features)
3. [Getting started](#getting-started)
4. [Examples](#examples)
5. [Documentation](#documentation)
6. [Caveats](#caveats)
7. [Milestones](#milestones)

## Description

Currently [WebRTC](https://stackoverflow.com/search?q=kubernetes+webrtc)
[lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[a](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[vitualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[story](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp): there
is no easy way to deploy a WebRTC backend service into Kubernetes to benefit from the
[resiliency](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce),
[scalability](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes),
and [high
availability](https://blog.cloudflare.com/announcing-our-real-time-communications-platform)
features we have come to expect from modern network services. Worse yet, the entire industry relies
on a handful of [public](https://bloggeek.me/google-free-turn-server/) [STUN
servers](https://www.npmjs.com/package/freeice) and [hosted TURN
services](https://bloggeek.me/managed-webrtc-turn-speed) to connect clients behind a NAT/firewall,
which may create a useless dependency on externally operated services, introduce a bottleneck,
raise security concerns, and come with a non-trivial price tag.

The main goal of STUNner is to allow *anyone* to deploy their own WebRTC infrastructure into
Kubernetes, without relying on any external service other than the cloud-provider's standard hosted
Kubernetes offering. This is achieved by STUNner acting as a gateway for ingesting WebRTC media
traffic into a Kubernetes cluster, exposing a public-facing STUN/TURN server that WebRTC clients
can use to open a transport relay connection to the media servers running *inside* the Kubernetes
cluster. This makes it possible to deploy WebRTC application servers and media servers into
ordinary Kubernetes pods, taking advantage of Kubernetes's excellent tooling to manage, scale,
monitor and troubleshoot the WebRTC infrastructure like any other cloud-bound workload.

![STUNner architecture](./doc/stunner_arch.svg)

Don't worry about the performance implications of processing all your media through a TURN server:
STUNner is written in [Go](https://go.dev) so it is extremely fast, it is co-located with your
media server pool so you don't pay the round-trip time to a far-away public STUN/TURN server, and
STUNner can be easily scaled up if needed, just like any other "normal" Kubernetes service.

## Features

Kubernetes has been designed and optimized for the typical HTTP/TCP Web workload, which makes
streaming workloads, and especially UDP/RTP based WebRTC media, feel like a foreign citizen.
STUNner aims to change this state-of-the-art, by exposing a single public STUN/TURN server port for
ingesting *all* media traffic into a Kubernetes cluster in a controlled and standards-compliant
way.

* **Seamless integration with Kubernetes.** STUNner can be deployed into any Kubernetes cluster,
  even into restricted ones like GKE Autopilot, using a single command. Manage your HTTP/HTTPS
  application servers with your favorite [service mesh](https://istio.io), and STUNner takes care
  of all UDP/RTP media.

* **Expose a WebRTC media server on a single external UDP port.** Get rid of the Kubernetes
  [hacks](https://kubernetes.io/docs/concepts/configuration/overview) like privileged pods and
  `hostNetwork`/`hostPort` services typically required to containerize your WebRTC media plane.
  Using STUNner a WebRTC deployment needs only two public-facing ports, one HTTPS port for the
  application server and a *single* UDP port for *all* your media.

* **No reliance on external services for NAT traversal.** Can't afford a decent [hosted TURN
  service](https://bloggeek.me/webrtc-turn) for client-side NAT traversal? Can't get good
  audio/video quality because the TURN service poses a bottleneck? STUNner can be deployed into the
  same cluster as the rest of your WebRTC infrastructure, and any WebRTC client can connect to it
  directly, without the use of *any* external STUN/TURN service apart from STUNner itself.

* **Easily scale your WebRTC infrastructure.** Tired of manually provisioning your WebRTC media
  servers?  STUNner lets you deploy the entire WebRTC infrastructure into ordinary Kubernetes pods,
  thus scaling the media plane is as easy as issuing a `kubectl scale` command. STUNner itself can
  be scaled with similar ease, completely separately from the media servers.

* **Secure perimeter defense.** No need to open thousands of UDP/TCP ports on your media server for
  potentially malicious access; with STUNner all media is received through a single ingress port
  that you can tightly monitor and control. STUNner stores all STUN/TURN credentials and DTLS keys
  in secure Kubernetes vaults, and uses standard Kubernetes Access Control Lists (ACLs) to lock
  down network access between your application servers and the media plane.

* **Simple code and extremely small size.** Written in pure Go using the battle-tested
  [pion/webrtc](https://github.com/pion/webrtc) framework, STUNner is just a couple of hundred
  lines of fully open-source code. The server is extremely lightweight: the typical STUNner
  container image size is only about 5 Mbytes.

## Getting Started

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server: STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes and make sure your media servers are still
reachable for WebRTC clients, despite running with a private IP address inside a Kubernetes pod.

The below installation instructions require an operational cluster running a supported version of
Kubernetes (>1.20). You can use any supported platform, for example
[Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) or any
[hosted](https://cloud.google.com/kubernetes-engine) or private Kubernetes cluster, but make sure
that the cluster comes with a functional [load-balancer
integration](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) (all
major hosted Kubernetes services should support this, and even Minikube
[provides](https://minikube.sigs.k8s.io/docs/handbook/accessing) standard `LoadBalancer` service
access). Otherwise, STUNner will not be able to allocate a public IP address for clients to reach
your WebRTC infra. In addition, STUNner relies on Kubernetes ACLs (`NetworkPolicy`) with [port
ranges](https://kubernetes.io/docs/concepts/services-networking/network-policies/#targeting-a-range-of-ports)
to block malicious access; see [details](#access-control) later.

### Configuration

The STUNner installation will create the below Kubernetes resources in the cluster:
1. a `ConfigMap` that stores STUNner local configuration,
2. a `Deployment` running one or more STUNner replicas,
3. a `LoadBalancer` service to expose the STUNner deployment on a public IP address and UDP port
   (by default, the port is UDP 3478), and finally
4. an ACL/firewall policy to control network communication from STUNner to the rest of the cluster.

The installation scripts packaged with STUNner will use hard-coded configuration defaults that must
be customized prior to deployment. In particular, make absolutely sure to customize the access
tokens (`STUNNER_REALM`, `STUNNER_USERNAME` and `STUNNER_PASSWORD`), otherwise STUNner will use
hard-coded STUN/TURN credentials. This should not pose a major security risk (see the [STUNner
Security Guide](doc/SECURITY.md) for more info on security), but it is still safer to customize the
access tokens before exposing STUNner to the Internet.

STUNner configuration is available in the Kubernetes `ConfigMap` named `stunner-config`. The most
important settings are as follows.
* `STUNNER_PUBLIC_ADDR` (no default): The public IP address clients can use to reach STUNner. When
  configuring [WebRTC clients with a TURN server URI](configuring-webrtc-clients-to-reach-stunner),
  this setting specifies the IP address in the TURN server URI.  By default, the public IP address
  will be dynamically assigned during installation.  The STUNner installation scripts take care of
  querying the external IP address from Kubernetes and automatically setting `STUNNER_PUBLIC_ADDR`;
  for manual installation the external IP must be set by hand (see
  [details](#learning-the-external-ip-and-port) below).
* `STUNNER_PUBLIC_PORT` (default: 3478): The public port used by clients to reach STUNner. When
  configuring [WebRTC clients with a TURN server URI](configuring-webrtc-clients-to-reach-stunner),
  this setting specifies the port in the TURN server URI.  Note that the Helm installation scripts
  may overwrite this configuration if the installation falls back to the `NodePort` service (i.e.,
  when STUNner fails to obtain an external IP from the Kubernetes ingress load balancer), see
  [details](#learning-the-external-ip-and-port) below.
* `STUNNER_PORT` (default: 3478): The internal port used by STUNner for communication inside the
  cluster. It is safe to set this to the public port.
* `STUNNER_TRANSPORT_UDP_ENABLE` (default: "1", enabled): Enable UDP TURN transport.
* `STUNNER_TRANSPORT_TCP_ENABLE` (default: "", disabled): Enable TCP TURN transport.
* `STUNNER_REALM` (default: `stunner.l7mp.io`): the
  [`REALM`](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.9) used to guide the user agent
  in authenticating with STUNner.
* `STUNNER_AUTH_TYPE` (default: `plaintext`): the STUN/TURN authentication mode, either `plaintext`
  using the username/password pair `$STUNNER_USERNAME`/`$STUNNER_PASSWORD`, or `longterm`, using
  the [STUN/TURN long-term credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2)
  mechanism with the share secret `$STUNNER_SECRET`.
* `STUNNER_USERNAME` (default: `user`): the
  [username](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.3) attribute clients can use to
  authenticate with STUNner over plain-text authentication. Make sure to customize!
* `STUNNER_PASSWORD` (default: `pass`): the password clients can use to authenticate with STUNner
   over plain-text authentication. Make sure to customize!
* `STUNNER_SHARED_SECRET`: the shared secret used for [STUN/TURN long-term
  credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2).
* `STUNNER_LOGLEVEL` (default: `all:WARN`): the default log level used by the STUNner daemons.
* `STUNNER_MIN_PORT` (default: 10000): smallest relay transport port assigned by STUNner. 
* `STUNNER_MAX_PORT` (default: 20000): highest relay transport port assigned by STUNner. 

The default configurations can be overridden by setting custom command line arguments when
launching the STUNner pods.

### Installation

STUNner supports two installation options: a self-contained and easy-to-use Helm chart and a manaul
installation method using static Kubernetes manifests.

The simplest way to deploy STUNner is through Helm. In this case, all STUNner configuration
parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files).

TODO

If Helm is not an option, you can perform a manual installation using the static Kubernetes
manifests packaged with STUNner. This mode is not recommended for general use however, since the
static Kubernetes manifests do not provide the same flexibility and automatization as the Helm
charts.

First, clone the STUNner repository.

```console
$ git clone https://github.com/l7mp/stunner.git
$ cd stunner
```

Then, customize the default settings in the STUNner service [manifest](examples/stunner.yaml) and
deploy it via `kubectl`.

```console
$ kubectl apply -f kubernetes/stunner.yaml
```
By default, all resources are created in the `default` namespace.

### Learning the external IP and port

There are two ways to expose the STUN/TURN ingress gateway service with STUNner: through a standard
Kubernetes [`LoadBalancer`
service](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) (the
default) and as a [`NodePort`
service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport), used as a
fallback if an ingress load-balancer is not available. In both cases the external IP address and
port that WebRTC clients can use to reach STUNner may be set dynamically by Kubernetes. (Of course,
Kubernetes lets you use your own [fix IP address and domain
name](https://kubernetes.io/docs/concepts/services-networking/service/#choosing-your-own-ip-address),
but the default installation scripts do not support this.) WebRTC clients will need to learn the
external IP and port somehow; this is outside the scope of STUNner, but see our
[examples](#examples) for a way to communicate the STUN/TURN address and port back to WebRTC
clients during user registration.

In order to simplify the integration of STUNner into the WebRTC application server, STUNner stores
the dynamic IP address/port assigned by Kubernetes into the `ConfigMap` named `stunner-config`
under the key `STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC applications can map this
`ConfigMap` as environment variables and communicate the IP address and port back to the clients
(see an [example](configuring-webrtc-clients-to-reach-stunner) below).

The Helm installation scripts take care of setting the IP address and port automatically in the
`ConfigMap`. However, when using the manual installation option the external IP address and port
will need to be handled manually during installation. The below instructions simplify this process.

After a successful installation, you should see something similar to the below:

```console
$ kubectl get all
NAME                           READY   STATUS    RESTARTS   AGE
pod/stunner-XXXXXXXXXX-YYYYY   1/1     Running   0          64s

NAME                 TYPE           CLUSTER-IP     EXTERNAL-IP    PORT(S)          AGE
service/kubernetes   ClusterIP      10.120.0.1     <none>         443/TCP          15d
service/stunner      LoadBalancer   10.120.15.44   A.B.C.D        3478:31351/UDP   64s

NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/stunner   1/1     1            1           65s

NAME                                 DESIRED   CURRENT   READY   AGE
replicaset.apps/stunner-XXXXXXXXXX   1         1         1       64s
```

Note the external IP address allocated by Kubernetes for the `stunner` service (`EXTERNAL-IP`
marked with a placeholder `A.B.C.D` in the above): this will be the public STUN/TURN access point
that your WebRTC clients will need to use in order to access the WebRTC media service through
STUNner.

Wait until Kubernetes assigns a valid external IP to STUNner.

```console
$ until [ -n "$(kubectl get svc -n default stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
```

If this hangs for minutes, then your load-balancer integration is not working (if using
[Minikube](https://github.com/kubernetes/minikube), make sure `minikube tunnel` is
[running](https://minikube.sigs.k8s.io/docs/handbook/accessing)). In this case, skip the next step
and proceed to configure STUNner external reachability using the `NodePort` service. Otherwise,
query the public IP address and port used by STUNner from Kubernetes.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -n default -o jsonpath='{.spec.ports[0].port}')
```

If the Kubernetes cluster fails to assign an external IP address for the `stunner` service, the
service would still be reachable externally via the `NodePort` automatically assigned by
Kubernetes. In this case (but only in this case!), set the IP address and port from the NodePort:

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -n default -o jsonpath='{.spec.ports[0].nodePort}')
```

Check that the external IP address `$STUNNER_PUBLIC_ADDR` is reachable by your WebRTC clients: some
Kubernetes clusters (like GKE Autopilot) are installed with private node IP addresses.

If all goes well, the STUNner service is now exposed on the IP address `$STUNNER_PUBLIC_ADDR` and
UDP port `$STUNNER_PUBLIC_PORT`. Finally, store back the public IP address and port into STUNner's
configuration, so that the WebRTC application server can learn this information in a simple way.

```console
$ kubectl patch configmap/stunner-config -n default --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```

### Configuring WebRTC clients to reach STUNner

The below JavaScript snippet will direct your WebRTC clients to use STUNner; make sure to
substitute the placeholders below (like `<STUNNER_PUBLIC_ADDR>`) with the correct configuration
from the above. For more information, see the [examples](#examples) packaged with STUNner.

```js
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=udp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
  iceTransportPolicy: 'relay',
};
var pc = new RTCPeerConnection(ICE_config);
```

The `iceTransportPolicy` can be optionally set to `relay`, which will make sure that the client
will skip the generation of host and server-reflexive ICE candidates (which will never work with
STUNner anyway) and immediately start by TURN relay candidates. Setting this policy will speed up
call establishment substantially, especially with trickle ICE.

## Examples

STUNner comes with several demos to show how to use it to deploy a WebRTC application into
Kubernetes. 

* [Opening a UDP tunnel via STUNner](examples/simple-tunnel): This introductory demo shows how to
  tunnel an external connection via STUNner to a UDP service deployed into Kubernetes. The demo can
  be used to quickly check a STUNner installation.
* [One to one video call with Kurento via STUNner](examples/kurento-one2one-call): This simple demo has been
  adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
  tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html),
  with minimal
  [modifications](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call)
  to deploy it into Kubernetes and integrate it with STUNner. The demo contains a
  [Node.js](https://nodejs.org) application server for creating a browser-based two-party WebRTC
  video-call, plus the Kurento media server deployed behind STUNner for media exchange and,
  potentially, automatic audio/video transcoding.
* [Direct one to one video call via STUNner](examples/direct-one2one-call): This simple demo has
  been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
  tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), but
  this time the clients connect directly to each other via STUNner, without the assistance of a
  media server.
* [Magic mirror via STUNner](examples/magic-mirror): This simple demo has been adopted from the
  [Kurento](https://www.kurento.org/) [magic
  mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
  demo. The demo shows a basic WebRTC loopback server with some media processing added: the
  application uses computer vision and augmented reality techniques to add a funny hat on top of
  faces.

## Documentation

See further documentation [here](doc/REAMDE.md):

* The [STUNner Security Guide](doc/SECURITY.md) describes the best-practices to minmize the
security risks associated with a misconfigured STUNner gateway.
* The [STUNner Authentication Guide](doc/AUTH.md) describes the different user authentication modes
supported by STUNner.

## Caveats

STUNner is a work-in-progress. Some features are missing, others may not work as expected. The
notable limitations at this point are as follows.

* STUNner *is not intended to be used as a public STUN/TURN server* (there are much better
  [alternatives](https://github.com/coturn/coturn) for this). being deployed into a Kubernetes
  service, it will not be able to identify the public IP address of a client sending a STUN binding
  request to it (without special
  [hacks](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip)),
  and the TURN transport relay connections opened by STUNner will not be reachable externally
  (again, without further
  [hacks](https://kubernetes.io/docs/concepts/security/pod-security-policy/#host-namespaces)). This
  is intended: STUNner is a Kubernetes ingress gateway which happens to expose a STUN/TURN
  compatible service to WebRTC clients, and not a public TURN service.
* Access through STUNner to the rest of the cluster *must* be locked down with a Kubernetes
  `NetworkPolicy`. Otherwise, certain internal Kubernetes services would become available
  externally; see the [notes on access control](/doc/SECURITY.md#access-control).
* STUNner supports arbitrary scale-up without dropping active calls, but *scale-down might
  disconnect calls* established through the STUNner pods and/or media server replicas being removed
  from the load-balancing pool. Note that this problem is
  [universal](https://webrtchacks.com/webrtc-media-servers-in-the-cloud) in WebRTC, but we plan to
  do something about it in a later STUNner release so stay tuned.
* The WebRTC DataChannel API is not supported at the moment.

## Milestones

* v0.9.2: On-boarding: STUNner basic UDP/TURN connectivity + helm chart + simple use cases (Kurento
  demos).
* v0.9.3: Security: long-term STUN/TURN credentials, [STUN/TURN over
  TCP/TLS/DTLS](https://www.rfc-editor.org/rfc/rfc6062.txt).
* v0.9.4: Day-2 operations: STUNner Kubernetes operator.
* v0.9.5: Performance: eBPF STUN/TURN acceleration.
* v0.9.6: Observability: Prometheus + Grafana dashboard.
* v0.9.7: Ubiquity: make STUNner work with Jitsi, Janus, mediasoup and pion-SFU.
* v1.0: GA
* v2.0: Service mesh: adaptive scaling & resiliency

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
