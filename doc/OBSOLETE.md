# Standalone mode (obsolete)

In order to gain full control over media ingestion, STUNner can be deployed without the
gateway-operator component. In this standalone mode the user is fully in charge of creating and
maintaining the configuration of the `stunnerd` pods.


# Installation

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server: STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes. STUNner will then make sure your media
servers are still reachable for WebRTC clients, despite running with a private IP address inside a
Kubernetes pod.

## Table of Contents
- [Installation](#installation)
  - [Table of Contents](#table-of-contents)
  - [Prerequisites](#prerequisites)
  - [Configuration](#configuration)
  - [Installation](#installation-1)
    - [Helm](#helm)
    - [Manual installation](#manual-installation)
  - [Learning the external IP and port](#learning-the-external-ip-and-port)
  - [Configuring WebRTC clients](#configuring-webrtc-clients)
  - [Enabling TURN transport over TCP](#enabling-turn-transport-over-tcp)
  - [Help](#help)
  - [License](#license)
  - [Acknowledgments](#acknowledgments)

## Prerequisites

The below installation instructions require an operational cluster running a supported version of
Kubernetes (>1.22). You can use any supported platform, for example
[Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) or any
[hosted](https://cloud.google.com/kubernetes-engine) or private Kubernetes cluster, but make sure
that the cluster comes with a functional [load-balancer
integration](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) (all
major hosted Kubernetes services should support this, and even Minikube
[provides](https://minikube.sigs.k8s.io/docs/handbook/accessing) standard `LoadBalancer` service
access). Otherwise, STUNner will not be able to allocate a public IP address for clients to reach
your WebRTC infra. In addition, STUNner relies on Kubernetes ACLs (`NetworkPolicy`) with [port
ranges](https://kubernetes.io/docs/concepts/services-networking/network-policies/#targeting-a-range-of-ports)
to block malicious access; see details in the [STUNner security
guide](/doc/SECURITY.md#access-control).

## Configuration

The default STUNner installation will create the below Kubernetes resources in the cluster:
1. a `ConfigMap` that stores STUNner local configuration,
2. a `Deployment` running one or more STUNner daemon replicas,
3. a `LoadBalancer` service to expose the STUNner deployment on a public IP address and UDP port
   (by default, the port is UDP 3478), and finally
4. a `NetworkPolicy`, i.e., an ACL/firewall policy to control network communication from STUNner to
   the rest of the cluster.

The installation scripts packaged with STUNner will use hard-coded configuration defaults that must
be customized prior to deployment. In particular, make absolutely sure to customize the access
tokens (`STUNNER_USERNAME` and `STUNNER_PASSWORD` for `plaintext` authentication, and
`STUNNER_SHARED_SECRET` and possibly `STUNNER_DURATION` for the `longterm` authentication mode),
otherwise STUNner will use hard-coded STUN/TURN credentials. This should not pose a major security
risk (see the [STUNner security guide](SECURITY.md#access-control) for more info), but it is
still safer to customize the access tokens before exposing STUNner to the Internet.

The most recent STUNner configuration is always available in the Kubernetes `ConfigMap` named
`stunner-config`. You can make all STUNner configuration parameters available to pods by
[mapping](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#define-container-environment-variables-using-configmap-data)
the `stunner-config` `ConfigMap` into the pod as environment variables. Note that any change to
this `ConfigMap` will take effect only once STUNner is restarted.

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
   over `plaintext` authentication. Make sure to customize!
* `STUNNER_SHARED_SECRET`: the shared secret used for `longterm` authentication mode.  Make sure to
  customize!
* `STUNNER_DURATION` (default: `86400` sec, i.e., one day): the lifetime of STUNner credentials in
  `longterm` authentication. Not used by STUNner directly, but the [long-term credential
  generation](/doc/AUTH.md) mechanism use this configuration parameter to customize
  username/password lifetime.
* `STUNNER_LOGLEVEL` (default: `all:WARN`): the default log level used by the STUNner daemons.
* `STUNNER_MIN_PORT` (default: 10000): smallest relay transport port assigned by STUNner.
* `STUNNER_MAX_PORT` (default: 20000): highest relay transport port assigned by STUNner.

The default configuration can be overridden by setting custom command line arguments when
[launching the STUNner pods](../cmd/stunnerd/README.md). All examples below assume that STUNner is
deployed into the `default` namespace; see the installation notes below on how to override this.

## Installation

STUNner supports two installation options: a self-contained and easy-to-use Helm chart and a manual
installation method using static Kubernetes manifests.

### Helm

The simplest way to deploy STUNner is through [Helm](https://helm.sh). In this case, all STUNner
configuration parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files). Find out more about the charts in the [STUNner-helm repository](https://github.com/l7mp/stunner-helm).

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update

helm install stunner-gateway-operator stunner/stunner-gateway-operator

helm install stunner stunner/stunner
```
The above will install both charts into the default namespace.
To customize your chart overwrite the [default
values](https://github.com/l7mp/stunner-helm/blob/main/helm/stunner/values.yaml). The below will set
custom namespace for installing STUNner; note that the `--create-namespace --namespace=<your-namespace>`
options will create the release namespace if not present which means there is no need for a pre-existing namespace.

```console
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace=<your-namespace>

helm install stunner stunner/stunner --create-namespace --namespace=<your-namespace>
```

The following will set a custom [STUN/TURN
realm](https://datatracker.ietf.org/doc/html/rfc8656#section-2). (This will apply only in the case of the [Standalone](https://github.com/l7mp/stunner-helm#without-the-operator-in-standalone-mode) mode.)

```console
helm install stunner stunner/stunner --set stunner.standalone.config.STUNNER_REALM=<your-realm-here>
```

### Manual installation

If Helm is not an option, you can perform a manual installation using the static Kubernetes
manifests packaged with STUNner. This mode is not recommended for general use however, since the
static Kubernetes manifests do not provide the same flexibility and automatization as the Helm
charts.

First, clone the STUNner repository.

```console
$ git clone https://github.com/l7mp/stunner.git
$ cd stunner
```

Then, customize the default settings in the STUNner service [manifest](../deploy/manifests/stunner.yaml)
and deploy it via `kubectl`.

```console
$ kubectl apply -f deploy/manifests/stunner.yaml
```
By default, all resources are created in the `default` namespace.

## Learning the external IP and port

There are two ways to expose the STUN/TURN ingress gateway service with STUNner: through a standard
Kubernetes [`LoadBalancer`
service](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) (the
default) or as a [`NodePort`
service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport), used as a
fallback if an ingress load-balancer is not available. In both cases the external IP address and
port that WebRTC clients can use to reach STUNner may be set dynamically by Kubernetes. (Of course,
Kubernetes lets you use your own [fix IP address and domain
name](https://kubernetes.io/docs/concepts/services-networking/service/#choosing-your-own-ip-address),
but the default installation scripts do not support this.) WebRTC clients will need to learn
STUNner's external IP and port somehow; this is outside the scope of STUNner; but see the [One to
one video call with Kurento via STUNner](/examples/kurento-one2one-call) demo for a solution to
communicate the STUN/TURN URI and port back to WebRTC clients during user registration.

In order to simplify the integration of STUNner into the WebRTC application server, STUNner stores
the dynamic IP address/port assigned by Kubernetes into the `stunner-config` `ConfigMap` under the
key `STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC application pods can reach this
`ConfigMap` as environment variables and communicate the IP address and port back to the clients
(see an [example](#configuring-webrtc-clients) below).

The [Helm installation](#helm) scripts take care of setting the IP address and port automatically
in the `ConfigMap`. However, when using the [manual installation](#manual-installation) option the
external IP address and port will need to be handled manually. The below instructions simplify this
process.

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
```

Note the external IP address allocated by Kubernetes for the `stunner` service (`EXTERNAL-IP`
marked with a placeholder `A.B.C.D` in the above): this will be the public STUN/TURN access point
that your WebRTC clients will need to use in order to access the WebRTC media service through
STUNner.

Wait until Kubernetes assigns a valid external IP to STUNner and query the public IP address and
port used by STUNner from Kubernetes.

```console
$ until [ -n "$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
$ export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -o jsonpath='{.spec.ports[0].port}')
```

If this hangs for minutes, then your Kubernetes load-balancer integration is not working (if using
[Minikube](https://github.com/kubernetes/minikube), make sure `minikube tunnel` is
[running](https://minikube.sigs.k8s.io/docs/handbook/accessing)). This may still allow STUNner to
be reached externally, using a Kubernetes `NodePort` service (provided that your [Kubernetes
supports
NodePorts](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-overview#no_direct_external_inbound_connections_for_private_clusters)). In
this case, but only in this case!, set the IP address and port from the NodePort:

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -o jsonpath='{.spec.ports[0].nodePort}')
```

Check that the IP address/port `${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT}` is reachable by your
WebRTC clients; some Kubernetes clusters are installed with private node IP addresses that may
prevent NodePort services to be reachable from the Internet.

If all goes well, the STUNner service is now exposed on the IP address `$STUNNER_PUBLIC_ADDR` and
UDP port `$STUNNER_PUBLIC_PORT`. Finally, store back the public IP address and port into STUNner's
configuration, so that the WebRTC application server can learn this information and forward it to
the clients.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```

## Configuring WebRTC clients

The last step is to configure your WebRTC clients to use STUNner as the TURN server.  The below
JavaScript snippet will then direct your WebRTC clients to use STUNner; make sure to substitute the
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

Note that STUNner comes with a [small Node.js
library]https://www.npmjs.com/package/@l7mp/stunner-auth-lib) that makes it simpler dealing with
ICE configurations and STUNner credentials in the application server.

## Enabling TURN transport over TCP

Some corporate firewalls block all UDP access from the private network, except DNS. To make sure
that clients can still reach STUNner from locked-down private networks, you can expose STUNner over
a [TCP-based TURN transport]([RFC 6062](https://www.rfc-editor.org/rfc/rfc6062)).

To maximize the chances of getting through an over-zealous firewall, below we expose STUNner over
the default HTTPS port 443.

First, enable TURN transport over TCP in STUNner.

```console
$ kubectl patch configmap/stunner-config --type merge -p "{\"data\":{\"STUNNER_TRANSPORT_TCP_ENABLE\":\"1\"}}"
```

Then, delete the default Kubernetes service that exposes STUNner over UDP and re-expose it over the
TCP port 443.
```console
$ delete service stunner
$ kubectl expose deployment stunner --protocol=TCP --port=443 --type=LoadBalancer
```

Wait until Kubernetes assigns a public IP address.
```console
$ until [ -n "$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
$ export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -o jsonpath='{.spec.ports[0].port}')
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```

Restart STUNner with the new configuration.
```console
$ kubectl rollout restart deployment/stunner
```

If using a media-server, don't forget to open up the [STUNner ACL](SECURITY.md#access-control) so
that STUNner can reach the media server pool over TCP.

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










# Authentication

STUNner provides secure access to the WebRTC infrastructure deployed into Kubernetes. STUNner uses
the IETF TURN protocol to ingest media traffic into the Kubernetes cluster, which, [by
design](https://datatracker.ietf.org/doc/html/rfc5766#section-17), provides comprehensive
security. In particular, STUNner provides message integrity and, if configured with the TLS/TCP or
DTLS/UDP listeners, complete confidentiality. To complete the CIA triad, the this guide shows how
to add user authentication to STUNner.

## Table of Contents
- [Authentication](#authentication)
  - [Table of Contents](#table-of-contents)
  - [The long-term credential mechanism](#the-long-term-credential-mechanism)
  - [STUNner authentication workflow](#stunner-authentication-workflow)
  - [Plaintext authentication](#plaintext-authentication)
  - [Longterm authentication](#longterm-authentication)
  - [Help](#help)
  - [License](#license)
  - [Acknowledgments](#acknowledgments)

## The long-term credential mechanism

STUNner relies on the STUN [long-term credential
mechanism](https://www.rfc-editor.org/rfc/rfc8489.html#page-26) to provide user authentication.

The long-term credential mechanism assumes that prior to the communication, STUNner and the WebRTC
clients agree on a username and password to be used for authentication.  The credential is
considered long-term since it is assumed that it is provisioned for a user and remains in effect
until the user is no longer a subscriber of the system (`plaintext` authentication), or until the
predefined lifetime of the username/password pair passes and the credential expires (`longterm`
authentication).

STUNner secures the authentication process against replay attacks using a digest challenge.  In
this mechanism, the server sends the user a realm (used to guide the user or agent in selection of
a username and password) and a nonce.  The nonce provides replay protection.  The client also
includes a message-integrity attribute in the authentication message, which provides an HMAC over
the entire request, including the nonce.  The server validates the nonce and checks the message
integrity.  If they match, the request is authenticated, otherwise the server rejects the request.

## STUNner authentication workflow

The intended authentication workflow in STUNner is as follows.

1. *A username/password pair is generated.* This is outside the scope of STUNner; however, STUNner
   comes with a [small Node.js library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib) that
   makes it simpler to generate STUNner credentials. For instance, the below will generate a
   username/password pair and a realm based on the current STUNner configuration.
   ```javascript
   const StunnerAuth = require('@l7mp/stunner-auth-lib');

   var credentials = StunnerAuth.getStunnerCredentials();
   ```
2. *The clients and STUNner gateway exchange a username/password pair over a secure channel.* The
   easiest way is to encode the username/password pair used for STUNner in the [ICE
   server configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer) returned to
   clients. E.g., using the above [Node.js
   library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib):
   ```javascript
   const StunnerAuth = require('@l7mp/stunner-auth-lib');
   ...
   var ICE_config = StunnerAuth.getIceConfig({
     address: '1.2.3.4',            // ovveride STUNNER_PUBLIC_ADDR
     port: 3478,                    // ovveride STUNNER_PUBLIC_PORT
     auth_type: 'plaintext',        // override STUNNER_AUTH_TYPE
     username: 'my-user',           // override STUNNER_USERNAME
     password: 'my-password',       // override STUNNER_PASSWORD
     ice_transport_policy: 'relay', // override STUNNER_ICE_TRANSPORT_POLICY
   });
   console.log(ICE_config);
   ```
   Output:
   ```javascript
   {
     iceServers: [
       {
         url: 'turn://1.2.3.4:3478?transport=udp',
         username: 'my-user',
         credential: 'my-password'
       }
     ],
     iceTransportPolicy: 'relay'
   }
   ```

   In the [Magic mirror via STUNner](../examples/kurento-magic-mirror/README.md) demo the ICE server
   configuration is generated and patched into the static Javascript code served to users on
   startup (this is suitable for STUNner's `plaintext` authentication), while the [One to one video
   call with Kurento via STUNner](examples/kurento-one2one-call) demo generates the STUNner
   credentials dynamically, during user registration, and returns the full ICE server configuration
   to the clients in the "register response" message (this workflow is usable for dynamic
   credential generation using the `longterm` authentication mode).
3. *WebRTC clients are configured with the STUNner authentication credentials.* The below snippet
   shows how to initialize a WebRTC
   [`PeerConnection`](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection)
   to use the above ICE server configuration in order to use STUNner as the default TURN service.
   ```javascript
   var ICE_config = <Read ICE configuration send by the application server>
   var pc = new RTCPeerConnection(ICE_config);
   ```

## Plaintext authentication

In STUNner, `plaintext` authentication is the simplest and least secure authentication mode,
basically corresponding to a traditional "log-in" username and password pair given to users. Note
that only a single username/password pair is used for *all* clients. This makes configuration very
easy; e.g., the ICE server configuration can be written into the static Javascript code served to
clients. At the same time, `plaintext` authentication is the least secure mode: once an attacker
learns a `plaintext` STUNner credential they can use it without limits to reach STUNner (until the
administrator rolls the credetials, see below).

The below commands will configure STUNner to use `plaintext` authentication using the
username/password pair `my-user/my-password` and restart STUNner for the new configuration to take
effect.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"plaintext\",\"STUNNER_USERNAME\":\"my-user\",\"STUNNER_PASSWORD\":\"my-password\"}}"
$ kubectl rollout restart deployment/stunner
```

The term `plaintext` may be deceptive: the password is never exchanged in plain text between the
client and STUNner. However, since the WebRTC Javascript API uses the TURN credentials unencrypted,
an attacker can easily extract the STUNner credentials from the client-side Javascript code.

In order to mitigate this risk, it is a good security practice to reset the username/password pair
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
that the WebRTC application server may need to be restarted as well, in order to learn the new
STUNner credentials.

## Longterm authentication

STUNner's `longterm` authentication mode provides clients time-limited access to STUNner.  STUNner
`longterm` credentials are dynamically generated with a pre-configured lifetime and, once the
lifetime expires, the credential cannot be used to authenticate (or refresh) with STUNner any
more. This authentication mode is more secure since credentials are not shared between clients and
come with a limited validity. Configuring `longterm` authentication in STUNner may be more complex
though, since credentials must be dynamically generated for each session and properly
returned to clients.

STUNner adopts the [`longterm` authentication
mechanism](https://pkg.go.dev/github.com/pion/turn/v2#GenerateLongTermCredentials) from [Pion
TURN](https://pkg.go.dev/github.com/pion/turn/v2). In particular, the username is a UNIX timestamp
specifying the time at with the credential expires, and the password is a base-64 encoded string
obtained by SHA-hashing the timestamp with a predefined shared secret. The advantage of this
mechanism is that it is enough to know the shared secret for STUNner to be able to check the
validity of a credential.

The below commands will configure STUNner to use `longterm` authentication, using the shared secret
`my-secret`. By default, STUNner credentials are valid for one day.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"longterm\",\"STUNNER_SHARED_SECRET\":\"my-secret\"}}"
$ kubectl rollout restart deployment/stunner
```

STUNner's [authentication helper library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib)
will be able to correctly read the configuration from the `stunner-config` `ConfigMap` and use the
appropriate credential generators to create new username/password pairs.
```javascript
var cred = StunnerAuth.getStunnerCredentials({
    auth_type: 'longterm',   // override STUNNER_AUTH_TYPE
    secret: 'my-secret',     // override STUNNER_SHARED_SECRET
    duration: 24 * 60 * 60,  // lifetime the longterm credential is effective
});
```




Like any conventional gateway service, an improperly configured STUNner service may easily end up
exposing sensitive services to the Internet. The below security guidelines will allow to minmize
the risks associated with a misconfigured STUNner.

## Table of Contents
- [Security](#security)
  - [Table of Contents](#table-of-contents)
  - [Threat](#threat)
  - [Locking down STUNner](#locking-down-stunner)
  - [Authentication](#authentication)
  - [Access control](#access-control)
  - [Exposing internal IP addresses](#exposing-internal-ip-addresses)
  - [Help](#help)
  - [License](#license)
  - [Acknowledgments](#acknowledgments)

## Threat

Before deploying STUNner, it is worth evaluating the potential [security
risks](https://www.rtcsec.com/article/slack-webrtc-turn-compromise-and-bug-bounty) a poorly
configured public STUN/TURN server poses.  To demonstrate the risks, below we shall use the
[`turncat`](../cmd/turncat) utility to reach the Kubernetes DNS service through a misconfigured
STUNner gateway.

Start with a fresh STUNner installation. As usual, we store the STUNner configuration for later
use.

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
$ go run cmd/turncat/main.go --realm $STUNNER_REALM --user ${STUNNER_USERNAME}=${STUNNER_PASSWORD} \
  --log=all:TRACE udp:127.0.0.1:5000 turn:${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp:${KUBE_DNS_IP}:53
```

Now, in another terminal try to query the Kubernetes DNS service through the `turncat` tunnel for
the internal service address allocated by Kubernetes for STUNner:

```console
$ dig +short @127.0.0.1 -p 5000 stunner.default.svc.cluster.local
```

If all goes well, this should hang until `dig` times out. This is because the [default installation
scripts block *all* communication](#access-control) from STUNner to the rest of the workload, and
the default-deny ACL needs to be explicitly opened up for STUNner to be able to reach a specific
service. To demonstrate the risk of an improperly configured STUNner gateway, we temporarily allow
STUNner to access the Kube DNS service.

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

## Locking down STUNner

By default, the Kubernetes workload should be isolated from STUNner with a default-deny ACL (but
see the below [security notice](#access-control) on access control).

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

In summary, unless properly locked down, STUNner may be used maliciously to open a tunnel to any
service running inside a Kubernetes cluster. Accordingly, it is critical to tightly control the
pods and services inside a cluster exposed via STUNner, using a properly configured Kubernetes ACL
(`NetworkPolicy`).

The below security considerations will greatly reduce the attack surface associated with
STUNner. Overall, **a properly configured STUNner deployment will present exactly the same attack
surface as a WebRTC infrastructure hosted on a public IP address** (possibly behind a firewall). In
any case, use STUNner at your own risk.

## Authentication

By default, STUNner uses a single statically set username/password pair for all clients and the
password is available in plain text at the clients (`plaintext` authentication mode). Anyone with
access to the static STUNner credentials can open a UDP tunnel to any service inside the Kubernetes
cluster, unless [blocked](#access-control) by a properly configured Kubernetes `NetworkPolicy`.

For more security sensitive workloads, we recommend the `longterm` authentication mode, which uses
per-client fixed lifetime username/password pairs. See the [STUNner authentication
guide](doc/AUTH.md) for configuring STUNner with the more secure `longterm` authentication mode.

## Access control

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
[reach the Kubernetes DNS service](#threat).

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
publicly; e.g., the [`turncat` utility](../cmd/turncat) packaged with STUNner can be used
conveniently for this [purpose](../examples/simple-tunnel/README.md).

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
option for you.




## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).

