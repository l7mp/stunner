# Standalone mode

In order to gain full control over media ingestion, STUNner can be deployed without the gateway
operator component. In this standalone mode, the user is fully in charge of creating and
maintaining the configuration of the `stunnerd` pods. With the introduction of the STUNner gateway
operator *the standalone mode is considered obsolete* as of STUNner v0.11. The below documentation
is provided only for historical reference; before the gateway operator existed this was *the*
recommended way to interact with STUNner.

## Table of contents

* [Prerequisites](#prerequisites)
* [Installation](#installation)
* [Configuration](#configuration)
* [Learning the external IP and port](#learning-the-external-IP-and-port)
* [Configuring WebRTC clients](#configuring-WebRTC-clients)
* [Authentication](#authentication)
* [Access control](#access-control)
* [Enabling TURN transport over TCP](#enabling-turn-transport-over-tcp) 
* [Enabling TURN transport over TLS and DTLS](#enabling-turn-transport-over-tls-and-dtls)

## Prerequisites

The below installation instructions require an operational cluster running a supported version of
Kubernetes (>1.22). Make sure that the cluster comes with a functional [load-balancer
integration](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer),
otherwise STUNner will not be able to allocate a public IP address for clients to reach your WebRTC
infra. In the standalone mode STUNner relies on Kubernetes ACLs (`NetworkPolicy`) with [port
ranges](https://kubernetes.io/docs/concepts/services-networking/network-policies/#targeting-a-range-of-ports)
to block malicious access; make sure your Kubernetes installation supports these.

## Installation

### Installation with Helm

Use the [Helm charts](https://github.com/l7mp/stunner-helm) for installing STUNner, setting the
`standalone.enabled` feature gate to `true`:

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner stunner/stunner --set stunner.standalone.enabled=true
```

The below wiill create a new namespace named `stunner` and install the STUNner dataplane pods into that
namespace. 

```console
helm install stunner stunner/stunner --set stunner.standalone.enabled=true --create-namespace --namespace=stunner
```

Note that we do not install the usual control plane: in this mode we ourselves need to manually
provide the dataplane configuration for STUNner.

### Manual installation

If Helm is not an option, you can perform a manual installation using the static Kubernetes
manifests packaged with STUNner.

First, clone the STUNner repository.

```console
git clone https://github.com/l7mp/stunner.git
cd stunner
```

Then, customize the default settings in the STUNner service
[manifest](/deploy/manifests/stunner-standalone.yaml) and deploy it via `kubectl`.

```console
kubectl apply -f deploy/manifests/stunner-standalone.yaml
```

By default, all resources are created in the `default` namespace.

## Configuration

The default STUNner installation will create the below Kubernetes resources:
1. a ConfigMap that stores STUNner local configuration,
2. a Deployment running one or more STUNner daemon replicas,
3. a LoadBalancer service to expose the STUNner deployment on a public IP address and UDP port
   (by default, the port is UDP 3478), and finally
4. a NetworkPolicy, i.e., an ACL/firewall policy to control network communication from STUNner to
   the rest of the Kubernetes workload.

The installation scripts packaged with STUNner will use hard-coded configuration defaults that must
be customized prior to deployment. In particular, make absolutely sure to customize the access
tokens (`STUNNER_USERNAME` and `STUNNER_PASSWORD` for `plaintext` authentication, and
`STUNNER_SHARED_SECRET` and possibly `STUNNER_DURATION` for the `longterm` authentication mode),
otherwise STUNner will use hard-coded STUN/TURN credentials. This should not pose a major security
risk (see [here](/doc/SECURITY.md) for more info), but it is still safer to customize the access
tokens before exposing STUNner to the Internet.

The most recent STUNner configuration is always available in the Kubernetes ConfigMap named
`stunnerd-config`. This configuration is made available to the `stunnerd` pods by
[mapping](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#define-container-environment-variables-using-configmap-data)
the `stunnerd-config` ConfigMap into the pods as environment variables. Note that changes to this
ConfigMap will take effect only once STUNner is restarted.

The most important STUNner configuration settings are as follows.
* `STUNNER_PUBLIC_ADDR` (no default): The public IP address clients can use to reach STUNner.  By
  default, the public IP address will be dynamically assigned during installation. The installation
  scripts take care of querying the external IP address from Kubernetes and automatically setting
  `STUNNER_PUBLIC_ADDR`; for manual installation the external IP must be set by hand (see
  [details](#learning-the-external-ip-and-port) below).
* `STUNNER_PUBLIC_PORT` (default: 3478): The public port used by clients to reach STUNner. Note
  that the Helm installation scripts may overwrite this configuration if the installation falls
  back to the `NodePort` service (i.e., when STUNner fails to obtain an external IP from the
  Kubernetes ingress load balancer), see [details](#learning-the-external-ip-and-port) below.
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
  mechanism with the secret `$STUNNER_SHARED_SECRET`.
* `STUNNER_USERNAME` (default: `user`): the
  [username](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.3) attribute clients can use to
  authenticate with STUNner over `plaintext` authentication. Make sure to customize!
* `STUNNER_PASSWORD` (default: `pass`): the password clients can use to authenticate with STUNner
  in `plaintext` authentication. Make sure to customize!
* `STUNNER_SHARED_SECRET`: the shared secret used for `longterm` authentication mode.  Make sure to
  customize!
* `STUNNER_DURATION` (default: `86400` sec, i.e., one day): the lifetime of STUNner credentials in
  `longterm` authentication.
* `STUNNER_LOGLEVEL` (default: `all:WARN`): the default log level used by the STUNner daemons.
* `STUNNER_MIN_PORT` (default: 10000): smallest relay transport port assigned by STUNner.
* `STUNNER_MAX_PORT` (default: 20000): highest relay transport port assigned by STUNner.

The default configuration can be overridden by setting custom command line arguments when
[launching the STUNner pods](/cmd/stunnerd/README.md). All examples below assume that STUNner is
deployed into the `default` namespace; see the installation notes below on how to override this.

Note that changing in the configuration values becomes valid only once STUNner is restarted (see
below).

## Learning the external IP and port

There are two ways to expose the STUN/TURN ingress gateway service with STUNner: through a standard
Kubernetes [`LoadBalancer`
service](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) (the
default) or as a [`NodePort`
service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport), used as a
fallback if an ingress load-balancer is not available. In both cases the external IP address and
port that WebRTC clients can use to reach STUNner may be set dynamically by Kubernetes. (Kubernetes
lets you use your own [fix IP address and domain
name](https://kubernetes.io/docs/concepts/services-networking/service/#choosing-your-own-ip-address),
but the default installation scripts do not support this.) 

In general, WebRTC clients will need to learn STUNner's external IP and port somehow. In order to
simplify the integration of STUNner into the WebRTC application server, STUNner stores the dynamic
IP address/port assigned by Kubernetes into the `stunnerd-config` ConfigMap under the key
`STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC application pods can map this ConfigMap
as environment variables and communicate the IP address and port back to the clients (see an
[example](#configuring-webrtc-clients) below).

The [Helm installation](#helm) scripts should take care of setting the IP address and port
automatically in the ConfigMap during installation. However, if later the LoadBalancer services
change for some reason then the new external IP address and port will need to be configured
manually in the ConfigMap. Similar is the case when using the static Kubernetes manifests to deploy
STUNner. The below instructions simplify this process.

After a successful installation, you should see something similar to the below:

```console
kubectl get all
NAME                               READY   STATUS    RESTARTS   AGE
pod/stunner-XXXXXXXXXX-YYYYY       1/1     Running   0          8s

NAME                            TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)          AGE
service/kubernetes              ClusterIP      10.72.128.1     <none>        443/TCP          6d4h
service/stunner                 ClusterIP      10.72.130.61    <none>        3478/UDP         81s
service/stunner-standalone-lb   LoadBalancer   10.72.128.166   A.B.C.D       3478:30630/UDP   81s

NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/stunner       1/1     1            1           8s
```

Note the external IP address allocated by Kubernetes for the `stunner-standalone-lb` service
(`EXTERNAL-IP` marked with a placeholder `A.B.C.D` in the above): this will be the public STUN/TURN
access point that your WebRTC clients will need to use in order to access the WebRTC media service
via STUNner.

Wait until Kubernetes assigns a valid external IP to STUNner and query the public IP address and
port used by STUNner from Kubernetes.

```console
until [ -n "$(kubectl get svc stunner-standalone-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner-standalone-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner-standalone-lb -o jsonpath='{.spec.ports[0].port}')
```

If this hangs for minutes, then your Kubernetes load-balancer integration is not working (if using
[Minikube](https://github.com/kubernetes/minikube), make sure `minikube tunnel` is
[running](https://minikube.sigs.k8s.io/docs/handbook/accessing)). This may still allow STUNner to
be reached externally, using a Kubernetes `NodePort` service (provided that your [Kubernetes
supports
NodePorts](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-overview#no_direct_external_inbound_connections_for_private_clusters)). In
this case, but only in this case!, set the IP address and port from the NodePort:

```console
export STUNNER_PUBLIC_ADDR=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner-standalone-lb -o jsonpath='{.spec.ports[0].nodePort}')
```

Check that the IP address/port `${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT}` is reachable by your
WebRTC clients; some Kubernetes clusters are installed with private node IP addresses that may
prevent NodePort services to be reachable from the Internet.

If all goes well, the STUNner service is now exposed on the IP address `$STUNNER_PUBLIC_ADDR` and
UDP port `$STUNNER_PUBLIC_PORT`. Finally, store the public IP address and port back into STUNner's
configuration, so that the WebRTC application server can learn this information and forward it to
the clients.

```console
kubectl patch configmap/stunnerd-config --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```

## Configuring WebRTC clients

The last step is to configure your WebRTC clients to use STUNner as the TURN server.  The below
JavaScript snippet will direct WebRTC clients to use STUNner; make sure to substitute the
placeholders (like `<STUNNER_PUBLIC_ADDR>`) with the correct configuration from the above.

```javascript
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=udp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

## Authentication

STUNner relies on the STUN [long-term credential
mechanism](https://www.rfc-editor.org/rfc/rfc8489.html#page-26) to provide user authentication. See
[here](/doc/AUTH.md) for more detail on STUNner's authentication modes.

The below commands will configure STUNner to use `plaintext` authentication using the
username/password pair `my-user/my-password` and restart STUNner for the new configuration to take
effect.

```console
kubectl patch configmap/stunnerd-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"plaintext\",\"STUNNER_USERNAME\":\"my-user\",\"STUNNER_PASSWORD\":\"my-password\"}}"
kubectl rollout restart deployment/stunner
```

The below commands will configure STUNner to use `longterm` authentication mode, using the shared
secret `my-secret`. By default, STUNner credentials are valid for one day.

```console
kubectl patch configmap/stunnerd-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"longterm\",\"STUNNER_SHARED_SECRET\":\"my-secret\"}}"
kubectl rollout restart deployment/stunner
```

## Access control

The security risks and best practices associated with STUNner are described
[here](/doc/SECURITY.md), below we summarize the only step that is specific to the standalone mode:
configuring access control.

By default, a standalone STUNner installation comes with an open route: this essentially means
that, possessing a valid TURN credential, an attacker can reach *any* UDP service inside the
Kubernetes cluster via STUNner. This is because, without an operator, there is no control plane to
supply [endpoint-discovery
service](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/service_discovery#endpoint-discovery-service-eds)
for the dataplane and therefore `stunnerd` does not know whether the peer address a client wished
to reach belongs to the legitimate backend service or not. In order to prevent open access through
STUNner, the default standalone installation comes with a default-deny Kubernetes NetworkPolicy
that locks down *all* access from the STUNner pods to the rest of the workload.

```yaml
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
```

In order for clients to reach a media server pod via STUNner the user must explicitly whitelist the
target service in this access control rule.  Suppose that we want STUNner to reach the media server
pods labeled as `app=media-server` over the UDP port range `[10000:20000]`, but we don't want
connections via STUNner to succeed to any other pod. This will be enough to support WebRTC media,
but will not allow clients to, e.g., reach the Kubernetes DNS service.

Assuming that the entire workload is deployed into the `default` namespace, the below
`NetworkPolicy` ensures that all access from any STUNner pod to any media server pod is allowed
over any UDP port between 10000 and 20000, and all other network access from STUNner is denied.

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

If your Kubernetes CNIs does not support [network policies with port
ranges](https://kubernetes.io/docs/concepts/services-networking/network-policies/#targeting-a-range-of-ports),
then the below will provide an access control rule similar to the above, except that it opens up
*all* UDP ports on the media server instead of limiting access to the UDP port range
`[10000:20000]`.

```yaml
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
```

## Enabling TURN transport over TCP

Some corporate firewalls block all UDP access from the private network, except DNS. To make sure
that clients can still reach STUNner, you can expose STUNner over a [TCP-based TURN
transport](https://www.rfc-editor.org/rfc/rfc6062). To maximize the chances of getting through a
zealous firewall, below we expose STUNner over the default HTTPS port 443.

First, enable TURN transport over TCP in STUNner.

```console
kubectl patch configmap/stunnerd-config --type merge -p "{\"data\":{\"STUNNER_TRANSPORT_TCP_ENABLE\":\"1\"}}"
```

Then, delete the default Kubernetes service that exposes STUNner over UDP and re-expose it over the
TCP port 443.
```console
kubectl delete service stunner-standalone-lb
kubectl expose deployment stunner-standalone-lb --protocol=TCP --port=443 --type=LoadBalancer
```

Wait until Kubernetes assigns a public IP address.
```console
until [ -n "$(kubectl get svc stunner-standalone-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner-standalone-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner-standalone-lb -o jsonpath='{.spec.ports[0].port}')
kubectl patch configmap/stunnerd-config --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```

Restart STUNner with the new configuration.
```console
kubectl rollout restart deployment/stunner
```

Finally, direct your clients to the re-exposed STUNner TCP service with the below `PeerConnection` configuration; don't
forget to rewrite the  TURN transport to TCP by adding the query `transport=tcp` to the
STUNner URI.
```javascript
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=tcp",
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

## Enabling TURN transport over TLS and DTLS

The ultimate tool to work around aggressive firewalls and middleboxes is exposing STUNner via TLS
and/or DTLS. Fixing the TLS listener port at 443 will make it impossible for the corporate firewall
to block TURN/TLS connections without blocking all external HTTPS access, so most probably at least
the TCP/443 port will be open to encrypted connections.

Start with a fresh Kubernetes install. Below we create a self-signed certificate for testing; make
sure to replace the cert/key pair below with your own trusted credentials.

```console
openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=example.domain.com"
kubectl create secret tls stunner-tls --key /tmp/tls.key --cert /tmp/tls.crt
```

Deploy a STUNner gateway in standalone mode with the pre-configured static manifest.
```console
cd stunner/
kubectl apply -f deploy/manifests/stunner-standalone-tls.yaml
```

This will fire up STUNner with two TURN listeners, a TLS/TCP and a DTLS/UDP listener, both at port
443, and create two LoadBalancer services to expose these to clients.

Wait until Kubernetes assigns a public IP address and learn the new public addresses.
```console
until [ -n "$(kubectl get svc stunner-tls -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
until [ -n "$(kubectl get svc stunner-dtls -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
export STUNNER_PUBLIC_ADDR_TLS=$(kubectl get svc stunner-tls -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export STUNNER_PUBLIC_ADDR_DTLS=$(kubectl get svc stunner-dtls -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

Check your configuration with the handy [`turncat`](/cmd/turncat) utility and the [UDP
greeter](/README.md#testing) service. First, query the UDP greeter service via TLS/TCP. Here, the
`turncat` command line argument `-i` puts `turncat` into insecure mode in order to accept our
self-signed TURN sever TLS certificate.

```console
cd stunner
go build -o turncat cmd/turncat/main.go
kubectl apply -f deploy/manifests/udp-greeter.yaml
export PEER_IP=$(kubectl get svc media-plane -o jsonpath='{.spec.clusterIP}')
export STUNNER_USERNAME=$(kubectl get cm stunner-config -o yaml -o jsonpath='{.data.STUNNER_USERNAME}')
export STUNNER_PASSWORD=$(kubectl get cm stunner-config -o yaml -o jsonpath='{.data.STUNNER_PASSWORD}')
./turncat -i - turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR_TLS}:443?transport=tls udp://${PEER_IP}:9001
Hello STUNner via TLS
Greetings from STUNner!
```

Type anything once `turncat` is running to receive a nice greeting from STUNner. DTLS/UDP should
also work fine:

```console
./turncat -i - turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR_DTLS}:443?transport=dtls udp://${PEER_IP}:9001
Another hello STUNner, now via DTLS!
Greetings from STUNner!
```

Remember, you can always direct your clients to your TURN listeners by setting the TURN URIs in the
ICE server configuration on your `PeerConnection`s.

```javascript
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR_TLS>:443?transport=tls",
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR_DTLS>:443?transport=dtls",
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

Note that the default Kubernetes manifest
['stunner-standalone-tls.yaml'](/deploy/manifests/stunner-standalone-tls.yaml) opens up the
NetworkPolicy for the `media-plane/default` service only, make sure to configure this to your own
setup.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

