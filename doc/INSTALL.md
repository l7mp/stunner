# Installation

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server: STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes. STUNner will then make sure your media
servers are still reachable for WebRTC clients, despite running with a private IP address inside a
Kubernetes pod.

## Table of Contents
1. [Prerequisites](#prerequisites)
2. [Configuration](#configuration)
3. [Installation](#installation)
4. [Learning the external IP and port](#learning-the-external-ip-and-port)
5. [Configuring WebRTC clients](configuring-webrtc-clients)
6. [Enabling TURN transport over TCP](#enabling-turn-transport-over-tcp)

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
risk (see the [STUNner security guide](/doc/SECURITY.md#access-control) for more info), but it is
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
  generation](/doc/AUTH.mc) mechanism use this configuration parameter to customize
  username/password lifetime.
* `STUNNER_LOGLEVEL` (default: `all:WARN`): the default log level used by the STUNner daemons.
* `STUNNER_MIN_PORT` (default: 10000): smallest relay transport port assigned by STUNner. 
* `STUNNER_MAX_PORT` (default: 20000): highest relay transport port assigned by STUNner. 

The default configuration can be overridden by setting custom command line arguments when
[launching the STUNner pods](/cmd/stunnerd/README.md). All examples below assume that STUNner is
deployed into the `default` namespace; see the installation notes below on how to override this.

## Installation

STUNner supports two installation options: a self-contained and easy-to-use Helm chart and a manual
installation method using static Kubernetes manifests.

### Helm

The simplest way to deploy STUNner is through [Helm](https://helm.sh). In this case, all STUNner
configuration parameters are available for customization as [Helm
Values](https://helm.sh/docs/chart_template_guide/values_files).

```console
$ helm repo add stunner https://l7mp.io/stunner
$ helm repo update
$ helm install stunner stunner/stunner
```
To customize your chart overwrite the [default
values](https://github.com/l7mp/stunner/blob/main/helm/stunner/values.yaml). The below will set
custom namespace for installing STUNner; note that the namespace must exist when
installing STUNner.

```console
$ kubectl create namespace <insert-namespace-name-here>
$ helm install stunner stunner/stunner --set stunner.namespace=<insert-namespace-name-here>
```

The following will set a custom [STUN/TURN
realm](https://datatracker.ietf.org/doc/html/rfc8656#section-2).

```console
$ helm install stunner stunner/stunner --set stunner.config.STUNNER_REALM=<your-realm-here>
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

Then, customize the default settings in the STUNner service [manifest](/deploy/manifests/stunner.yaml)
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
one video call with Kurento via STUNner](examples/kurento-one2one-call) demo for a solution to
communicate the STUN/TURN URI and port back to WebRTC clients during user registration.

In order to simplify the integration of STUNner into the WebRTC application server, STUNner stores
the dynamic IP address/port assigned by Kubernetes into the `stunner-config` `ConfigMap` under the
key `STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC application pods can reach this
`ConfigMap` as environment variables and communicate the IP address and port back to the clients
(see an [example](configuring-webrtc-clients) below).

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

## Enabling TCP TURN transport

Some corporate firewalls block all UDP access from the private network, except DNS. To make sure
that clients can still reach STUNner from locked-down private networks, you can expose STUNner over
a [TCP-based TURN transport]([RFC 6062](https://tools.ietf.org/html/rfc6062)). 

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

If using a media-server, don't forget to open up the [STUNner ACL](/doc/SECURITY#access-control) so
that STUNner can reach the media server pool over TCP.

Finally, direct your clients to the re-exposed STUNner TCP service with the below `PeerConnection` configuration; don't
forget to rewrite the  TURN transport to TCP by adding the query `transport=tcp` to the
STUNner URI.
```javascript
var ICE_config = {
  'iceServers': [
    {
      'url': "turn:<STUNNER_PUBLIC_ADDR>:<STUNNER_PUBLIC_PORT>?transport=tcp',
      'username': <STUNNER_USERNAME>,
      'credential': <STUNNER_PASSWORD>,
    },
  ],
};
var pc = new RTCPeerConnection(ICE_config);
```

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
