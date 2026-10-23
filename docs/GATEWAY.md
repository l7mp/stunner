# Gateway

A Gateway describes the listeners STUNner exposes to clients, the public face of every use case:
TURN listeners for WebRTC, plain UDP, TCP, TLS and DTLS listeners, SIP Gateways for SIP trunks and
SRT listeners for SRT streams. The [GatewayClass](GATEWAYCLASS.md) and the [routes](ROUTE.md) have
pages of their own.

1. [Gateway](#gateway)
1. [SIPTrunk](#siptrunk) (beta)

## Gateway

Gateways describe the listeners exposed to clients.

The below Gateway resource will configure STUNner to open a STUN/TURN listener over the UDP port 3478 and make it available on a public IP address and port to clients. Each Gateway will have a [`stunnerd`](cmd/stunnerd.md) Deployment that will run the dataplane and a LoadBalancer Service that will expose the gateway to the Internet, both using the same name and namespace as the Gateway. Once the Gateway is removed, the corresponding resources are automatically garbage-collected.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: udp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: TURN-UDP
```

The below example defines two TURN listeners: a TURN listener at the UDP:3478 port that accepts routes from any namespace (see below), and a TURN listener at port TLS/TCP:443 that accepts routes only from namespaces labeled with `app=dev`.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: complex-gateway
  namespace: stunner
  annotations:
    stunner.l7mp.io/service-type: NodePort
    stunner.l7mp.io/enable-mixed-protocol-lb: "true"
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-port: "8086"
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-protocol: "http"
    service.beta.kubernetes.io/do-loadbalancer-healthcheck-path: "/live"
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: TURN-UDP
      allowedRoutes:
        namespaces:
          from: All
    - name: tls-listener
      port: 443
      protocol: TURN-TLS
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            namespace: stunner
            name: tls-secret
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels:
              app: dev
```

Below is a reference of the most important fields of the Gateway [`spec`](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects).

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `gatewayClassName` | `string` | The name of the GatewayClass that provides the root of the hierarchy the Gateway is attached to. | Yes |
| `listeners` | `list` | The list of listeners. | Yes |
| `addresses` | `list` | The list of manually hinted public addresses (only the first one is used).                                   | No |

> [!WARNING]
>
> Gateway resources are *not* safe for modification. This means that certain changes to a Gateway will restart the underlying TURN server listener, causing all active client sessions to terminate.  The particular rules are as follows:
> - adding or removing a listener will start/stop *only* the TURN listener being created/removed, without affecting the rest of the listeners on the same Gateway;
> - changing the transport protocol or port of an *existing* listener will restart the listener but leave the rest of the listeners intact; TLS keys and certificates apply to future connections without a restart;
> - changing the TURN authentication realm will restart *all* TURN servers.

Manually hinted public address describes an address that can be bound to a Gateway. It is defined by an address type, which can be either `IPAddress` (default) or `Hostname`, and an address value. Note that only the first address is used. Setting the `spec.addresses` field in the Gateway will enforce the use of that address all over STUNner as a public address for the gateway. If the address type specifies an IP address then that address will also be used in the Service created by STUNner to expose the Gateway as a [loadBalancerIP](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=non%20%27LoadBalancer%27%20type.-,loadBalancerIP,-string) and [externalIPs](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#service-v1-core:~:text=and%2Dservice%2Dproxies-,externalIPs,-string%20array).

| Field   | Type     | Description                                                   | Required |
|:--------|:--------:|:--------------------------------------------------------------|:--------:|
| `type`  | `string` | Type of the address, either `IPAddress` (default) or `Hostname`.  | Yes      |
| `value` | `string` | Address that should be bound to the Gateway's service.        | Yes      |

> [!WARNING]
>
> Be careful when using this feature. Since Kubernetes v1.24 the `loadBalancerIP` field is deprecated and it will be ignored if your Kubernetes install does not support the feature. In addition, the `externalIPs` field is denied by some cloud-providers.

### Listener configuration

Each `listener` is defined by a unique name, a protocol and a port. The protocol decides what the listener serves: a TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS listener is a TURN server over the respective transport, a plain UDP, TCP, TLS or DTLS listener is an L4 proxy that relays each client connection to a backend of its routes, choosing the backends in turn, and an SRT listener (beta) routes SRT streams over UDP to the backends of its routes by the stream they name. In addition, a `tls` configuration is required for TLS and DTLS listeners of both kinds. Per-listener configuration is as follows.

| Field | Type | Description | Required |
| :--- | :---: | :--- | :---: |
| `name` | `string` | Name of the listener. Must be unique per Gateway. | Yes |
| `port` | `int` | Network port for the listener. | Yes |
| `protocol` | `string` | Protocol of the listener: TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS for a TURN listener, UDP, TCP, TLS or DTLS for an L4 listener, SRT for an SRT listener. | Yes |
| `tls` | `object` | [TLS configuration](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig).| Yes (for TLS/DTLS listeners) |
| `allowedRoutes.namespaces.from` | `object` | [Route attachment policy](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.AllowedRoutes), either `All`, `Selector`, or `Same`. Default: `Same`. | No |

For TLS and DTLS listeners, `tls.mode` must be set to `Terminate` or omitted (STUNner terminates TLS, there is no `Passthrough`), and `tls.certificateRefs` must be a [reference to a Kubernetes Secret](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io%2fv1beta1.GatewayTLSConfig) of type `tls` or `opaque` with exactly two keys: `tls.crt` must hold the TLS PEM certificate and `tls.key` must hold the TLS PEM key. The certificate may come from anywhere. If you use cert-manager's Gateway API support (cert-manager v1.21 or later with `--enable-gateway-api`, DNS-01 challenges only), a TLS listener gets its certificate without further setup, while TURN-TLS and TURN-DTLS listeners need cert-manager to accept these protocols with `--gateway-api-extra-protocols=TURN-TLS,TURN-DTLS`. A DNS-01 certificate names a hostname, not the load balancer IP: set `spec.addresses: [{type: Hostname, value: <hostname>}]` on the Gateway so that its listeners advertise the hostname (e.g., `turns:<hostname>:<port>` in the ICE server list) and clients verify the certificate against it, and point a DNS record of the hostname at the load balancer.

### SIP Gateways

A Gateway whose `spec.infrastructure.parametersRef` names a [SIPTrunk](#siptrunk) is a SIP
gateway (beta): its media listener carries the RTP of every call and each other listener,
`UDP`, `TCP` or `TLS`, carries SIP. A Gateway whose `parametersRef` names a missing SIPTrunk, or a
kind STUNner does not support, is not accepted (reason `InvalidParameters`) and gets no dataplane.
See the [SIP gateway guide](SIP.md).

### SRT Gateways

A Gateway with an `SRT` listener is an SRT gateway (beta): every listener of the Gateway
must be `SRT`, a UDP port that takes SRT callers and places each stream on a backend of the
UDPRoutes attached to the listener, at the `port` of the backend reference. The publisher and the
readers of a stream reach the same backend. A Gateway that mixes `SRT` with other listener
protocols is not accepted (reason `ListenersNotValid`). Of more SRT Gateways than the tier serves
(one free, three member, any number enterprise), the oldest ones by creation time are served, the
others are not accepted (reason `LicenseLimit`). See the [SRT gateway
guide](SRT.md).

### Load balancer configuration

STUNner will automatically generate a Kubernetes LoadBalancer Service to expose each Gateway to clients. All listeners specified in the Gateway are wrapped by a single Service and will be assigned a single externally reachable IP address. If you want multiple listeners on different public IPs, create multiple Gateways. UDP, DTLS and SRT listeners are exposed as UDP services, TCP and TLS listeners as TCP.

STUNner implements several ways to customize the automatically created Service, each involving certain pre-defined [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations).  First, you can add global annotations to the `loadBalancerServiceAnnotations` field of the [GatewayConfig spec](GATEWAYCLASS.md#gatewayconfig), which affect the Service created for each Gateway that links to the GatewayConfig (via the GatewayClass `parametersRef`). To customize annotations on a per-gateway basis, you can also add specific annotations to the Gateway itself. Gateway annotations override the global annotations on conflict. Finally, you can also add custom labels/annotations to the automatically created STUNner Services manually, these are retained on Service update (unless there is a conflict). Note that labels/annotations added via the Gateway and the GatewayConfig are also propagated to the corresponding `stunnerd` Deployments.

You can also add labels to the automatically created Services using the `loadBalancerServiceLabels` field of the [GatewayConfig spec](GATEWAYCLASS.md#gatewayconfig). These labels go only to the Services, not to the `stunnerd` Deployments. They cannot override the labels set by STUNner itself, and invalid labels are ignored. Unlike annotations, labels removed from the GatewayConfig are also removed from the Services: STUNner keeps track of the labels it has added in the `stunner.l7mp.io/managed-labels` annotation and removes only those.

The following rules apply:
- Each annotation is copied verbatim into the Service created for any Gateway. This can be used, for instance, to specify health-check settings on the load-balancer Service (using the `service.beta.kubernetes.io/*-loadbalancer-healthcheck-*` annotations, see above).
- Annotations with the prefix `stunner.l7mp.io/...` have special meaning: apart from being copied into the Service these annotations also affect some specifics of the created Service, like the service type or the nodeports assigned to listeners.

STUNner defines the following special annotations:

1. **Service type:** The special annotation `stunner.l7mp.io/service-type` can be used to customize the type of the Service created by STUNner. The value can be either `ClusterIP`, `NodePort`, or `LoadBalancer` (this is the default); for instance, setting `stunner.l7mp.io/service-type: ClusterIP` will prevent STUNner from exposing a Gateway publicly (useful for testing).

1. **Mixed-protocol support:** Currently, STUNner limits each Gateway to a single transport protocol, e.g., UDP or TCP. This is intended to improve the consistency across the Kubernetes services of different cloud providers, which provide varying support for [mixed multi-protocol LoadBalancer Services](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types). If you still want to expose a UDP and a TCP port on the same IP using a single Gateway, add the annotation `stunner.l7mp.io/enable-mixed-protocol-lb: true` to the Gateway. Since mixed-protocol LBs are not supported in many popular Kubernetes offerings, STUNner currently defaults to disabling this feature.

   The below Gateway will expose both ports with their respective protocols.

   ```yaml
   apiVersion: gateway.networking.k8s.io/v1
   kind: Gateway
   metadata:
     name: mixed-protocol-gateway
     annotations:
       stunner.l7mp.io/enable-mixed-protocol-lb: "true"
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
       - name: udp-listener
         port: 3478
         protocol: TURN-UDP
       - name: tcp-listener
         port: 3479
         protocol: TURN-TCP
   ```

1. **Retaining the source IP:** Normally, Kubernetes load balancers apply source IP address translation when ingesting packets into the cluster. This replaces clients' original IP address with a private IP address. For STUNner's intended use case, as an ingress media gateway exposing the cluster's media services over the TURN protocol, this does not matter. However, STUNner can also act as a STUN server, which requires clients' source IP to be retained at the load balancer. This can be achieved by adding the annotation `stunner.l7mp.io/external-traffic-policy: local` to a Gateway, which will set the [`service.spec.externalTrafficPolicy`](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip) field in the Service created by STUNner for the Gateway to `Local`. Note that this Kubernetes feature comes with fairly complex [limitations](https://kubernetes.io/docs/tutorials/services/source-ip): if a STUN or TURN request hits a Kubernetes node that is not running a `stunnerd` pod, then the request will silently fail. This is required for Kubernetes to retain the client IP, which otherwise would be lost when passing packets between nodes. Use this setting at your own [risk](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#caveats-and-limitations-when-preserving-source-ips).

1. **Manually provisioning the dataplane:** In some cases it may be useful to manually provision a dataplane for a Gateway, e.g., to deploy `stunnerd` in a DaemonSet instead of a Deployment. Adding the annotation `stunner.l7mp.io/disable-managed-dataplane: true` to a Gateway will prevent STUNner from spawning a dataplane for the Gateway. This then allows one to manually create a `stunnerd` dataplane and connect it to the CDS server exposed by the operator to obtain the dataplane configuration. Remove the annotation to revert to the default mode and let STUNner manage the dataplane for the Gateway. Manual dataplane provisioning requires intimate knowledge of the STUNner internals, use this feature only if you know what you are doing.

1. **Selecting the NodePort:** By default, Kubernetes assigns a random external port from the range [30000-32767] to each listener of a Gateway exposed with a NodePort Service. This requires all ports in the [30000-32767] range to be opened on the external firewall, which may raise security concerns for hardened deployments. In order to assign specific nodeports to particular listeners, add the annotation `stunner.l7mp.io/nodeport:` `{listener_name_1:nodeport_1,listener_name_2:nodeport_2,...}` to the Gateway, where each key-value pair is a name of a listener and the selected (numeric) NodePort. The value itself must be a proper JSON map. Unknown listeners are silently ignored. Note that STUNner makes no specific effort to reconcile conflicting NodePorts: whenever the selected NodePort is unavailable Kubernetes will silently reject the Service, which can lead to hard-to-debug failures. Use this feature at your own risk.

1. **Selecting the target port:** Some hardened Kubernetes deployments prohibit containers from opening privileged ports (i.e., everything under 1024). This causes problems when one wants to ingest TURN over the standard TCP/TLS port 443. Kubernetes lets Services choose an arbitrary [target port](https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service) for each service port, which makes it possible to map a particular external port to an arbitrary (potentially non-privileged) port in the containers. In order to enforce a particular target port per listener, add the annotation `stunner.l7mp.io/targetport:` `{listener_name_1:targetport_1,...}` to the corresponding Gateway (the syntax and semantics are the same as those for the nodeport annotation). For instance, the below Gateway would expose the TURN TCP/TLS on port 443, but it would map the egress port to the target port 44321 in the STUNner dataplane container:

   ```yaml
   apiVersion: gateway.networking.k8s.io/v1
   kind: Gateway
   metadata:
     name: tls-gateway
     annotations:
       stunner.l7mp.io/targetport: "{\"tls-listener\":44321}"
   spec:
     gatewayClassName: stunner-gatewayclass
     listeners:
     - name: tls-listener
       port: 443
       protocol: TURN-TLS
       tls:
         certificateRefs:
           - kind: Secret
             namespace: stunner
             name: tls-secret
   ```

1. **Disabling the exposition of the health-check port:** Some older Kubernetes load-balancer providers required the exposition of the health-check port on LoadBalancer Services for UDP listeners to become externally reachable. Therefore, by default STUNner adds the health-check port (usually set via specific Gateway annotations) to the service-ports in automatically created LoadBalancer services. This has the unfortunate consequence that the health-check port becomes publicly reachable, which is considered a security issue by some, see https://github.com/l7mp/stunner-gateway-operator/issues/49. To prevent STUNner from exposing the health-check port, add the annotation `stunner.l7mp.io/disable-health-check-expose: true` to the corresponding Gateway. Note that this may make TURN/UDP listeners unreachable on the Gateway, so use this only if you know that this setting will work with your Kubernetes provider.

1. **Disabling session affinity:** By default STUNner applies the `sessionAffinity: ClientIP` setting on the LB services it creates to expose Gateways. Normally this setting improves stability by ensuring that each TURN session is safely pinned to the same dataplane pod for its entire lifetime. Certain hosted Kubernetes platforms, however, seem to reject UDP LB services that have this setting on, [breaking STUNner deployments](https://github.com/l7mp/stunner/issues/155) on these systems. In order to prevent STUNner from enforcing session affinity on the LB Service corresponding to a Gateway, just set the `stunner.l7mp.io/disable-session-affinity: true` annotation on the Gateway. Otherwise, session affinity is turned on.

The below table summarizes the Gateway annotations supported by STUNner.

| Key/value                                           | Description                                                                                                                                                | Default        |
|:----------------------------------------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------|:--------------:|
| `stunner.l7mp.io/service-type: <svc-type>`          | [Type of the Service](https://kubernetes.io/docs/concepts/services-networking/service) per Gateway, either `ClusterIP`, `NodePort`, or `LoadBalancer`.     | `LoadBalancer` |
| `stunner.l7mp.io/enable-mixed-protocol-lb: <bool>`  | [Mixed protocol load balancer service](https://kubernetes.io/docs/concepts/services-networking/service/#load-balancers-with-mixed-protocol-types) support. | False          |
| `stunner.l7mp.io/external-traffic-policy: <string>` | Set the value to `Local` to preserve clients' source IP at the load balancer.                                                                              | `Cluster`      |
| `stunner.l7mp.io/disable-managed-dataplane: <bool>` | Switch managed-dataplane support off for a Gateway.                                                                                                        | False          |
| `stunner.l7mp.io/nodeport: <map>`                   | Request a specific NodePort for particular listeners. Value is a JSON map of listener-nodeport key-value pairs.                                            | None           |
| `stunner.l7mp.io/targetport: <map>`                 | Request a specific target port for particular listeners. Value is a JSON map of listener-targetport key-value pairs.                                       | None           |
| `stunner.l7mp.io/disable-health-check-expose: true` | Disable the default exposition of the health-check port (if any).                                                                                          | False          |
| `stunner.l7mp.io/disable-session-affinity: true`    | Disable session affinity for a Gateway.                                                                                                                    | False          |

## SIPTrunk

> [!WARNING]
> SIPTrunk is in beta: its API may change in any release and there is no support on any tier. Please
> test and report issues, but in any way, use it at your own risk.

A SIPTrunk turns the Gateway that names it in `spec.infrastructure.parametersRef` into a SIP
gateway: it names the media listener of the Gateway and tells how the carriers connect, by the
source addresses of an IP-authenticated trunk, by the registrations of a registration-based one, or
both. It lives in the namespace of the Gateway. See the [SIP gateway guide](SIP.md) for examples.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: SIPTrunk
metadata:
  name: carriers
  namespace: voice
spec:
  mediaListener: media
  allowedSources:
    - 203.0.113.0/24
  registrations:
    - name: carrier
      registrar: "sips:sip.carrier.example"
      aor: "sip:+3612345678@sip.carrier.example"
      authRef:
        name: carrier-digest
```

| Field | Type | Description | Required |
|:--|:--|:--|:--|
| `mediaListener` | `string` | The name of the Gateway listener carrying the media, a `UDP` listener. Every other listener of the Gateway carries SIP. | Yes |
| `allowedSources` | `[]string` | The address prefixes (or addresses) the SIP listeners admit calls from. Empty admits no inbound calls, which suits registration-only trunks. | No |
| `registrations` | `[]object` | The registrations kept up with registration-based trunks. | No |
| `registrations[].name` | `string` | The name of the registration, unique in the SIPTrunk. | Yes |
| `registrations[].registrar` | `string` | The SIP URI of the carrier's registrar, as in Kamailio or Asterisk: `sip:host[:port][;transport=udp\|tcp\|tls]`, or `sips:host[:port]` for TLS; a bare `host[:port]` is a `sip:` URI. The transport defaults to UDP (TLS for `sips:`), the port to 5060 (5061 over TLS). | Yes |
| `registrations[].aor` | `string` | The address of record to register, a `sip:` or `sips:` URI. | Yes |
| `registrations[].authRef` | `object` | A Secret in the namespace of the SIPTrunk with the `username` and `password` keys, answering the registrar's digest challenge. | No |
| `registrations[].expires` | `string` | The registration lifetime STUNner asks for. Default is `600s`. | No |

The SIP listeners of the Gateway admit calls only from `allowedSources`, and the calls go to the
backends of the UDPRoutes attached to the SIP listeners; the UDPRoutes attached to the media
listener list the media peers. A TLS registration verifies the registrar's certificate against the
system roots and the host of `registrar`.
