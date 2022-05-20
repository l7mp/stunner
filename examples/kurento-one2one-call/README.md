# Media-plane mode: One to one video call with Kurento via STUNner

This tutorial demonstrates the fully fledged *media-plane deployment model* of STUNner, that is,
when WebRTC clients connect to each other via the Kurento media server deployed into
Kubernetes. The media servers in turn are exposed to the clients via a STUNner gateway.  The demo
has been adopted from the [Kurento](https://www.kurento.org/) [one-to-one video call
tutorial](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html), with
minimal
[modifications](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call) to
deploy it into Kubernetes and integrate it with STUNner. The demo contains a
[Node.js](https://nodejs.org) application server for creating a browser-based two-party WebRTC
video-call, plus the Kurento media server deployed behind STUNner for media exchange and,
potentially, automatic audio/video transcoding.

To quote the original Kurento
[documentation](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html):

> This web application consists of a one-to-one video call using WebRTC technology. In other words,
> this application provides a simple video softphone.

In the original demo, both the application server and the media server are expected to be reachable
via a public routable IP address (in the case of the
[demo](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-one2one.html) this was
the `localhost`). This default demo setup was adopted here for using with STUNner, so that it can
be installed into a standard Kubernetes cluster.

![STUNner media-plane deployment architecture](../../doc/stunner_arch.svg)

In this demo you will learn how to:
* integrate a typical WebRTC application server with STUNner,
* deploy the modified application server into a Kubernetes,
* deploy the Kurento media server into Kubernetes behind STUNner,
* secure a STUNner deployment, and
* scale a standard WebRTC workload using Kubernetes and STUNner.

## Installation

### Prerequisites

Consult the [STUNner installation and configuration guide](/doc/INSTALL.md) to set up STUNner.

### Quick installation

STUNner supports two installation options: a self-contained and easy-to-use Helm chart and a manual
installation method using static Kubernetes manifests.

The simplest way to deploy the demo is using its Helm chart. It is important to wait with the
installation until the `STUNNER_PUBLIC_ADDR` key is added with a valid address to the `ConfigMap`
called `stunner-config`.

```console
$ helm repo add stunner https://l7mp.io/stunner
$ helm repo update
$ helm install kurento-one2one-call stunner/kurento_webrtc_demo
```

The other way to deploy the demo is to clone the [STUNner git
repository](https://github.com/l7mp/stunner) and deploy the
[manifest](/examples/kurento-one2one-call.yaml) packaged with STUNner.

```console
$ git clone https://github.com/l7mp/stunner
$ cd stunner
$ kubectl apply -f examples/kurento-one2one-call/kurento-one2one-call.yaml
```

The demo exposes a publicly available HTTPS web service on port 8443. Kubernetes assigns an
ephemeral public IP address to the web service, so first we need to learn the external IP.

```console
$ kubectl get service webrtc-server -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

The result should be a valid IP address in the form `A.B.C.D`. If no IP address is returned, wait a
bit more until Kubernetes successfully assigns the external IP. If still no success, try to use the
[`NodePort`
service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) to reach
the application server. Then, direct your browser to the URL `https://<A.B.C.D>:8443` (but
substitute the external IP address first), accept the self-signed certificate, register some user
name, and you can immediately start to video-chat with anyone registered at the service. To try it
out, open another browser tab, repeat the above registration steps and enjoy a nice video-call with
yourself.

## Porting a WebRTC application to STUNner

The [Kurento
docs](https://doc-kurento.readthedocs.io/en/latest/tutorials/node/tutorial-one2one.html) cover all
the WebRTC related information, below we just summarize the changes we have made to the Kurento
demo code to make it work with STUNner and Kubernetes. The full source code of the application
server can be found
[here](https://github.com/l7mp/kurento-tutorial-node/tree/master/kurento-one2one-call).

1. Store the WebRTC application server's TLS certificate in a Kubernetes `Secret` (called
   `webrtc-server-secret`) and map the keys into the file system of the application server
   pod. This allows easy and dynamic customization of the TLS certificates used with the demo.
2. Deploy the Kurento media server into a `Deployment` (called `kms`). Observe that Kurento has
   been configured with *no STUN and TURN servers* and *no external IP addresses*, and it runs in
   an ordinary Kubernetes pod at an ephemeral private IP address which is not available from
   browsers directly. Here is where the magic happens: STUNner makes sure that WebRTC media just
   keeps flowing between clients and the media server, without *any* of the two being directly
   accessible via a public IP.
3. Expose the media server pool, i.e., the Kurento media server deployment, over the Kubernetes
   service `kms-control` over the TCP port 8888. This assigns a virtual IP address (a `ClusterIP`)
   that the application server can use to configure the WebRTC endpoints and media pipelines in
   Kurento. Note that this address is private and it is not reachable from outside the cluster.
4. Modify the WebRTC [application server
   logic](https://github.com/l7mp/kurento-tutorial-node/blob/master/kurento-one2one-call/server.js)
   to (1) use the STUNner [Node.js authentication
   library]https://www.npmjs.com/package/@l7mp/stunner-auth-lib) to generate a WebRTC [ICE server
   configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer) and (2) return the
   ICE configuration to the WebRTC clients in the `registerResponse` messages indicating a
   successful user registration.
5. Modify the [JavaScript
   code](https://github.com/l7mp/kurento-tutorial-node/blob/master/kurento-one2one-call/static/js/index.js)
   served to clients to (1) store the ICE server configuration returned from the application
   server, and (2) set up the WebRTC `PeerConnection` at both the caller and the callee so that the
   ICE handshake will use STUNner's public address and port with the correct STUN/TURN credentials
   for establishing the media connection.
6. Use the
   [Dockerfile](https://github.com/l7mp/kurento-tutorial-node/blob/master/kurento-one2one-call/Dockerfile)
   packaged to build the modified WebRTC application server container image or deploy the [prebuilt
   image](https://hub.docker.com/repository/docker/l7mp/kurento-one2one-call-server).
7. Start the modified WebRTC application server in a Kubernetes `Deployment` (called
   `webrtc-server`). Note that the STUNner configuration is made available to the application
   server in environment variables taken from STUNner's default Kubernetes `ConfigMap` and the TLS
   keys are taken from the Kubernetes `Secret` configured above.
8. Expose the application server on a Kubernetes `LoadBalancer` service so that external clients
   can reach it via the TCP port 8443.
9. And finally, as a critical step, make sure that STUNner is permitted to forward UDP/RTP media
   packets to the media servers. Recall, by default all internal access from STUNner is locked down
   by a Kubernetes `NetworkPolicy`. The demo installation script opens this ACL up so that STUNner
   can reach all WebRTC endpoints configured on the Kurento media servers, but just the WebRTC
   ports and *nothing else* (but see the below [security notice](/#security) on access control).

And that's all. We added only 32 lines of code to the Kurento demo to make it work with Kubernetes,
with most of the changes needed to return the ephemeral public STUN/TURN URI and credentials to the
clients. If you allocate STUNner to a stable IP and domain name, you don't even need to modify
*anything* in the demo and it will just work.

## Scaling

Suppose that the single STUNner instance fired up by the default installation script is no longer
sufficient; e.g., due to concerns related to performance or availability.  In a "conventional"
privately hosted setup, you would need to provision a new physical STUN/TURN server instance,
allocate a public IP, add the new server to your STUN/TURN server pool, and monitor the liveliness
of the new server continuously. This takes a lot of manual effort and considerable time. In
Kubernetes, however, you can use a single command to *scale STUNner to an arbitrary number of
replicas*. Kubernetes will potentially add new nodes to the cluster if needed, and the new replicas
will be *automatically* added to the STUN/TURN server pool accessible behind the (single) public IP
address/port pair (`<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>`), with UDP/RTP streams
conveniently being load-balanced across STUNner replicas.

For instance, the below command will fire up 15 STUNner replicas, usually in a matter of seconds.

```console
$ kubectl scale deployment stunner --replicas=15
```
You can even use Kubernetes
[autoscaling](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) to adapt
the size of the STUNner pool to the actual demand.

Notably, the media server pool can likewise be (auto-)scaled with Kubernetes
effortlessly. Conventional WebRTC media servers are unique snowflakes: tied to a public IP address
and managed by hand. With STUNner the entire WebRTC infrastructure can be deployed into
Kubernetes. As media servers are now ephemeral and disposable, running in ordinary Kubernetes pods,
it is easy to replicate and scale the media plane with automated tools.

The below command will scale the Kurento media server pool to 20 instances and again, automatic
health-checks and load-balancing should just work as expected.

```console
$ kubectl scale deployment kms --replicas=20
```

## Security

As described in the [STUNner security guide](/doc/SECURITY.md), it is critical to lock down
(potentially hostile) access to sensitive services running inside the cluster via STUNner. The
necessary ACLs are automatically configured by the installation manifests above; below we describe
what's happening in the background.

For a secure STUNner deployment, we need to ensure that the only service allowed for clients to
access via the transport relay connections allocated by STUNner is the media server pool, and only
over UDP. This will be done using an Access Control List, which in Kubernetes is called a
`NetwotkPolicy`.

By default, STUNner is deployed into the `default` namespace and all STUNner pods are labeled as
`app=stunner`. In addition, the media server runs in the same namaspace using the label `app=kms`,
and WebRTC endpoints on the Kurento server are assigned from the UDP port range
[10000:20000]. Then, the below `NetworkPolicy` ensures that all access from any STUNner pod to any
media server pod is allowed over any UDP port between 10000 and 20000, and all other network access
from STUNner is denied.

```yaml
$ kubectl apply -f - <<EOF
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
          app: kms
    ports:
    # Only UDP ports 10000-20000 are allowed between 
    #   the source-destination pairs
    - protocol: UDP
      port: 10000
      endPort: 20000
EOF
```

Note that certain Kubernetes CNIs do not support network policies, or support only a subset of what
STUNner needs. E.g., Kubernetes <1.22 does not support the `endPort` configuration in the above
`NetworkPolicy`. For such cases, the below ACL allows STUNner to access *all* UDP ports on the
media server. This is less secure, but still blocks malicious access via STUNner to any service
other than the media servers.

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
          app: kms
    ports:
    - protocol: UDP
EOF
```

### Clean up

Delete the demo deployment and lock down network access from STUNner using the below commands:

```console
$ kubectl delete deployment webrtc-server
$ kubectl delete service webrtc-server
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

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Demo adopted from [Kurento](https://www.kurento.org).
