# STUNner Tutorial: Call a SIP echo service in Kubernetes

*Beta: no support on any tier. Please test and report issues, but in any way, use it at your own
risk.*

This tutorial connects a SIP phone on your machine through a STUNner SIP Gateway to a SIP echo
service running in Kubernetes: STUNner proxies the SIP signaling and relays the RTP media, the
echo service answers the call and sends the media back, and `mediactl` counts what comes
back. It is the smallest end-to-end test of the beta [SIP gateway](../../SIP.md), the
same path a carrier's calls take into your SIP server.

In this tutorial you will learn how to:
* deploy a SIP service into Kubernetes,
* make a STUNner Gateway a SIP gateway with a SIPTrunk,
* call the service through STUNner with the `mediactl` tool.

## Prerequisites

* A STUNner installation from the official Helm chart, see the [installation
  guide](../../INSTALL.md).
* A load balancer that preserves the source address of the traffic it forwards
  (`externalTrafficPolicy: Local`): the managed load balancers of the large cloud providers do, and
  so does [MetalLB](https://metallb.io) on a local [kind](https://kind.sigs.k8s.io) cluster.
* A machine to run `mediactl` on whose address the cluster sees unchanged: a machine with a
  public IP address, or your own machine calling a local kind cluster. STUNner matches the media
  of a call to the address in its SDP, so a phone behind a NAT that rewrites its source
  address gets no media back (carriers do not sit behind such NATs, which is why STUNner can rely
  on this).
* The `mediactl` tool, a media source and sink that also makes SIP calls: download it from the
  [STUNner releases](https://github.com/l7mp/stunner/releases) or install it with `go install
  github.com/l7mp/stunner/v2/cmd/mediactl@latest`.

## Configuration

1. Deploy the SIP echo service: the STUNner `mediactl` image, answering every
   call on UDP port 5060 and echoing the media.

   ```console
   cd stunner
   kubectl apply -f docs/examples/sip-echo/sip-echo-server.yaml
   ```

1. Find the address your calls come from, as the cluster sees it: on a local kind cluster this
   is the address of your machine on the kind network (`docker network inspect kind`), otherwise
   your public address (for instance `curl -4 ifconfig.me`).

   ```console
   export MY_IP=$(curl -s -4 ifconfig.me)
   ```

1. Create the SIP Gateway. The manifest holds the GatewayClass and the GatewayConfig of the other
   tutorials, a Gateway with a SIP listener on UDP port 5060 and a media listener on UDP port
   30000, a SIPTrunk that admits calls only from your address, and a UDPRoute to the echo service.
   The Gateway names the SIPTrunk in `spec.infrastructure.parametersRef`, which makes it a SIP
   gateway; the `stunner.l7mp.io/external-traffic-policy: local` annotation keeps the source
   addresses intact.

   ```console
   kubectl create namespace stunner
   sed "s/MY_IP/${MY_IP}/" docs/examples/sip-echo/sip-echo-stunner.yaml | kubectl apply -f -
   ```

1. Check that the Gateway is accepted and has a public address.

   ```console
   kubectl -n stunner get gateway sip-gateway
   ```

   The `PROGRAMMED` column should read `True` and `ADDRESS` should hold the load balancer address.
   A Gateway that is not accepted says why in its status: `kubectl -n stunner get gateway
   sip-gateway -o yaml`.

## Test

Call the echo service for ten seconds, letting `mediactl` find the address of the Gateway through
your kubeconfig:

```console
mediactl sip call -t 10s k8s://stunner/sip-gateway/sip
```

Without a kubeconfig, call the address of the Gateway: `mediactl sip call -t 10s
sip:echo@<address>:5060`.

`mediactl` logs the connected call, sends audio for ten seconds and hangs up, logging what it
sent and what came back:

```console
mediactl INFO: connected call ...
mediactl INFO: call ... ended (local hangup): sent=500 received=500 lost=0 rtcp-sent=... rtcp-received=...
```

A `received` count close to `sent` and no loss means the media made the round trip through
STUNner. You can watch the call in the STUNner dataplane, while it runs:

```console
stunnerctl -n stunner status sip-gateway
```

## Troubleshooting

* **The call gets no answer.** The SIP listener admits only the addresses of `allowedSources`.
  Check that `MY_IP` is the address the cluster sees, and that the load balancer preserves it (the
  `stunner.l7mp.io/external-traffic-policy: local` annotation).
* **The call connects but `received` stays zero.** The media is matched to the address in the
  SDP of `mediactl`: a NAT on the way that rewrites the source address breaks the match. Run
  `mediactl` from a machine with a public address, or from the network of a local kind cluster.
* **The Gateway is not accepted.** The open-source operator supports no SIPTrunk: install the
  official STUNner chart. The free tier serves one SIP Gateway at a time (the member tier
  three); the oldest ones by creation time win.

## Clean up

```console
kubectl delete -f docs/examples/sip-echo/sip-echo-server.yaml
sed "s/MY_IP/${MY_IP}/" docs/examples/sip-echo/sip-echo-stunner.yaml | kubectl delete -f -
```

## Next steps

To connect a real carrier, follow the [SIP gateway guide](../../SIP.md): an IP-authenticated
trunk looks like the SIPTrunk of this tutorial with the carrier's address ranges in
`allowedSources`, and a registration-based trunk adds a registration with the carrier's
credentials.
