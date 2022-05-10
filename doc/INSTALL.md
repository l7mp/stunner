# Installation

STUNner comes with prefab deployment manifests to fire up a fully functional STUNner-based WebRTC
media gateway in minutes. Note that the default deployment does not contain an application server
and a media server: STUNner in itself is not a WebRTC backend, it is just an *enabler* for you to
deploy your *own* WebRTC infrastructure into Kubernetes and make sure your media servers are still
reachable for WebRTC clients, despite running with a private IP address inside a Kubernetes pod.

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
to block malicious access; see [details](#access-control) later.

### TL;DR

With a minimal understanding of WebRTC and Kubernetes, deploying STUNner should not take more than
10 minutes.

* [Customize STUNner and deploy it](#installation) into your Kubernetes cluster and [expose
  it](#learning-the-external-ip-and-port) over a public IP address and port.
* Optionally [deploy a WebRTC media server](examples/kurento-one2one-call) into Kubernetes as well.
* [Set STUNner as the ICE server](#configuring-webbrtc-clients-to-reach-stunner) in your WebRTC
  clients.
* ...
* Profit!!

### Configuration

The STUNner installation will create the below Kubernetes resources in the cluster:
1. a `ConfigMap` that stores STUNner local configuration,
2. a `Deployment` running one or more STUNner daemon replicas,
3. a `LoadBalancer` service to expose the STUNner deployment on a public IP address and UDP port
   (by default, the port is UDP 3478), and finally
4. a `NetworkPolicy`, i.e., an ACL/firewall policy to control network communication from STUNner to
   the rest of the cluster.

The installation scripts packaged with STUNner will use hard-coded configuration defaults that must
be customized prior to deployment. In particular, make absolutely sure to customize the access
tokens (`STUNNER_REALM`, `STUNNER_USERNAME` and `STUNNER_PASSWORD`), otherwise STUNner will use
hard-coded STUN/TURN credentials. This should not pose a major security risk (see the [STUNner
Security Guide](doc/SECURITY.md) for more info on security), but it is still safer to customize the
access tokens before exposing STUNner to the Internet.

STUNner configuration is available in the Kubernetes `ConfigMap` named `stunner-config`. The most
important settings are as follows.
* `STUNNER_PUBLIC_ADDR` (no default): The public IP address clients can use to reach STUNner. When
  configuring [WebRTC clients with a TURN server URI](configuring-webrtc-clients-to-reach-stunner),
  this setting specifies the IP address in the TURN server URI.  By default, the public IP address
  will be dynamically assigned during installation.  The STUNner installation scripts take care of
  querying the external IP address from Kubernetes and automatically setting `STUNNER_PUBLIC_ADDR`;
  for manual installation the external IP must be set by hand (see
  [details](#learning-the-external-ip-and-port) below).
* `STUNNER_PUBLIC_PORT` (default: 3478): The public port used by clients to reach STUNner. When
  configuring [WebRTC clients with a TURN server URI](configuring-webrtc-clients-to-reach-stunner),
  this setting specifies the port in the TURN server URI.  Note that the Helm installation scripts
  may overwrite this configuration if the installation falls back to the `NodePort` service (i.e.,
  when STUNner fails to obtain an external IP from the Kubernetes ingress load balancer), see
  [details](#learning-the-external-ip-and-port) below.
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
  mechanism with the share secret `$STUNNER_SECRET`.
* `STUNNER_USERNAME` (default: `user`): the
  [username](https://www.rfc-editor.org/rfc/rfc8489.html#section-14.3) attribute clients can use to
  authenticate with STUNner over plain-text authentication. Make sure to customize!
* `STUNNER_PASSWORD` (default: `pass`): the password clients can use to authenticate with STUNner
   over plain-text authentication. Make sure to customize!
* `STUNNER_SHARED_SECRET`: the shared secret used for [STUN/TURN long-term
  credential](https://www.rfc-editor.org/rfc/rfc8489.html#section-9.2).
* `STUNNER_LOGLEVEL` (default: `all:WARN`): the default log level used by the STUNner daemons.
* `STUNNER_MIN_PORT` (default: 10000): smallest relay transport port assigned by STUNner. 
* `STUNNER_MAX_PORT` (default: 20000): highest relay transport port assigned by STUNner. 

The default configurations can be overridden by setting custom command line arguments when
launching the STUNner pods. All examples below assume that STUNner is deployed into the `default`
namespace; see the installation notes below on how to override this.

### Installation

STUNner supports two installation options: a self-contained and easy-to-use Helm chart and a manual
installation method using static Kubernetes manifests.

#### Helm

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
custom namespace  for installing STUNner; note that the namespace must exist when
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

#### Manual installation

If Helm is not an option, you can perform a manual installation using the static Kubernetes
manifests packaged with STUNner. This mode is not recommended for general use however, since the
static Kubernetes manifests do not provide the same flexibility and automatization as the Helm
charts.

First, clone the STUNner repository.

```console
$ git clone https://github.com/l7mp/stunner.git
$ cd stunner
```

Then, customize the default settings in the STUNner service [manifest](examples/stunner.yaml) and
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
but the default installation scripts do not support this.) WebRTC clients will need to learn
STUNner's external IP and port somehow; this is outside the scope of STUNner, but see our
[examples](#examples) for a way to communicate the STUN/TURN URI and port back to WebRTC clients
during user registration.

In order to simplify the integration of STUNner into the WebRTC application server, STUNner stores
the dynamic IP address/port assigned by Kubernetes into the `ConfigMap` named `stunner-config`
under the key `STUNNER_PUBLIC_IP` and `STUNNER_PUBLIC_PORT`. Then, WebRTC applications can map this
`ConfigMap` as environment variables and communicate the IP address and port back to the clients
(see an [example](configuring-webrtc-clients-to-reach-stunner) below).

The [Helm installation](#helm) scripts take care of setting the IP address and port automatically
in the `ConfigMap`. However, when using the [manual installation](#manual-installation) option the
external IP address and port will need to be handled manually during installation. The below
instructions simplify this process.

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
$ until [ -n "$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')" ]; do sleep 1; done
```

If this hangs for minutes, then your load-balancer integration is not working (if using
[Minikube](https://github.com/kubernetes/minikube), make sure `minikube tunnel` is
[running](https://minikube.sigs.k8s.io/docs/handbook/accessing)). In this case, skip the next step
and proceed to configure STUNner external reachability using the `NodePort` service. Otherwise,
query the public IP address and port used by STUNner from Kubernetes.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get svc stunner -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -o jsonpath='{.spec.ports[0].port}')
```

If Kubernetes fails to assign an external IP address for the `stunner` service, the service would
still be reachable externally via the `NodePort` automatically assigned by Kubernetes. In this case
(but only in this case!), set the IP address and port from the NodePort:

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get svc stunner -o jsonpath='{.spec.ports[0].nodePort}')
```

Check that the external IP address `$STUNNER_PUBLIC_ADDR` is reachable by your WebRTC clients: some
Kubernetes clusters (like GKE Autopilot) are installed with private node IP addresses.

If all goes well, the STUNner service is now exposed on the IP address `$STUNNER_PUBLIC_ADDR` and
UDP port `$STUNNER_PUBLIC_PORT`. Finally, store back the public IP address and port into STUNner's
configuration, so that the WebRTC application server can learn this information and forward it to
the clients.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_PUBLIC_ADDR\":\"${STUNNER_PUBLIC_ADDR}\",\"STUNNER_PUBLIC_PORT\":\"${STUNNER_PUBLIC_PORT}\"}}"
```


## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
