# STUNner demo: Magic mirror with Kurento

This is the Kurento [Magic
mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
demo, adopted for STUNner and Kubernetes. The demo shows a basic WebRTC loopback server with some
media processing added: the application uses computer vision and augmented reality techniques to
add a funny hat on top of faces.

The demo contains a fully featured WebRTC application server, developed using the express framework
for Node.js, and the standard Kurento media server for processing audio/video media. In the
original demo, both the application server and the media server are expected to be reachable via a
public routable IP address. This demo was adopted here for using with STUNner, so that it can be
installed into a standard Kubernetes cluster.

In this demo you will learn the following steps to:
* integrate a typical WebRTC application server to be used with STUNner,
* deploy the modified application server into a Kubernetes,
* deploy the Kurento media server into Kubernetes behind STUNner,
* secure a STUNner deployment, and
* scale a standard WebRTC workload using Kubernetes and STUNner.

## Installation

### Prerequisites

Consult the [STUNner installation and configuration guide](../../doc/INSTALL.md) to set up STUNner.

### Quick installation

The simplest way to deploy the demo is to clone the [STUNner git
repository](https://github.com/l7mp/stunner) and deploy the
[manifest](kurento-magic-mirror.yaml) packaged with STUNner.

```console
$ git clone https://github.com/l7mp/stunner
$ cd stunner
$ kubectl apply -f examples/kurento-magic-mirror/kurento-magic-mirror.yaml
```

The demo exposes a publicly available HTTPS web service on port 8443. Kubernetes assigns an
ephemeral public IP address to the web service, so first we need to learn the external IP.

```console
$ kubectl get service webrtc-server -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

The result should be a valid IP address in the form `A.B.C.D`. If no IP address is returned, wait a
bit more until Kubernetes successfully assigns the external IP. Then, direct your browser to the
URL `https://<A.B.C.D>:8443` (of course, make sure substitute the previous IP address), accept the
self-signed certificate, register some user name, and you can immediately enjoy the demo.

### Scaling

This demo uses the AI/ML based computer vision features built into Kubernetes to process media. As
such, it is fairly hard on the media server CPU. Thanks to STUNner, the media server pool can be
simply (auto-)scaled with Kubernetes effortlessly.

The below command will scale the Kurento media server pool to 4 instances and again, automatic
health-checks and load-balancing should just work as expected.

```console
$ kubectl scale deployment kms --replicas=4
```

## Help

STUNner development is coordinated in Discord, send [us](../../AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Demo adopted from [Kurento](https://www.kurento.org).
