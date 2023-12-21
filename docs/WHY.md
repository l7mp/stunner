# Introduction

STUNner is a *WebRTC media gateway for Kubernetes*. All words matter here: indeed STUNner is for
*WebRTC*, so it is specifically designed to help dealing with the intricacies of WebRTC protocol
encapsulations, it is a *media gateway* so its job is to ingest WebRTC audio/video streams into a
virtualized media plane, and it is *opinionated towards Kubernetes*, so everything around STUNner
is designed and built to fit into the Kubernetes ecosystem. That being said, STUNner can easily be
used outside of this context (e.g., as a regular STUN/TURN server), but this is not the main focus.

## The problem

The pain points STUNner is trying to solve are all related to that Kubernetes and WebRTC are
currently foes, not friends.

Kubernetes has been designed and optimized for the typical HTTP/TCP Web workload, which makes
streaming workloads, and especially UDP/RTP based WebRTC media, feel like a foreign citizen. Most
importantly, Kubernetes runs the media server pods/containers over a private L3 network over a
private IP address and the several rounds of Network Address Translation (NAT) steps are required
to ingest media traffic into this private pod network. Most cloud load-balancers apply a DNAT step
to route packets to a Kubernetes node and then an SNAT step to inject a packet into the private pod
network, so that by the time a media packet reaches a pod essentially all header fields in the [IP
5-tuple](https://www.techopedia.com/definition/28190/5-tuple) are modified except the destination
port. Then, if any pod sends the packet over to another pod via a Kubernetes service load-balancer
then the packet will again undergo a DNAT step, and so on.

The *Kubernetes dataplane teems with NATs*. This is not a big deal for the web protocols Kubernetes
was designed for, since each HTTP/TCP connection involves a session context that can be used by a
server to identify clients. This is not the case with WebRTC media protocol stack though, since
UDP/RTP connections do not involve anything remotely similar to an HTTP context. Consequently, the
only "semi-stable" connection identifier WebRTC servers can use to identify a client is by
expecting the client's packets to arrive from a negotiated IP source address and source port. When
the IP 5-tuple changes, for instance because there is a NAT in the datapath, then WebRTC media
connections break. Due to reasons which are mostly historical at this point, *UDP/RTP connections
do not survive not even a single NAT step*, let alone the 2-3 rounds of NATs a packet regularly
undergoes in the Kubernetes dataplane.

## The state-of-the-art

The current stance is that the only way to deploy a WebRTC media server into Kubernetes is to
exploit a [well-documented Kubernetes
anti-pattern](https://kubernetes.io/docs/concepts/configuration/overview): *running the media
server pods in the host network namespace* of Kubernetes nodes (using the `hostNetwork=true`
setting in the pod's container template). This way the media server shares the network namespace of
the host (i.e., the Kubernetes node) it is running on, inheriting the public address (if any) of
the host and (hopefully) sidestepping the private pod network with the involved NATs.

There are *lots* of reasons why this deployment model is less than ideal:

- **Each node can run a single pod only.** The basic idea in Kubernetes is that nodes should run
  lots of pods simultaneously, perhaps in the hundreds, in order to benefit from resource pooling
  and statistical multiplexing, amortize the costs of running the per-node Kubernetes boilerplate
  (the kubelet, kube-proxy, etc.), enable elastic scaling, etc. Using host-networking breaks this
  promise: since there is no guarantee that two media server pods would not both allocate the same
  UDP port to terminate a UDP/RTP stream, deploying both into the host-network namespace of the
  same node would easily result in hard-to-debug port clashes.

- **It inhibits elastic scaling.** Kubernetes scales workloads at the per-pod granularity. When
  each node occupies an entire Kubernetes node, scaling the media plane equals adding/removing
  Kubernetes nodes, which is a cumbersome, lengthy, and most importantly, costly process. In
  addition, it becomes very difficult to provision the resource requests and limits of each media
  server node: a `t2.small` (1 vCPU/2 GB mem) may be too small for a large video-conferencing room,
  while a `t2.xlarge` (8 vCPU/32 GB mem) is too costly for running, say, a 2-party call. Worse yet,
  the decision has to be made at installation time.

- **It is a security nightmare.** Given today's operational reality, exposing a fleet of media
  servers to the Internet over a public IP address, and opening up all UDP ports for potentially
  malicious access, is an adventurous undertaking to say the least. Wouldn't it be nice to hide
  your media servers behind a secure perimeter defense mechanism and lock down *all* uncontrolled
  access and nefarious business by running it over a private IP?

- **It is a terrible waste of resources.** Think about this: when you use Kubernetes you pay a
  double virtualization price. Namely, the node itself is usually just regular VM on a physical
  server, which means a first layer of virtualization, on top of which the pod runs in another
  container virtualization layer. It is only worth paying this prize if you amortize the cost of
  the VM across many containers/pods. If you run a single media server per node then why using
  Kubernetes at all? Just use a simple VM instead, and pay the virtualization cost only once.

- **You still need a STUN/TURN server for clients to reach the cluster.** Putting the media server
  over a public IP solves only half of the problem: the server side. For the client side you still
  need a costly NAT traversal service to let clients behind a NAT to connect to your media
  servers. But why not putting the NAT-traversal facilities right into your Kubernetes cluster and
  share the same facility for the client and the server side?

- **Kubernetes nodes might not even have a public IP address.** There are lots of locked-down
  hosted Kubernetes offerings (e.g., GKE private clusters) where nodes run without a public IP
  address for security reasons. This then precludes the host-networking hack. But even if nodes are
  publicly available, many Kubernetes services simply disable host-networking all together (e.g.,
  [GKE Autopilot](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-overview))
  exactly because it is such an intrusive hack.

## Why STUNner

Long story short, running your media servers with `hostNetwork=true` is just not THE WAY in
Kubernetes! There is a reason why, instead of just installing the entire workload into the
host-network namespace, Kubernetes relies on a comprehensive collection of [Gateway
services](https://gateway-api.sigs.k8s.io) to ingest traffic into the cluster in a controlled
manner. STUNner is exactly such a gateway service, carefully tailored to WebRTC media.

Using STUNner allows you to specify a set of high-level declarative policies (UDP/TCP ports,
authentication credentials, etc.) to define the way you want traffic to enter the cluster and
control the internal services client media can reach (i.e., UDP routes and backend services). This
will then make it possible to leave behind the host-networking hack once and for all, and run,
scale and monitor the media-plane workload in the usual private pod-network behind the secure
perimeter defense provided by STUNner. From here, the rest is just PURE STUNner MAGIC!  (In fact,
it is not: STUNner is just an everyday STUN/TURN server with some small tweaks to let it play
nicely with Kubernetes.)

