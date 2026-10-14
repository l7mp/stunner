# Free vs. premium tiers

STUNner comes in three tiers. The open-source **free tier** suits simple WebRTC applications. The paid **member tier** adds the premium features targeted for small and medium scale operations, while the fully featured **enterprise tier** is optimized for large-scale deployments. See [here](https://l7mp.io/pricing) for up-to-date info on how to purchase a license.

## Table of Contents

1. [Feature comparison](#feature-comparison)
1. [Running without a license](#running-without-a-license)
1. [Upgrading to a premium tier](#upgrading-to-a-premium-tier)

## Feature comparison

All tiers share the same core: the premium tiers run the same STUNner, with additional features unlocked by a license.

| Feature                                                                    | Free | Member | Enterprise |
|:---------------------------------------------------------------------------|:----:|:------:|:----------:|
| [Gateway API](GATEWAY.md) configuration via Gateways and UDPRoutes          | ✓    | ✓      | ✓          |
| TURN over UDP, TCP, TLS and DTLS, and L4 [listeners](GATEWAY.md#gateway)    | ✓    | ✓      | ✓          |
| [Static and ephemeral authentication](AUTH.md)                             | ✓    | ✓      | ✓          |
| [Horizontal scaling](SCALING.md) and [monitoring](MONITORING.md)            | ✓    | ✓      | ✓          |
| [`stunnerctl`](cmd/stunnerctl.md), [`stunnerd`](cmd/stunnerd.md) and [`icetester`](cmd/icetester.md) | ✓ | ✓ | ✓ |
| [User quota](PREMIUM_REFERENCE.md#user-quota)                              |      | ✓      | ✓          |
| [STUN server mode](PREMIUM_REFERENCE.md#stun-server-mode)                  |      | ✓      | ✓          |
| [Deploying into a DaemonSet](PREMIUM_REFERENCE.md#deploying-into-a-daemonset) |   | ✓      | ✓          |
| [Relay address discovery](PREMIUM_REFERENCE.md#relay-address-discovery)    |      | ✓      | ✓          |
| [TCP routes](PREMIUM_REFERENCE.md#tcp-routes)                              |      | ✓      | ✓          |
| [Dual-stack TURN](PREMIUM_REFERENCE.md#dual-stack-turn)                    |      | ✓      | ✓          |
| [High-availability operator](PREMIUM_REFERENCE.md#high-availability-operator) |   | ✓      | ✓          |
| [Post-quantum TLS](PREMIUM_REFERENCE.md#post-quantum-tls)                  |      | ✓      | ✓          |
| [TURN offload](PREMIUM_REFERENCE.md#turn-offload) (Linux/eBPF acceleration) |     |        | ✓          |

See the [premium feature reference](PREMIUM_REFERENCE.md) for the details of each premium feature.

## Running without a license

The free tier needs no license. Premium features remain locked until a license that includes them is validated, and some of them fall back to the free tier behavior:

- TCP routes: the operator still accepts your TCPRoutes and maintains their status, but it renders no dataplane cluster for them and reports the reason in the `ResolvedRefs` status condition of the route.
- Dual-stack TURN: STUNner falls back to single-stack mode.
- High-availability operator: an operator failover rolls the dataplane, disconnecting the active clients.

The [Gateway API reference](GATEWAY.md) marks the configuration fields that are not supported in the free tier.

## Upgrading to a premium tier

Subscribing to a premium tier gets you a customer key. Install the premium Helm chart with your customer key and check that your license is valid by following the [premium installation guide](PREMIUM_INSTALL.md). The [`stunnerctl license`](cmd/stunnerctl.md#license-status) command reports your tier (`free`, `member` or `enterprise`) and the features it unlocks.

If unsure, [contact us](mailto:info@l7mp.io).
