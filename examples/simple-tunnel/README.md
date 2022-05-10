# STUNner testing: Opening a UDP tunnel via STUNner

This introductory demo shows how to tunnel an external connection via STUNner to a UDP service
deployed into Kubernetes. The demo can be used to quickly check a STUNner installation.

In this demo you will learn how to:
* configure a UDP echo service in Kubernetes,
* use the [`turncat`](/utils/turncat) utility to connect to STUNner,
* secure the STUNner deployment,
* connect a local UDP sender to the echo service running in Kubernetes via STUNner, and
* test a STUNner installation with [`turncat`](/utils/turncat).

## Installation

### Prerequisites

You need to have a Kubernetes cluster (>1.22), and the `kubectl` command-line tool must be
configured to communicate with your cluster. If you do not already have a cluster, you can create
one by using [minikube](https://minikube.sigs.k8s.io/docs/start). Furthermore, make sure that
STUNner is deployed into the cluster (see the [STUNner configuration
guide](/README.md#configuration) and the [STUNner installation
guide](/README.md#installation)). The below examples assume that STUNner has been installed into a
default namespace with simple plain text authentication. The demo requires a solid understanding of
the basic concepts in [Kubernetes](https://kubernetes.io/docs/home) and
[WebRTC](https://webrtc.org/getting-started/overview). It is good idea to start with setting up the
original [Kurento One to one video
call](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-one2one.html) demo
locally, in order to understand how the Kubernetes based demo differs (very little).

### Setup

STUNner comes with a simple STUN/TURN client called [`turncat`](/utils/turncat) that can be used to
test a STUNner installation. The `turncat` client will open a UDP tunnel through STUNner into the
Kubernetes cluster, which can be used to access any UDP service running inside the cluster (unless
blocked by a [properly configured](/README.md#security) `NetworkPolicy`). Note that your WebRTC
clients will not need `turncat` to reach the cluster, since all Web browsers come with a STUN/TURN
client included; `turncat` here is used only to simulate what a WebRTC client would do when trying
to reach STUNner. For more info, see the `turncat` [documentation](/utils/turncat).

In this demo we test the STUNner installation by deploying a UDP echo server into the cluster and
exposing it for external access via STUNner.

![STUNner test setup](/doc/stunner_echo.svg)

### Configuration

First, make sure STUNner is using plain-text authentication:
```console
$ kubectl get cm stunner-config -o jsonpath="{.data.STUNNER_AUTH_TYPE}"
```

If the output is `plaintext` you're good to go. Otherwise, consult the [STUNner Authentication
Guide](doc/AUTH.md) on how to restart STUNner with plain-text authentication.

Create a `Deployment` called `udp-echo` containing only a single pod and make this pod available
over the UDP port 9001 as a cluster-internal service with the same name. Use everyone's favorite
network debugging tool, [`socat(1)`](https://linux.die.net/man/1/socat), to deploy a simple UDP
server into the pod. Any message sent to the UDP server will result the response `Greetings from
STUNner!`

```console
$ kubectl create deployment udp-echo --image=l7mp/net-debug:latest
$ kubectl expose deployment udp-echo --name=udp-echo --type=ClusterIP --protocol=UDP --port=9001
$ kubectl exec -it $(kubectl get pod -l app=udp-echo -o jsonpath="{.items[0].metadata.name}") -- \
    socat -d -d udp-l:9001,fork EXEC:"echo Greetings from STUNner!"
```

Store the STUN/TURN configurations and credentials for later use.

```console
$ export STUNNER_PUBLIC_ADDR=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_ADDR}')
$ export STUNNER_PUBLIC_PORT=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_PORT}')
$ export STUNNER_REALM=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_REALM}')
$ export STUNNER_USERNAME=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_USERNAME}')
$ export STUNNER_PASSWORD=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PASSWORD}')
```

Learn the virtual IP address (`ClusterIP`) assigned by Kubernetes to the `udp-echo` service:

```console
$ export UDP_ECHO_IP=$(kubectl get svc udp-echo -o jsonpath='{.spec.clusterIP}')
```

Observe that the result is a private IP address: indeed, the `udp-echo` service is not available to
external services at this point. We shall use STUNner to expose the service to the Internet via a
TURN service.

### Security

The default installation scripts install an ACL into Kubernetes that blocks *all* communication
from STUNner to the rest of the workload. This is to minimize the risk of an improperly configured
STUNner gateway to [expose sensitive services to the external world](doc/SECURITY.md). In order to
allow STUNner to open transport relay connections to the `udp-echo` service, we have to explicitly
open up this ACL first.

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
    - podSelector:
        matchLabels:
          app: udp-echo
    ports:
    - protocol: UDP
      port: 9001
EOF
```

### Test

Finally fire up `turncat` locally; this will open a UDP server port on `localhost:9000` and tunnel
all packets to the `udp-echo` service in your Kubernetes cluster through STUNner.

```console
$ cd stunner
$ go run utils/turncat/main.go --realm $STUNNER_REALM --user ${STUNNER_USERNAME}=${STUNNER_PASSWORD} \
  --log=all:TRACE udp:127.0.0.1:9000 turn:${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp:${UDP_ECHO_IP}:9001
```

Now, in another terminal open a UDP connection through the tunnel opened by `turncat` and send
something to the UDP echo server running inside the cluster.

```console
$ echo "Hello STUNner" | socat - udp:localhost:9000
```

If all goes well, you should see the message `Greetings from STUNner!` echoed back from the
cluster. 

### Cleaning up

First, make sure to lock down the ACL to the [default-deny rule](locking-down-STUNner):

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

Then, exit `turncat` and delete the resources created for the test.

```console
$ kubectl delete deployment udp-echo
$ kubectl delete service udp-echo
```

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.
