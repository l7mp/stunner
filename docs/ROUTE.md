# Routes

Routes attach to the listeners of Gateways and name the backends the clients of a listener reach:
UDPRoute and TCPRoute for backends in the cluster, TURNRoute for peers behind an upstream TURN
server, and StaticService for backends outside the cluster.

1. [UDPRoute](#udproute)
1. [TCPRoute](#tcproute)
1. [TURNRoute](#turnroute)
1. [StaticService](#staticservice)

## UDPRoute

UDPRoute resources can be attached to Gateways in order to specify the backend services permitted to be reached via the Gateway. Multiple UDPRoutes can attach to the same Gateway, and each UDPRoute can specify multiple backend services; in this case access to *all* backends in *each* of the attached UDPRoutes is allowed. A UDPRoute can be attached to a Gateway by setting the `parentRef` to the Gateway's name and namespace. This is, however, contingent on whether the Gateway accepts routes from the given namespace: customize the `allowedRoutes` per each Gateway listener to control which namespaces the listener accepts routes from.

The below UDPRoute will configure STUNner to route client connections received on the Gateway called `udp-gateway` to *any UDP port* on the pods of the media server pool identified by the Kubernetes service `media-server-pool` in the `media-plane` namespace.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
```

Note that STUNner provides its own UDPRoute resource next to the official UDPRoute of the Gateway API, which STUNner supports at v1 as well. The official UDPRoute requires a `port` in every backend reference: a TURN listener ignores it, other listeners dial the backend at it (see below). The official route has no `endPort`, and its backend weights are ignored.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
          port: 1
```

Below is a reference of the most important fields of the STUNner UDPRoute `spec`.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `parentRefs` | `list` | Name/namespace of the Gateways to attach the route to. If no namespace is given, then the Gateway will be searched in the UDPRoute's namespace. | Yes |
| `rules.backendRefs` | `list` | A list of backends (Services or StaticServices) reachable through the UDPRoute. It is allowed to specify a service from a namespace other than the UDPRoute's own namespace. | No |

Backend reference configuration is as follows:

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `group` | `string` | API group for the backend, either empty string for Service backends or `stunner.l7mp.io` for StaticService backends. Default: `""`. | No |
| `kind` | `string` | The kind of the backend resource, either `Service` or `StaticService`. Default: `Service`. | No |
| `name` | `string` | Name of the backend Service or StaticService. | Yes |
| `namespace` | `string` | Namespace of the backend Service or StaticService. | No |
| `port` | `int` | The port a listener other than TURN dials the backend at: a port of the Service, dialed at its target port on the pods (a port the Service does not have is dialed as is); the port itself for a StaticService. Without it such a listener has nothing to dial. Also the start of the port range of the backend, effective only with `endPort`: a `port` alone restricts nothing. Default: empty (every port).| No (Yes for listeners other than TURN) |
| `endPort` | `int` | End of the port range of the backend, if `port` is also specified: a TURN listener, and the media listener of a SIP Gateway, reach the peers of the backend only on the ports of the range. Default: empty.| No |

The pods of a Service backend play two roles. A listener that dials its backends (every listener
but TURN, and the SIP signaling of a SIP Gateway) opens new connections only to the ready pods, or,
while every pod is terminating, to the terminating pods that still pass their readiness probe, as
Kubernetes does; the live connections of a pod that turns unready keep going. A TURN listener and
the media listener of a SIP Gateway admit every pod of the Service as a peer, ready or not, because
which pod a client talks to is the application's choice.

UDPRoute resources are safe for modification: [`stunnerd`](cmd/stunnerd.md) knows how to reconcile modified routes without restarting any listeners/TURN servers.

## TCPRoute

TCPRoute is the TCP counterpart of the UDPRoute: same spec, same backend kinds, but the relay leg towards the backend is a TCP connection instead of a UDP flow. Use it when the backend service speaks TCP.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: TCPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: tcp-gateway
  rules:
    - backendRefs:
        - name: media-server-pool
          namespace: media-plane
```

The `spec` and the backend reference fields are identical to those of the [UDPRoute](#udproute). As with UDPRoutes, the official Gateway API TCPRoute resource can be used instead of STUNner's own; a same-name STUNner-native TCPRoute masks the official one.

TCPRoute resources are safe for modification, exactly like UDPRoutes.

> [!NOTE]
> There is no protocol conversion anywhere: the client's allocation fixes the protocol of the relay leg, so a TCP backend is reachable only through an [RFC 6062](https://datatracker.ietf.org/doc/html/rfc6062) TCP allocation. 

## TURNRoute

TURNRoute routes the clients of a Gateway listener to their peers through an upstream TURN server, for instance another STUNner or a public TURN service. On a TURN listener the allocations of the clients are relayed through the upstream TURN server, and every peer is admitted: the upstream TURN server decides what its clients may reach. On a plain (L4) listener each client connection is relayed to a backend of the route through the upstream TURN server.

The below TURNRoute relays the allocations made at the `udp-listener` of the Gateway `udp-gateway` through the upstream TURN server `turn.example.com`, authenticating with the credentials in the Secret `upstream-auth`.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: TURNRoute
metadata:
  name: upstream-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
      sectionName: udp-listener
  tunnel:
    uri: "turns:turn.example.com:443?transport=tcp"
    authRef:
      name: upstream-auth
---
apiVersion: v1
kind: Secret
metadata:
  name: upstream-auth
  namespace: stunner
type: Opaque
stringData:
  type: static
  username: my-user
  password: my-password
```

Below is a reference of the TURNRoute `spec`.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `parentRefs` | `list` | Name/namespace of the Gateways to attach the route to, as for UDPRoutes. | Yes |
| `rules.backendRefs` | `list` | The backends (Services or StaticServices) a plain (L4) listener dials through the upstream TURN server, at the `port` of the backend reference. A TURN listener does not use them. | No |
| `tunnel.uri` | `string` | The upstream TURN server as an [RFC 7065](https://datatracker.ietf.org/doc/html/rfc7065) TURN URI, e.g., `turn:turn.example.com:3478?transport=udp`. | Yes |
| `tunnel.authRef` | `object` | Reference to the Secret holding the credentials presented to the upstream TURN server, possibly in another namespace. Default: no authentication. | No |
| `tunnel.insecure` | `bool` | Skip the verification of the certificate of the upstream TURN server over TLS and DTLS. Default: `false`. | No |
| `tunnel.sni` | `string` | The server name the certificate of the upstream TURN server is verified against. Default: the host of the URI. | No |

The Secret referenced by `tunnel.authRef` holds the key `type`, either `static` (the default) or `ephemeral`, the optional keys `realm` and `lifetime` (the validity of the generated ephemeral credentials, e.g., `1h`), and the credentials: `username` and `password` for static, `secret` for ephemeral authentication.

> [!NOTE]
> If a TURNRoute is attached to a TURN listener then it must be the only route attached to the listener. With more than one route, the first route of the listener is used to create the relay connections and the operator logs a warning.

## StaticService

When the target backend of a UDPRoute is running *inside* Kubernetes then the backend is always a proper Kubernetes Service. However, when the target is deployed *outside* Kubernetes then there is no Kubernetes Service that could be configured as a backend. This is particularly problematic in the cases when STUNner is used as a public TURN service. For such deployments, the StaticService resource provides a way to assign a routable IP address range to a UDPRoute.

The below StaticService represents a hypothetical Kubernetes Service backing a set of pods with IP addresses in the range `192.0.2.0/24` or `198.51.100.0/24`.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: StaticService
metadata:
  name: static-svc
  namespace: stunner
spec:
  prefixes:
    - "192.0.2.0/24"
    - "198.51.100.0/24"
```

Assigning this StaticService to a UDPRoute allows access to *any* IP address in the specified ranges.

```yaml
apiVersion: stunner.l7mp.io/v1
kind: UDPRoute
metadata:
  name: media-plane-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - group: stunner.l7mp.io
          kind: StaticService
          name: static-svc
```

The StaticService `spec.prefixes` must be a list of proper IPv4 or IPv6 prefixes: any IP address in any of the listed prefixes will be whitelisted. An entry may restrict the ports of the prefix with a port range, written as `<prefix>:<min-max>` (e.g., `192.0.2.0/24:<10000-20000>`), which overrides the `port`/`endPort` of the backend reference. Note that a prefix admits only peers of its own address family, so use the prefix `0.0.0.0/0` to provide wildcard access via a UDPRoute, and add `::/0` next to it on an IPv6-only or dual-stack cluster.

> [!WARNING]
>
> Never use StaticServices to access Services running *inside* Kubernetes, this may open up an unintended backdoor to your cluster. Use StaticServices only with *external* target backends.
