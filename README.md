# STUNner: A Kubernetes ingress gateway for WebRTC

**WORK IN PROGRESS**

STUNner is a cloud-native STUN/TURN server designed for hosting scalable WebRTC services over
Kubernetes.

Ever wondered how you are going to [deploy your WebRTC infrastructure into the
cloud](https://webrtchacks.com/webrtc-media-servers-in-the-cloud)? Frightened away by the
complexities of Kubernetes container networking, and the surprising ways in which it may interact
with your UDP/RTP media? Tried to read through the endless stream of [Stack
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
makes streaming workloads, and especially UDP/RTP based WebRTC media, feel like a foreign citizen.
STUNner aims to change this state-of-the-art, by exposing a single public STUN/TURN server port for
ingesting *all* media traffic into a Kubernetes cluster in a controlled and standards-compliant
way.

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

## Getting Started

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server. STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes and make sure your media server is still
reachable for WebRTC clients despite running with a private IP address inside a Kubernetes pod.

The below installation instructions require you to have an operational Kubernetes cluster running a
supported version of Kubernetes (>1.20). You can use any supported platform, for example
[Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) or any hosted or private
service, but make sure that the cluster has an operational [load-balancer
integration](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) running
(all major hosted Kubernetes services should support this, and even Minikube
[provides](https://minikube.sigs.k8s.io/docs/handbook/accessing) standard `LoadBalancer` service
access), otherwise STUNner will not be able to allocate a public IP address for clients to reach
your WebRTC infra.

### Configuration

STUNner will need to have the below Kubernetes resources configured and deployed in order to run:
1. a `ConfigMap` that stores STUNner local configuration,
2. a `Deployment` running one or more STUNner replicas,
3. a `LoadBalancer` service to expose the STUNner deployment on a public IP address and UDP port
   (by default, the port is UDP 3478), and finally
4. an ACL/firewall policy to control network communication from STUNner to the rest of the cluster.

The installation scripts packaged will STUNner will use hard-coded configuration defaults. Make
sure to customize the defaults before deploying STUNner; in particular, make absolutely sure to
customize the access tokens (`STUNNER_REALM`, `STUNNER_USERNAME` and `STUNNER_PASSWORD`), otherwise
STUNner will use hard-coded STUN/TURN credentials. This should not pose a major security risk (see
the [security notes](#security) below), but it is still safer to customize the access tokens before
exposing STUNner to the Internet.

By default, all resources are created in the `default` namespace.

### Install STUNner

The simplest way to deploy STUNner is through Helm.

TODO

If Helm is not an option, you can use the static Kubernetes manifests packaged with STUNner. This
mode is not recommended for general use however, since the static Kubernetes manifests do not
provide the same flexibility and configurability as the Helm charts.

First, download the STUNner repository.

```console
$ git clone https://github.com/l7mp/stunner.git
$ cd stunner
```

Then, deploy the STUNner service [manifest](kubernetes/stunner.yaml).
```console
$ kubectl apply -f kubernetes/stunner.yaml
```

### Learning the external IP and port

STUNner exposes the STUN/TURN server through a standard Kubernetes `LoadBalancer` service. Since
Kubernetes assigns an [ephemeral public
IP](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) address to
`LoadBalancer` services, the STUN/TURN server's public IP address and port may change from
deployment to deployment. (Of course, Kubernetes lets you use your own [fix
IP/DNS](https://kubernetes.io/docs/concepts/services-networking/service/#choosing-your-own-ip-address),
but the default installation scripts do not support this.)  WebRTC clients will need to learn the
external IP and port somehow; this is outside the scope of STUNner, but see our [Kurento
demo](#demo) for a way to communicate the STUN/TURN address and port back to WebRTC clients during
client registration.

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

If this hangs for minutes, then your load-balancer integration is not working. Otherwise, query the
public IP address and port from Kubernetes.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get service stunner -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PORT=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_PORT}')
```

From this point, your STUNner service is exposed to your clients on the IP address
`$STUNNER_PUBLIC_ADDR` and UDP port `$STUNNER_PUBLIC_ADDR`. In order to have all STUNner
configuration available in the Kubernetes cluster, it is worth storing the public IP address into
STUNner's `ConfigMap`:

```console
$ kubectl patch configmap/stunner-config -n default --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\"}}"
```

Here is a simple way to direct your webRTC clients to reach STUNner; make sure to substitute the
placeholders below (like `<STUNNER_PUBLIC_ADDR`) with the correct configuration.

```javascript
var ICE_config= {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PORT>?transport=udp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ]
}
var pc = new RTCPeerConnection(ICE_config);
```

### Testing

STUNner comes with a simple STUN/TURN client called [`turncat`](utils/turncat) that can be used to
test whether installation is successful. The `turncat` client will open a UDP tunnel through
STUNner into your Kubernetes cluster that can be used to access any UDP service running inside the
cluster; for more info, see the [documentation](utils/turncat). For simplicity, we shall use
`turncat` to reach the Kubernetes DNS service.

First, we store the STUN/TURN credentials for later use.

```console
$ export STUNNER_REALM=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_REALM}')
$ export STUNNER_USERNAME=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_USERNAME}')
$ export STUNNER_PASSWORD=$(kubectl get cm stunner-config -n default -o jsonpath='{.data.STUNNER_PASSWORD}')
$ export KUBE_DNS_IP=$(kubectl get svc kube-dns -n kube-system -o jsonpath='{.spec.clusterIP}')
```
Then fire up `turncat` locally; this will open a UDP server port on `localhost:5000` and forward
all received packets to the `kube-dns` service.

```console
$ go run main.go --realm $STUNNER_REALM --user ${STUNNER_USERNAME}=${STUNNER_PASSWORD} \
  --log=all:TRACE udp:127.0.0.1:5000 turn:${STUNNER_PUBLIC_ADDR}:${STUNNER_PORT} udp:${KUBE_DNS_IP}:53
```

Now, in another terminal try to query the Kubernetes DNS service for the internal IP address
allocated for STUNner:

```console
$ dig +short  @127.0.0.1 -p 5000  stunner.default.svc.cluster.local
```

If all goes well, this should hang until `dig` times out. The reason is that, by default, *all*
communication from STUNner is prohibited to minimize the exposure of your sensitive internal
services to the external world. For testing purposes, we can temporarily open up the Kubernetes ACL
to allow access from STUNner to the Kube DNS service as follows.

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels: {}
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - protocol: UDP
      port: 53
EOF
```

Repeating the above DNS query should now return the `ClusterIP` assigned to the `stunner` service:

```console
$ dig +short  @127.0.0.1 -p 5000  stunner.default.svc.cluster.local
10.120.4.153
```

After testing, stop `turncat` and revert the default-deny ACL (but see the below [security
notice](#access-control) on Access control).

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
EOF
```

## Demo

STUNner comes with a complete demo to show how to use it to deploy an WebRTC application into
Kubernetes. The demo has been adopted from the [Kurento](https://www.kurento.org/) one to one
[video call
tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), with
minimal
[modifications](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call) to
deploy it into Kubernetes and integrate it with STUNner. The demo contains a
[Node.js](https://nodejs.org) application server for creating browser-based two-party WebRTC
video-calls, plus the Kurento media server deployed behind STUNner for media exchange and,
potentially, automatic audio/video transcoding.

### Installation

The simplest way to deploy the demo is to use the [manifest](kubernetes/kurento-webrtc.yaml)
packaged with STUNner. Note: you need to have the STUNner git repository clones locally for this to
work, and STUNner must be running with the default configuration.

```console
$ kubectl apply -f kubernetes/kurento-webrtc.yaml
```

The demo exposes a publicly available HTTPS web service on port 8443. Again, Kubernetes assigns an
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

### Understanding what's happening

The [Kurento
docs](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html) cover all
the WebRTC related information, below we just summarize the changes we have made to the Kurento
code to make it work with STUNner.

1. Store the TLS certificate in a Kubernetes `Secret` (called `webrtc-server-secret`) and map the
   keys into the file system of the application server pod. This allows easy customization of the
   TLS certificates used with the demo.
2. Deploy the Kurento media server into a `Deployment` (called `kms`). Observe that the Kurento has
   been configured with *no STUN and TURN servers* and *no external IP address* and runs in an
   ordinary Kubernetes pod at an ephemeral private IP address that is not available from browsers
   fromdirectly! Here is where the *magic* happens: STUNner makes sure that WebRTC media just keeps
   flowing between clients and the media server, without *any* of the two being accessible via a
   public IP.
3. Expose the media server pool, i.e., the Kurento media server deployment, over the Kubernetes
   service `kms-control` over the TCP port 8888. This assigns a virtual IP address (a `ClusterIP`)
   to the media server pool for the application server to configure the WebRTC endpoints.
4. [Modify](https://github.com/l7mp/kurento-tutorial-node/commit/54289c2c6592d9237f3b465a0e10fa425b8ade8b#diff-70406ec94adfebba544257cb82e2aed222a8941c8b8db766aee488272446f1acR26)
   the WebRTC application server (1) to
   [store](https://github.com/l7mp/kurento-tutorial-node/blob/54289c2c6592d9237f3b465a0e10fa425b8ade8b/kurento-one2one-call/server.js#L26)
   the STUNner configuration parameters (available in the environment variables
   `STUNNER_PUBLIC_ADDR`, `STUNNER_PORT`, `STUNNER_USERNAME`, and `STUNNER_PASSWORD`, see below) in
   an appropriate WebRTC [ICE server
   configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer) and (2)
   [return](https://github.com/l7mp/kurento-tutorial-node/blob/54289c2c6592d9237f3b465a0e10fa425b8ade8b/kurento-one2one-call/server.js#L442)
   the ICE configuration to the WebRTC clients in the `registerResponse` messages indicating a
   successful user registration.
5. [Modify](https://github.com/l7mp/kurento-tutorial-node/commit/54289c2c6592d9237f3b465a0e10fa425b8ade8b#diff-70406ec94adfebba544257cb82e2aed222a8941c8b8db766aee488272446f1acR26)
   the [JavaScript
   code](https://github.com/l7mp/kurento-tutorial-node/blob/master/kurento-one2one-call/static/js/index.js)
   served to clients to (1)
   [store](https://github.com/l7mp/kurento-tutorial-node/blob/54289c2c6592d9237f3b465a0e10fa425b8ade8b/kurento-one2one-call/static/js/index.js#L134)
   the ICE server configuration returned from the application server, and (2) set up the WebRTC
   PeerConnections at both the [caller](https://github.com/l7mp/kurento-tutorial-node/blob/54289c2c6592d9237f3b465a0e10fa425b8ade8b/kurento-one2one-call/static/js/index.js#L255) and the
   [callee](https://github.com/l7mp/kurento-tutorial-node/blob/54289c2c6592d9237f3b465a0e10fa425b8ade8b/kurento-one2one-call/static/js/index.js#L186).
6. Use the
   [Dockerfile](https://github.com/l7mp/kurento-tutorial-node/blob/master/kurento-one2one-call/Dockerfile)
   packaged with STUNner to build the modified WebRTC application server container image or use the
   [prebuilt image](https://hub.docker.com/repository/docker/l7mp/kurento-webrtc-server).
7. Deploy the modified WebRTC application server into a `Deployment` (called `webrtc-server`). Note
   that the STUNner configuration is made available to the application server in environment
   variables and the TLS keys are taken from the Kubernetes `Secret` called `webrtc-server-secret`
   configured above.
8. Expose the application server on a Kubernetes `LoadBalancer` service so that external clients
   can reach it via the TCP port 8443.
9. And finally, as a critical step, make sure that STUNner is permitted to reach the media servers
   to forward UDP/RTP media packets to it. Recall, by default all internal access from STUNner is
   locked down by a Kubernetes `NetworkPolicy` (called `stunner-network-policy`). The installation
   opens this ACL up so that STUNner can reach al WebRTC endpoints on the Kurento media servers,
   but *just nothing else*. This makes sure that the [trick](#testing) to query the Kubernetes DNS
   service externally will not work; external STUN/TURN clients can reach only the UDP ports
   exposed on the media server and nothing else.

And that's all. We added only 32 lines of code to the Kurento demo to make it work with Kubernetes,
with most of the changes needed to return the ephemeral public IP STUN/TURN URI and credentials to
the clients. If you allocate STUNner to a stable IP or DNS name, you don't even need to modify
*anything* in the demo and it will just work.

## Why hosting WebRTC over Kubernetes?

At the moment, [WebRTC](https://stackoverflow.com/search?q=kubernetes+webrtc)
[lacks](https://stackoverflow.com/questions/61140228/kubernetes-loadbalancer-open-a-wide-range-thousands-of-port)
[a](https://stackoverflow.com/questions/64232853/how-to-use-webrtc-with-rtcpeerconnection-on-kubernetes)
[vitualization](https://stackoverflow.com/questions/68339856/webrtc-on-kubernetes-cluster/68352515#68352515)
[story](https://stackoverflow.com/questions/52929955/akskubernetes-service-with-udp-and-tcp) that
would allow to deploy a WebRTC backend service into the cloud and enjoy the
[resiliency](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce)
[scalability](https://stackoverflow.com/questions/62088089/scaling-down-video-conference-software-in-kubernetes)
and [high
availability](https://blog.cloudflare.com/announcing-our-real-time-communications-platform)
features we have come to expect from modern network services. Worse yet, the entire industry relies
on a handful of [public STUN/TURN servers](https://www.npmjs.com/package/freeice) to connect
clients behind a firewall, which may create a useless dependency on an externally operated
non-profit service, introduce a bottleneck and needlessly increase UDP/RTP delay/jitter by
stretching the round-trip time between clients and media servers through a public TURN server, and
raise certain security concerns related to third-parties accessing all media communications (even
if encrypted).

The main goal of STUNner is to allow *anyone* to deploy their own WebRTC infrastructure into
Kubernetes, without a reliance on any external services other than the cloud-provider's standard
hosted Kubernetes offering, and enjoy the unmatched [agility and
manageability](https://kubernetes.io/docs/concepts/overview/what-is-kubernetes/) Kubernetes
provides to cloud-bound workloads.

Consider scaling the media plane, for a quick demo. If the single STUNner instance started by the
default installation scripts proves a bottleneck for some reason (e.g., due to performance or
availability). In a traditional setting, you need to provision a new STUN/TURN server instance,
allocate a public IP, add the new server to your STUN/TURN server pool, and monitor the liveliness
of the new server. In Kubernetes, however, you can use *a single command to scale STUNner to an
arbitrary number of replicas*, with Kubernetes potentially adding new nodes to the cluster if
needed, and the new replicas will be *automatically* added to the STUN/TURN server pool accessible
behind the (single) public IP address/port pair (`<STUNNER_PUBLIC_ADDR>:<STUNNER_PORT>`), with new
UDP/RTP streams load-balanced between the replicas. For instance, the below will fire up 15 STUNner
replicas.

```console
$ kubectl scale deployment stunner --replicas=15
```

With Kubernetes, the same applies to the media server. For instance, the below scales the media
server in the [Kurento demo](kubectl scale deployment stunner --replicas=20) to 20 instances and
again, automatic health-checks and load-balancing across the new replicas should just work as
expected.

```console
$ kubectl scale deployment kms --replicas=20
```

## Security

Before deploying STUNner, it is well-worth being aware of the potential [security
risks](https://www.rtcsec.com/article/slack-webrtc-turn-compromise-and-bug-bounty) a poorly
configured publicly available STUN/TURN server poses. Unless properly locked down by an ACL,
STUNner may be used hostilely to open a UDP tunnel to *any* UDP service running inside a Kubernetes
cluster (recall our little [trick](#testing) from above).

The below security considerations will greatly reduce the attack surface associated by
STUNner. Overall, **a properly configured STUNner deployment will present exactly the same attack
surface* as a WebRTC infrastructure hosted on a public IP address** (possibly behind a
fiewwall). In any case, use STUNner at your own risk.

### Authentication

For the initial release, STUNner uses a single statically set username/password pair for all
clients and the password is available in plain text at the clients. Anyone with access to the
static STUNner credentials can open a UDP tunnel to any service inside the Kubernetes cluster,
unless [blocked](#access-control) by a properly configured Kubernetes `NetworkPolicy`.

In order to mitigate the risks, it is a good practice to roll the username/password pair every
once in a while.  Suppose you want to set the STUN/TURN username to `my_user` and the password to
`my_pass`. To do this simply modify the STUNner `ConfigMap` and simply restart STUNner to enforce
the new access tokens:

```console
$ kubectl patch configmap/stunner-config -n default --type merge \
    -p "{\"data\":{\"STUNNER_USERNAME\":\"my_user\",\"STUNNER_PASSWORD\":\"my_pass\"}}"
$ kubectl rollout restart deployment/stunner
```

If the WebRTC application server uses [dynamic STUN/TURN credentials](#demo) then it may need to be
restarted as well to learn the new credentials.

### Access control

The ultimate condition for a secure STUNner deployment is a correctly configured access control
rules that restricts external users to open transport relay connections only to the media server
and nothing else. The STUNner installation comes with a default `NetworkPolicy` that locks down
*all* access from STUNner to the rest of the workload (not even Kube DNS is allowed). This is to
enforce the security best practice that the access permissions of STUNner must be carefully
customized before deployment.

It is a good practice to configure the ACL so that STUNner can access only the media server ports
used by WebRTC media connection endpoints, and *nothing else*. Thanks to the versatility of
Kubernetes, this is very
[simple](https://kubernetes.io/docs/concepts/services-networking/network-policies) to do with a
`NetworkPolicy`. For instance, in our [Kurento demo](#demo), STUNner is deployed into the `default`
namespace and it is labeled with `app: stunner`, and the media server runs in the same namaspace
using the label `app: webrtc-server`. In addition, WebRTC endpoints on the Kurento server are
assigned a UDP port between 10000 and 20000. Then, the below `NetworkPolicy` ensures that access
from any STUNner pod to the media server is allowed over any UDP port between 10000 and 20000, and
any other network traffic is denied.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
  namespace: default
spec:
# Choose the STUNner pods
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  # Allow only this rule, everything else is denied
  - to:
    # Choose the media server
    - podSelector:
        matchLabels:
          app: kms
    ports:
    # only ports 10000-20000 are allowed
    - protocol: UDP
      port: 10000
      endPort: 20000
```

WARNING: Some Kubernetes CNIs do not support network policies, or support only a subset of what
STUNner needs. We tested STUNner with [Calico](https://www.tigera.io/project-calico) and the
standard GKE data plane, but your [mileage may vary](https://cilium.io). (Note that the GKE data
plane v2 will not work due a lack of support for the `endPort` field in ACL rules.) In any case,
[test your ACLs](https://banzaicloud.com/blog/network-policy/) before exposing STUNner publicly;
the [`turncat`](utils/turncat) client packaged with STUNner can be used for this
[purpose](#testing).

### Exposing internal IP addresses

The trick in STUNner is that both the TURN relay transport address and the media server address are
internal pod IP addresses, and pods can connect
[directly](https://sookocheff.com/post/kubernetes/understanding-kubernetes-networking-model/#kubernetes-networking-model),
without a NAT. This makes it possible to host the entire WebRTC infrastructure over the private
internal pod network and still allow external clients to make connections to the media servers via
STUNner.  At the same time, this also has the consequence that internal IP addresses will be
exposed to the 

In particular, an attacker can learn the *private* IP address of all STUNner pods and all media
server pods. We believe that this does not cause a major security risk: remember, none of these
private IP addresses can be reached externally. Nevertheless, if worried about such information
exposures then it STUNner may not be the best option.

## Caveats

STUNner is a work-in-progress. Therefore, some features are missing, others are not working as
expected. Some particular limitations at this point are as follows.

* Only username/password authentication is supported. Support for the standard STUN/TURN [long term
credential mechanism](https://datatracker.ietf.org/doc/html/rfc8489#section-9.2) is on the top of
our TODO list, please bear with us for now.
* Access through STUnner to the rest of the cluster *must* be locked down with a Kubernetes
  `NetworkPolicy`. Otherwise, certain internal Kubernetes services would become available
  externally; see our [notes](#security).
* STUNner supports arbitrary scale-up without dropping active calls, but scale-down might
  disconnect calls established through the STUNner and/or media servers being removed from the
  pool. Note that this problem is
  [universal](https://webrtchacks.com/webrtc-media-servers-in-the-cloud) in WebRTC, but we plan to
  do something about it in a later STUNner release so stay tuned.

## Milestones

* v0.1: Basic connectivity: STUNner + helm chart + simple use cases (Kurento demo).
* v0.2: Security: per-session long-term STUN/TURN credentials.
* v0.3: Performance: eBPF acceleration.
* v0.4: Observability: Prometheus + Grafana dashboard.
* v0.5: Ubiquity: make it work with Jitsi, Janus, mediasoup and pion-SFU.
* v1.0: GA
* v2.0: Service mesh: adaptive scaling & resiliency

## Help

STUNner has a lively Discord server, send [us](AUTHORS) an email to get an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](AUTHORS).

MIT License - see [LICENSE](LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
