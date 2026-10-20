# Security

Like any conventional gateway service, an improperly configured STUNner service may easily end up exposing sensitive services to the Internet. The below security guidelines will allow to minimize the risks associated with a misconfigured STUNner gateway service.

## Table of Contents

1. [Threat](#threat)
1. [Locking down STUNner](#locking-down-stunner)
1. [Authentication](#authentication)
1. [Access control](#access-control)
1. [Exposing internal IP addresses](#exposing-internal-ip-addresses)
1. [User quota](#user-quota-only-available-in-the-premium-version)
1. [SIP gateway](#sip-gateway)

## Threat

Before deploying STUNner, it is worth evaluating the potential [security risks](https://www.rtcsec.com/article/slack-webrtc-turn-compromise-and-bug-bounty) a poorly configured public STUN/TURN server poses.  To demonstrate the risks, below we shall use the [tunnel mode of `stunnerd`](cmd/stunnerd.md#tunnel-mode) and `dig` to query the Kubernetes DNS service through a misconfigured STUNner gateway.

Start with a [fresh STUNner installation](INSTALL.md) into an empty namespace called `stunner` and apply the below configuration.

```console
cd stunner
kubectl apply -f deploy/manifests/stunner-expose-kube-dns.yaml
```

This will open a STUNner Gateway called `udp-gateway` at port UDP:3478 and add a UDPRoute with the Kubernetes cluster DNS service as the backend:

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: stunner-udproute
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: kube-dns
          namespace: kube-system
```

Learn the virtual IP address (`ClusterIP`) assigned by Kubernetes to the cluster DNS service:

```console
export KUBE_DNS_IP=$(kubectl get svc -n kube-system -l k8s-app=kube-dns -o jsonpath='{.items[0].spec.clusterIP}')
```

Build [`stunnerd`](cmd/stunnerd.md), fire up a tunnel with a UDP listener on `localhost:5000`, and forward all received packets to the cluster DNS service through STUNner.

```console
./stunnerd --log=all:DEBUG udp://127.0.0.1:5000 k8s://stunner/udp-gateway:udp-listener udp://${KUBE_DNS_IP}:53
```

Now, in another terminal query the Kubernetes DNS service through the tunnel.

```console
dig +short @127.0.0.1 -p 5000 kubernetes.default.svc.cluster.local
```

You should see the internal Cluster IP address for the Kubernetes API server. 

This little experiment should demonstrate the threats associated with a poorly configured STUNner gateway: it may allow external access to *any* UDP service running inside your cluster. The prerequisites for this:

1. the target service *must* be wrapped with a UDPRoute or a TCPRoute;
2. the attacker *must* know at least one pod address or the ClusterIP for the targeted service.

Should any of these prerequisites fail, STUNner will block access to the target service.

Now rewrite the backend service in the UDPRoute to an arbitrary non-existent service.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: stunner-udproute
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: dummy
```

Repeat the above `dig` command to query the Kubernetes DNS service again and observe how the query times out. This demonstrates that a properly locked down STUNner installation blocks all access outside of the backend services explicitly opened up via a UDPRoute.

## Locking down STUNner

Unless properly locked down, STUNner may be used maliciously to open a tunnel to any UDP or TCP service running inside a Kubernetes cluster. Accordingly, it is critical to tightly control the pods and services exposed via STUNner.

STUNner's basic security model is as follows:

> In a properly configured deployment, STUNner provides the same level of security as a media server pool exposed to the Internet over a public IP address. A malicious attacker, even possessing a valid TURN credential, can reach only the media servers deployed behind STUNner, but no other services.

The below security considerations will greatly reduce this attack surface even further. In any case, use STUNner at your own risk.

## Authentication

By default, STUNner uses a single static username/password pair for all WebRTC clients and the password is available in plain text at the clients (`static` authentication mode). Anyone with access to the static STUNner credentials can open a UDP tunnel via STUNner, provided that they know the private IP address of the target service or pod and provided that a UDPRoute exists that specifies the target service as a backend. This means that a service is exposed only if STUNner is explicitly configured so.

For production WebRTC deployments we recommend the `ephemeral` authentication mode, which uses per-client fixed lifetime username/password pairs. This makes it more difficult for attackers to steal and reuse STUNner's TURN credentials. See the [authentication guide](AUTH.md) for configuring STUNner with `ephemeral` authentication.

Note that STUNner can also be deployed as a STUN server without enabling the TURN protocol (only available in the premium tiers), in which case it needs no authentication. Refer to the [user guide](PREMIUM_REFERENCE.md) for the details.

### Plain listeners

A plain UDP or TCP listener relays raw client flows to its preconfigured peer with *no authentication whatsoever*: raw flows carry no credentials, so anyone who can reach the listener socket can relay traffic to the peer. An open plain listener is therefore unauthenticated open ingress to the pinned peer, limited only to the endpoints of the server's clusters, with no per-user quota and no rate limiting. A Gateway listener is plain when its protocol is `UDP`, `TCP`, `TLS` or `DTLS` instead of `TURN-UDP`, `TURN-TCP`, `TURN-TLS` or `TURN-DTLS` or with a SIPTrunk in the parameters reference. Keep plain listeners on internal or otherwise firewalled addresses unless the peer service is meant to be publicly reachable.

A typical use is exposing a pure TCP workload that runs only inside the cluster, such as the RTMP ingest of a media server. This lets drones, IP cameras and encoders push streams to the media server over TCP port 1935. The below Gateway exposes a [MediaMTX](https://github.com/bluenviron/mediamtx) server in the `media-plane` namespace on a plain TCP listener, next to the TURN listeners of the same STUNner installation.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: rtmp-ingest
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: rtmp
      port: 1935
      protocol: TCP
---
apiVersion: stunner.l7mp.io/v1
kind: TCPRoute
metadata:
  name: rtmp-ingest
  namespace: stunner
spec:
  parentRefs:
    - name: rtmp-ingest
  rules:
    - backendRefs:
        - name: mediamtx
          namespace: media-plane
          port: 1935
```

Observe the `protocol: TCP` setting on the listener: every connection that reaches port 1935 is relayed to the `mediamtx` Service at port 1935, without any credentials. STUNner adds no authentication of its own here, so the workload must authenticate its clients: configure publish credentials in MediaMTX before exposing it. To keep a plain listener inside the cluster instead, add the `stunner.l7mp.io/service-type: ClusterIP` annotation to the Gateway.

## Access control

STUNner requires the user to explicitly open up external access to internal services by specifying a proper UDPRoute. For instance, the below UDPRoute allows access *only* to the `media-server` service in the `media-plane` namespace, and nothing else.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: stunner-udproute
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server
          namespace: media-plane
```

STUNner admits a backend by IP address and enforces the `port` and `endPort` of a backend reference: a client allowed to reach a backend reaches only those ports of it. For a second line of defense, use a NetworkPolicy as below.

For hardened deployments, it is possible to add a second level of isolation between STUNner and the rest of the workload using the Kubernetes NetworkPolicy facility. Creating a NetworkPolicy will essentially implement a firewall, blocking all access from the source to the target workload except the services explicitly whitelisted by the user. The below example allows access from STUNner to *any* media server pod labeled as `app=media-server` in the `default` namespace over the UDP port range `[10000:20000]`, but nothing else.

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

Kubernetes network policies can be easily [tested](https://banzaicloud.com/blog/network-policy) before exposing STUNner publicly; e.g., the [tunnel mode of `stunnerd`](cmd/stunnerd.md#tunnel-mode) can be used conveniently for this [purpose](examples/simple-tunnel/README.md).

## Exposing internal IP addresses

The trick in STUNner is that both the internal proxy address (e.g., the TURN relay transport address) and the media server address are internal pod IP addresses, and pods in Kubernetes are guaranteed to be able to connect [directly](https://sookocheff.com/post/kubernetes/understanding-kubernetes-networking-model/#kubernetes-networking-model) without the involvement of a NAT. This makes it possible to host the entire media server infrastructure over the private internal pod network and still allow external clients to make connections to the media servers via STUNner.  At the same time, this also has the bitter consequence that internal IP addresses may now exposed to the Internet: e.g., WebRTC clients may read servers' pod addresses from the ICE candidates.

The threat model is that, possessing the correct credentials, an attacker can scan the *private* IP address of all STUNner pods and all media server pods. This should pose no major security risk though: remember, none of these private IP addresses can be reached externally. The attack surface can be further reduced to the STUNner pods' private IP addresses by using the [symmetric ICE mode](DEPLOYMENT.md#symmetric-ice-mode).

Nevertheless, if worried about information exposure then STUNner may not be the best option at the moment. In later releases, we plan to implement a feature to obscure the relay transport addresses returned by STUNner. Please file an issue if you think this limitation is a blocker for your use case.

## User quota (only available in the premium version)

In order to prevent potential Denial-of-Service (DoS) attacks that may be launched by an attacker creating a huge number of parallel TURN allocations to overwhelm STUNner, it is possible to impose a user quota on the number of simultaneous allocations that can be made with the same TURN credential. Refer to the `UserQuota` feature description in the [premium user guide](PREMIUM_REFERENCE.md) for the details.

## SIP gateway

> [!WARNING]
> The SIP gateway is in beta: the API may change in any release and there is no support on any tier.
> Please test and report issues, but in any way, use it at your own risk.

A public SIP port is found and probed by scanners within minutes. STUNner proxies only inbound
calls, so a SIP gateway cannot be abused to place calls, but every call that reaches your SIP
server may still start a session unless properly locked down. Below are a set of best-practices to
make sure only SIP trunks can place calls in the cluster:

- **Admit only the SIP carriers.** List the carriers' signaling address ranges in the SIPTrunk's
  `allowedSources`; an empty list admits no inbound call at all, which is right for
  registration-only trunks. The filter needs the carriers' real source addresses, so set
  `stunner.l7mp.io/external-traffic-policy: local` on the Gateway. Note that register-only SIP
  trunks require no `allowedSources` setting.
- **Prefer TLS.** Use `TLS` listeners for SIP over TLS from carriers when available, and `sips:`
  registrars for registrations, so neither the signaling nor the credentials cross the Internet in
  the clear. SRTP media is relayed untouched: STUNner never holds the media keys.
- **Keep the media port tight.** STUNner relays media only for the calls it set up, and only from
  the source addresses in their SDP; anything else arriving at the media port is dropped.
- **Keep the credentials in Secrets.** Registration credentials live in the Secret the
  registration's `authRef` names, in the namespace of the SIPTrunk.
- **Cap the calls.** Every tier caps the parallel calls per SIP Gateway (10 free, 100 member), and
  a GatewayConfig `userQuota` below the cap lowers it: the cap also bounds what a misconfigured
  trunk can cost.

