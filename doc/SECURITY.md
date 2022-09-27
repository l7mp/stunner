# Security

Like any conventional gateway service, an improperly configured STUNner service may easily end up
exposing sensitive services to the Internet. The below security guidelines will allow to minimize
the risks associated with a misconfigured STUNner.

## Threat

Before deploying STUNner, it is worth evaluating the potential [security
risks](https://www.rtcsec.com/article/slack-webrtc-turn-compromise-and-bug-bounty) a poorly
configured public STUN/TURN server poses.  To demonstrate the risks, below we shall use the
[`turncat`](/cmd/turncat) utility to reach the Kubernetes DNS service through a misconfigured
STUNner gateway.

Start with a [fresh STUNner installation](/doc/INSTALL.md) into an empty namespace called `stunner`
and apply the below configuration. 

```console
cd stunner
kubectl apply -f deploy/manifests/stunner-expose-kube-dns.yaml
```

This will open a STUNner Gateway at port UDP:3478 and add a UDPRoute with the Kubernetes cluster
DNS service as the backend:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
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

Build `turncat`, the Swiss-army-knife [testing tool](/cmd/turncat/README.md) for STUNner, fire up a
UDP listener on port on `localhost:5000` and forward all received packets to the cluster DNS
service through STUNner.

```console
./turncat --log=all:DEBUG udp://127.0.0.1:5000 k8s://stunner/stunnerd-config:udp-listener udp://${KUBE_DNS_IP}:53
```

Now, in another terminal query the Kubernetes DNS service through the `turncat` tunnel to learn the
internal service address allocated by Kubernetes for STUNner:

```console
dig +short @127.0.0.1 -p 5000 stunner.default.svc.cluster.local
```

You should see the internal Cluster IP address allocated by Kubernetes for the STUNner dataplane
service. Try experiment with other FQDNs, like `kubernetes.default.svc.cluster.local`, etc., the
query should return the corresponding internal service IP.

This little experiment demonstrates the threats associated with a poorly configured STUNner
gateway. Note that reaching internal Kubernetes services via STUNner is contingent on two
conditions: first, the target service must run over UDP (e.g., the `kube-dns`), since STUNner's
transport relay connections are limited to UDP, and second, the user must specifically add a
UDPRoute to the target service. 

Now rewrite the backend service in the UDPRoute to an arbitrary non-existent service.

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
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

Repeat the above `dig` command to query the Kubernetes DNS service again and observe how the query
times out. This demonstrates that a properly locked down STUNner installation blocks all accesses
outside of the backend services the user explicitly opens up via a UDPRoute.

## Locking down STUNner

Unless properly locked down, STUNner may be used maliciously to open a tunnel to any UDP service
running inside a Kubernetes cluster. Accordingly, it is critical to tightly control the pods and
services inside a cluster exposed via STUNner.

The below security considerations will greatly reduce the attack surface associated with
STUNner. In any case, use STUNner at your own risk.

## Authentication

By default, STUNner uses a single statically set username/password pair for all clients and the
password is available in plain text at the clients (`plaintext` authentication mode). Anyone with
access to the static STUNner credentials can open a UDP tunnel to any service inside the Kubernetes
cluster.

For more security sensitive workloads, we recommend the `longterm` authentication mode, which uses
per-client fixed lifetime username/password pairs. See the [authentication guide](/doc/AUTH.md) for
configuring STUNner with `longterm` authentication.

## Access control

STUNner requires the user to explicitly open up external access to services internal to the cluster
using a UDPRoute. For instance, the below UDPRoute allows access *only* to the media-server service
in the media-plane namespace, and nothing else.


```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
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
        - namespace: media-plane
```

To avoid potential misuse, STUNner disables open access to the cluster (the user can explicitly
open an open `stunnerd` cluster in the standalone mode though but this is discouraged for
security).

For hardened deployments, it is possible add a second level of isolation between STUNner and the
rest of the Kubernetes cluster using the Kubernetes NetworkPolicy facility. Creating a
NetworkPolicy will essentially implement a firewall, blocking all access from the source to the
target workload unless explicitly whitelisted in the NetworkPolicy. The below example allows access
from STUNner to *any* media server pod labeled as `app=media-server` in the `default` namespace
over the UDP port range `[10000:20000]`, but nothing else.

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

Kubernetes network policies can be easily [tested](https://banzaicloud.com/blog/network-policy)
before exposing STUNner publicly; e.g., the [`turncat` utility](../cmd/turncat) packaged with
STUNner can be used conveniently for this [purpose](/examples/simple-tunnel/README.md).

## Exposing internal IP addresses

The trick in STUNner is that both the TURN relay transport address and the media server address are
internal pod IP addresses, and pods in Kubernetes are guaranteed to be able to connect
[directly](https://sookocheff.com/post/kubernetes/understanding-kubernetes-networking-model/#kubernetes-networking-model),
without the involvement of a NAT. This makes it possible to host the entire WebRTC infrastructure
over the private internal pod network and still allow external clients to make connections to the
media servers via STUNner.  At the same time, this also has the bitter consequence that internal IP
addresses are now exposed to the WebRTC clients in ICE candidates.

The threat model is that, possessing the correct credentials, an attacker can scan the *private* IP
address of all STUNner pods and all media server pods via STUNner. This should not pose a major
security risk though: remember, none of these private IP addresses can be reached
externally. Nevertheless, if worried about information exposure then STUNner may not be the best
option at the moment. In later releases, we plan to remove this artifact.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
