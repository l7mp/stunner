# Concepts

This guide describes STUNner's architecture and the most important components of an operational installation.

## Architecture

A STUNner installation consists of two parts, a *control plane* and a *data plane*. The control
plane consists of declarative policies specifying the way STUNner should route media traffic to the
media servers, plus a gateway operator that renders the high-level policies into an actual
dataplane configuration. The data plane in turn comprises one or more `stunnerd` pods, which are
responsible for actually ingesting media traffic into the cluster. The dataplane pods are
automatically provisioned by the gateway operator so they should come and go as you add and remove
STUNner gateways.

![STUNner architecture](img/stunner_arch_big.svg)

## Control plane

The STUNner control plane consists of the following components:

* **Gateway API resources:** The high-level STUNner configuration is a collection of [Gateway API](https://gateway-api.sigs.k8s.io) resources that together describe the way media traffic should enter the cluster. The anchor of the configuration hierarchy is the GatewayClass object, and the rest of the resources form a complete hierarchy underneath it: the GatewayConfig describes general STUNner configuration, Gateways define the port and transport protocol per each TURN server listener, and UDPRoutes point to the backend services client traffic should be forwarded to. See the reference of the [GatewayClass](GATEWAYCLASS.md), the [Gateway](GATEWAY.md) and the [routes](ROUTE.md).

* **Gateway operator:** The main purpose of the gateway operator is to watch Gateway API resources and, once a Gateway API resource is added or modified by the user, update the dataplane accordingly (see below).

* **STUNner authentication service** (not shown on the figure): The auth service is an ancillary service that can be used to generate TURN credentials and complete [ICE server configurations](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection#iceservers) to bootstrap clients. See more info [here](AUTH.md).

## Dataplane

The STUNner dataplane is comprised of a fleet of `stunnerd` pods implementing the media gateway services clients connect to (TURN servers for WebRTC, plain L4 relays, and the beta SIP proxy and RTP relay), plus some additional configuration to expose these services to clients. The complete dataplane configuration per each Gateway is as follows:

* **`stunnerd` Deployment:** Once you create a new Gateway the gateway operator will spawn a new
  dataplane for the Gateway automatically. For each Gateway there will be `stunnerd` Deployment
  with the same name and namespace. The `stunnerd` daemon builds its services from three kinds of
  objects: a *listener* per Gateway listener accepts clients' connections and hands them to the
  *server* behind it (a TURN server implemented on top of the
  [pion/turn](https://github.com/pion/turn) framework for a TURN listener, an L4 relay for a plain
  UDP/TCP transport listener, or a SIP proxy or an RTP relay for a SIP Gateway), and a *cluster*
  per route reaches the backend services (e.g., the media servers), with some ancillary
  administrative and authentication mechanisms in place to check client credentials, logging, etc.
  Whenever you modify a Gateway (UDPRoute), the gateway operator renders a new dataplane
  configuration with the modified listener (cluster, respectively) specs and downloads it to the
  `stunnerd` pods, which in turn reconcile their internal state with respect the new configuration.
  You are free to scale the dataplane to as many `stunnerd` pods as you wish: Kubernetes will make
  sure that new client connections are distributed evenly over the scaled-out STUNner dataplane.

* **LoadBalancer Service:** STUNner creates a separate LoadBalancer Service per each Gateway to expose the TURN listeners of the `stunnerd` pods to the outside world. Similarly to the case of the `stunnerd` Deployment, there will be a separate LoadBalancer Service per each Gateway with the same name and namespace. 
