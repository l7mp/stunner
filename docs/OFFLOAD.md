# Offload

STUNner uses an eBPF offload on the TC or XDP hooks to improve the performance of UDP channels. The offload implementation is [based on pion/turn](https://github.com/pion/turn/pull/360).

## Setting up the eBPF offload

Offload mechanisms are enabled automatically. During start-up, the STUNner offload engine tries to load first the TC or second the XDP offload into the system. If both fail, STUNner falls back to no offload.

## Check offload functionality

**Logs:** The offload engine logs its status consequently from its start until its shutdown. In addition, each channel binding offload gets a log entry on INFO level.

```
EXAMPLE LOG ENTRY BE HERE
```

**eBPF maps:**
TODO

<!-- ## Troubleshooting -->



## Known Limitations of the XDP offload
- XDP works on GNU/Linux only.
- Loading the XDP program might require admin privileges that might be not available on arbitrary Kubernetes clusters.
- The offload works for UDP routes only.
- We disabled the XDP offload for host-local redirects. We had some weird issues with forwarding traffic between NICs with the xdp driver to NICs with the xdpgeneric drivers (except the lo interface).
- Monitoring is limited. Combining the user space and kernel space metrics are currently not supported.
