# STUNner: A Kubernetes ingress gateway for WebRTC

**(WORK IN PROGRESS)**

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
WebRTC services with Kubernetes, just to get (mostly) insufficient answers?  Puzzled by the
security and financial implications of the whole WebRTC industry relying on a handful of
third-party STUN/TURN services?

Worry no more! STUNner allows you to deploy _any_ WebRTC service into Kubernetes, smoothly
integrating it into the [cloud-native ecosystem](https://landscape.cncf.io), and enjoy the
convenience of the management, security and observability features provided by a cloud-based
deployment.  STUNner exposes a standards-compliant STUN/TURN server for clients to access your
virtualized WebRTC infrastructure, maintaining full browser compatibility and requiring minimal or
no modification to your existing WebRTC codebase.

## Table of Contents
1. [Description](#description)
2. [Features](#features)
3. [Getting started](#getting-started)
4. [Security](#security)
5. [Caveats](#caveats)
6. [Milestones](#milestones)

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
services](https://bloggeek.me/managed-webrtc-turn-speed) to connect clients behind a firewall,
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
  application servers with your favorite [service mesh](https://istio.io), and STUNner will take
  care of all UDP/RTP based media.

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
  container image size is only about 2.5 Mbytes.

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
be customized before deploying STUNner. In particular, make absolutely sure to customize the access
tokens (`STUNNER_REALM`, `STUNNER_USERNAME` and `STUNNER_PASSWORD`), otherwise STUNner will use
hard-coded STUN/TURN credentials. This should not pose a major security risk (see the [security
notes](#security) below), but it is still safer to customize the access tokens before exposing
STUNner to the Internet.

STUNner configuration is available in the Kubernetes `ConfigMap` named `stunner-config`. The most
important settings are as follows.
* `STUNNER_PUBLIC_ADDR` (no default): The public IP address clients can use to reach the WebRTC
  media servers via STUNner. Clients will need to be configured with a TURN server, with this
  address as the TURN server IP address (see an
  [example](configuring-webrtc-clients-to-reach-stunner) below). By default, the public IP address
  will be dynamically assigned by Kubernetes during installation. The Helm chart takes care of
  querying the external IP address from Kubernetes and automatically configuring it during
  installation; otherwise, the external IP must be set manually (e.g., when using the static
  manifests for installation, see [details](#learning-the-external-ip-and-port) below).
* `STUNNER_PUBLIC_PORT` (default: 3478): The public port used by clients to reach the WebRTC media
  servers via STUNner. Clients will need to be configured with a TURN server, with this port as the
  TURN server port (see an [example](configuring-webrtc-clients-to-reach-stunner) below). Note that
  the Helm installation scripts may overwrite this configuration if the installation falls back to
  the `NodePort` service (i.e., when STUNner fails to obtain an external IP from the Kubernetes
  ingress load balancer), see [details](#learning-the-external-ip-and-port) below.
* `STUNNER_PORT` (default: 3478): The internal port used by STUNner for communication inside the
  cluster. It is safe to set this to the public port.
* `STUNNER_REALM` (default: `stunner.l7mp.io`): the
  [`REALM`](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.9) used to guide the user agent
  in selection of a username and password for the STUN/TURN [long-term
  credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2) mechanism.
* `STUNNER_USERNAME` (default: `user`): the
  [`USERNAME`](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.3) attribute clients can use
  to authenticate with STUNner via the the STUN/TURN [long-term
  credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2) mechanism. Make sure to
  customize!
* `STUNNER_PASSWORD` (default: `pass`): the password clients can use to authenticate with STUNner
  via the the STUN/TURN [long-term
  credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2) mechanism. Make sure to
  customize!
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

Then, customize the default settings in the STUNner service [manifest](kubernetes/stunner.yaml) and
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
external IP and port somehow; this is outside the scope of STUNner, but see our [demos](#demos) for
a way to communicate the STUN/TURN address and port back to WebRTC clients during user
registration.

In order to simplify the integration of STUNner with the rest of the WebRTC services, STUNner
stores the dynamic IP address/port assigned by Kubernetes into the `ConfigMap` named
`stunner-config` under the key `STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC
applications can map this `ConfigMap` as environment variables and communicate the IP address and
port back to the clients (see an [example](configuring-webrtc-clients-to-reach-stunner) below).

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

If this hangs for minutes, then your load-balancer integration is not working (ff using
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
from the above. For more information, see the [demos](#demos).

```js
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=udp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ]
};
var pc = new RTCPeerConnection(ICE_config);
```

### Testing

STUNner comes with a simple STUN/TURN client called [`turncat`](utils/turncat) that can be used to
test a STUNner installation. The `turncat` client will open a UDP tunnel through STUNner into the
Kubernetes cluster, which can be used to access any UDP service running inside the cluster. Note
that your WebRTC clients will not need `turncat` to reach the cluster, since all Web browsers come
with a STUN/TURN client included; `turncat` here is used only to simulate what a WebRTC client
would do when trying to reach STUNner. For more info, see the `turncat`
[documentation](utils/turncat).

We test the STUNner installation by deploying a UDP echo server into the cluster and exposing it
for external access via STUNner.

![STUNner test setup](./doc/stunner_echo.svg)

First, create a `Deployment` called `udp-echo` containing only a single pod and make this pod
available over the UDP port 9001 as a cluster-internal service with the same name. Use everyone's
favorite network debugging tool, [`socat(1)`](https://linux.die.net/man/1/socat), to deploy a
simple UDP echo server into the pod.

```console
$ kubectl create deployment udp-echo --image=l7mp/net-debug:0.5.3
$ kubectl expose deployment udp-echo --name=udp-echo --type=ClusterIP --protocol=UDP --port=9001
$ kubectl exec -it $(kubectl get pod -l app=udp-echo -o jsonpath="{.items[0].metadata.name}") -- \
    socat -d -d udp-l:9001,fork EXEC:cat
```

Store the STUN/TURN configurations and credentials for later use.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_ADDR}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_PORT}')
$ export STUNNER_REALM=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_REALM}')
$ export STUNNER_USERNAME=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_USERNAME}')
$ export STUNNER_PASSWORD=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PASSWORD}')
```

Learn the virtual IP address (`ClusterIP`) assigned by Kubernetes to the `udp-echo` service:

```console
$ export UDP_ECHO_IP=$(kubectl get svc udp-echo -o jsonpath='{.spec.clusterIP}')
```

Observe that the result is a private IP address: indeed, the `udp-echo` service is not available to
external services at this point. We shall use STUNner to expose the service to the Internet via a
TURN service.

The default installation scripts install an ACL into Kubernetes that blocks *all* communication
from STUNner to the rest of the workload. This is to minimize the risk of an improperly configured
STUNner gateway to [expose sensitive services to the external world](#security). In order to allow
STUNner to open transport relay connections to the `udp-echo` service, we have to explicitly open
up this ACL first.

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: udp-echo
    ports:
    - protocol: UDP
      port: 9001
EOF
```

And finally fire up `turncat` locally; this will open a UDP server port on `localhost:9000` and
tunnel all packets to the `udp-echo` service in your Kubernetes cluster through STUNner.

```console
$ cd stunner
$ go run utils/turncat/main.go --realm $STUNNER_REALM --user ${STUNNER_USERNAME}=${STUNNER_PASSWORD} \
  --log=all:TRACE udp:127.0.0.1:9000 turn:${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp:${UDP_ECHO_IP}:9001
```

Now, in another terminal open a UDP connection through the tunnel opened by `turncat` and send
something to the UDP echo server running inside the cluster.

```console
$ echo "Hello STUNner" | socat -d -d - udp:localhost:9000
```

If all goes well, you should see the same text echoed back from the cluster. After the test, make
sure to lock down the ACL to the default-deny rule.

### Demos

STUNner comes with several demos to show how to use it to deploy a WebRTC application into
Kubernetes. 

* [One to one video call with Kurento](examples/kurento-one2one-call): This simple demo has been
  adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
  tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html),
  with minimal
  [modifications](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call)
  that make it possible to deploy it into Kubernetes and integrate it with STUNner. The demo
  contains a [Node.js](https://nodejs.org) application server for creating a browser-based
  two-party WebRTC video-call, plus the Kurento media server deployed behind STUNner for media
  exchange and, potentially, automatic audio/video transcoding.
* TODO

## Security

Like any conventional gateway service, an improperly configured STUNner service may easily end up
exposing sensitive services to the Internet. The below security guidelines will allow to minmize
the risks associated with a misconfigured STUNner.

### Threat model

Before deploying STUNner, it is worth evaluating the potential [security
risks](https://www.rtcsec.com/article/slack-webrtc-turn-compromise-and-bug-bounty) a poorly
configured public STUN/TURN server poses.

To demonstrate the risks, below we shall use `turncat`
to reach the Kubernetes DNS service through a misconfigured STUNner gateway.

As usual, first store the STUNner configuration for later use.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_ADDR}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_PORT}')
$ export STUNNER_REALM=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_REALM}')
$ export STUNNER_USERNAME=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_USERNAME}')
$ export STUNNER_PASSWORD=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PASSWORD}')
```

Next, learn the virtual IP address (`ClusterIP`) assigned by Kubernetes to the cluster DNS service:

```console
$ export KUBE_DNS_IP=$(kubectl get svc -n kube-system -l k8s-app=kube-dns -o jsonpath='{.items[0].spec.clusterIP}')
```

Fire up `turncat` locally; this will open a UDP server port on `localhost:5000` and forward all
received packets to the cluster DNS service through STUNner.

```console
$ cd stunner
$ go run utils/turncat/main.go --realm $STUNNER_REALM --user ${STUNNER_USERNAME}=${STUNNER_PASSWORD} \
  --log=all:TRACE udp:127.0.0.1:5000 turn:${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp:${KUBE_DNS_IP}:53
```

Now, in another terminal try to query the Kubernetes DNS service through the `turncat` tunnel for
the internal service address allocated by Kubernetes for STUNner:

```console
$ dig +short @127.0.0.1 -p 5000 stunner.default.svc.cluster.local
```

If all goes well, this should hang until `dig` times out. Recall, the default installation scripts
block *all* communication from STUNner to the rest of the workload, and the default-deny ACL needs
to be explicitly opened up for STUNner to be able to reach a specific service. To demonstrate the
risk of an improperly configured STUNner gateway, we temporarily allow STUNner to access the Kube
DNS service.

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
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

Repeating the DNS query should now return the `ClusterIP` assigned to the `stunner` service:

```console
$ dig +short  @127.0.0.1 -p 5000  stunner.default.svc.cluster.local
10.120.4.153
```

After testing, make sure to revert the default-deny ACL (but see the below [security
notice](#access-control) on access control).

```console
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
EOF
```

Repeating the DNS query should again time out, as before.

In summary, unless properly locked down, STUNner may be used hostilely to open a UDP tunnel to any
UDP service running inside a Kubernetes cluster. Accordingly, it is critical to tightly control the
pods and services inside a cluster exposed via STUNner, using a properly configured Kubernetes ACL
(`NetworkPolicy`).

The below security considerations will greatly reduce the attack surface associated with
STUNner. Overall, **a properly configured STUNner deployment will present exactly the same attack
surface as a WebRTC infrastructure hosted on a public IP address** (possibly behind a firewall). In
any case, use STUNner at your own risk.

### Authentication

For the initial release, STUNner uses a single statically set username/password pair for all
clients and the password is available in plain text at the clients. Anyone with access to the
static STUNner credentials can open a UDP tunnel to any service inside the Kubernetes cluster,
unless [blocked](#access-control) by a properly configured Kubernetes `NetworkPolicy`.

In order to mitigate the risk, it is a good security practice to reset the username/password pair
every once in a while.  Suppose you want to set the STUN/TURN username to `my_user` and the
password to `my_pass`. To do this simply modify the STUNner `ConfigMap` and restart STUNner to
enforce the new access tokens:

```console
$ kubectl patch configmap/stunner-config -n default --type merge \
    -p "{\"data\":{\"STUNNER_USERNAME\":\"my_user\",\"STUNNER_PASSWORD\":\"my_pass\"}}"
$ kubectl rollout restart deployment/stunner
```

You can even set up a [cron
job](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs) to automate this. Note
that if the WebRTC application server uses [dynamic STUN/TURN credentials](#demo), then it may need
to be restarted as well to learn the new credentials.

### Access control

The ultimate condition for a secure STUNner deployment is a correctly configured access control
regime that restricts external users to open transport relay connections inside the cluster. The
ACL must make sure that only the media servers, and only on a limited set of UDP ports, can be
reached externally.  This can be achieved using an Access Control List, essentially an "internal"
firewall in the cluster, which in Kubernetes is called a `NetworkPolicy`. 

The STUNner installation comes with a default ACL (i.e., `NetworkPolicy`) that locks down *all*
access from STUNner to the rest of the workload (not even Kube DNS is allowed). This is to enforce
the security best practice that the access permissions of STUNner be carefully customized before
deployment.

Here is how to customize this ACL to secure the WebRTC media plane.  Suppose that we want STUNner
to be able to reach *any* media server replica labeled as `app=media-server` over the UDP port
range `[10000:20000]`, but we don't want transport relay connections via STUNner to succeed to
*any* other pod. This will be enough to support WebRTC media, but will not allow clients to, e.g.,
[reach the Kubernetes DNS service](#threat-model). 

The below `NetworkPolicy` will then ensures that all access from any STUNner pod to any media
server pod is allowed over any UDP port between 10000 and 20000, and all other network access from
STUNner is denied.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
spec:
# Choose the STUNner pods as source
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  # Allow only this rule, everything else is denied
  - to:
    # Choose the media server pods as destination
    - podSelector:
        matchLabels:
          app: media-server
    ports:
    # Only UDP ports 10000-20000 are allowed between 
    #   the source-destination pairs
    - protocol: UDP
      port: 10000
      endPort: 20000
```

WARNING: Some Kubernetes CNIs do not support network policies, or support only a subset of what
STUNner needs. We tested STUNner with [Calico](https://www.tigera.io/project-calico) and the
standard GKE data plane, but your [mileage may vary](https://cilium.io).  In particular, only
Kubernetes versions >1.22 support [ACLs with port
ranges](https://kubernetes.io/docs/concepts/services-networking/network-policies/#targeting-a-range-of-ports)
(i.e., the `endPort` field). Furthermore, certain Kubernetes CNIs (like the GKE data plane v2),
even if accepting the `endPort` parameter, will fail to correctly enforce it. For such cases the
below `NetworkPolicy` will allow STUNner to access _all_ UDP ports on the media server; this is
less secure, but still blocks malicious access via STUNner to any service other than the media
servers.

```yaml
$ kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stunner-network-policy
spec:
  podSelector:
    matchLabels:
      app: stunner
  policyTypes:
  - Egress
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: media-server
    ports:
    - protocol: UDP
EOF
```

In any case, [test your ACLs](https://banzaicloud.com/blog/network-policy) before exposing STUNner
publicly; e.g., the [`turncat` utility](utils/turncat) packaged with STUNner can be used
conveniently for this [purpose](#testing).

### Exposing internal IP addresses

The trick in STUNner is that both the TURN relay transport address and the media server address are
internal pod IP addresses, and pods in Kubernetes are guaranteed to be able to connect
[directly](https://sookocheff.com/post/kubernetes/understanding-kubernetes-networking-model/#kubernetes-networking-model),
without the involvement of a NAT. This makes it possible to host the entire WebRTC infrastructure
over the private internal pod network and still allow external clients to make connections to the
media servers via STUNner.  At the same time, this also has the bitter consequence that internal IP
addresses are now exposed to the WebRTC clients in ICE candidates.

The threat model is that, if possessing the correct credentials, an attacker can scan the *private*
IP address of all STUNner pods and all media server pods via STUNner. This should not pose a major
security risk though: remember, none of these private IP addresses can be reached
externally. Nevertheless, if worried about information exposure then STUNner may not be the best
option for you.

## Caveats

STUNner is a work-in-progress. Some features are missing, others may not work as expected. The
notable limitations at this point are as follows.

* STUNner is not intended to be used as a public STUN/TURN server (there are much better
  [alternatives](https://github.com/coturn/coturn) for this). Thus, it will not be able to identify
  the public IP address of a client sending a STUN binding request to it, and the TURN transport
  relay connections opened by STUNner will not be reachable externally. This is intended: STUNner
  is a Kubernetes ingress gateway which happens to expose a STUN/TURN compatible server to WebRTC
  clients for compatibility.
* Only simple plain-text username/password authentication is implemented. Support for the standard
  STUN/TURN [long term credential
  mechanism](https://datatracker.ietf.org/doc/html/rfc8489#section-9.2) is on the top of our TODO
  list, please bear with us for now.
* Access through STUNner to the rest of the cluster *must* be locked down with a Kubernetes
  `NetworkPolicy`. Otherwise, certain internal Kubernetes services would become available
  externally; see the [notes on access control](#access-control).
* STUNner supports arbitrary scale-up without dropping active calls, but scale-down might
  disconnect calls established through the STUNner pods and/or media server replicas being removed
  from the load-balancing pool. Note that this problem is
  [universal](https://webrtchacks.com/webrtc-media-servers-in-the-cloud) in WebRTC, but we plan to
  do something about it in a later STUNner release so stay tuned.
* [TURN over TCP](https://www.rfc-editor.org/rfc/rfc6062.txt) and the WebRTC DataChannel API are
  not supported at the moment.

## Milestones

* v0.1: Basic connectivity: STUNner + helm chart + simple use cases (Kurento demo).
* v0.2: Security: per-session long-term STUN/TURN credentials.
* v0.3: Performance: eBPF STUN/TURN acceleration.
* v0.4: Observability: Prometheus + Grafana dashboard.
* v0.5: Ubiquity: implement [TURN over TCP](https://www.rfc-editor.org/rfc/rfc6062.txt) and make
  STUNner work with Jitsi, Janus, mediasoup and pion-SFU.
* v1.0: GA
* v2.0: Service mesh: adaptive scaling & resiliency

## Help

STUNner development is coordinated in Discord, send [us](AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](AUTHORS).

MIT License - see [LICENSE](LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
