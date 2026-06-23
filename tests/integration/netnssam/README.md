# netns SAM Integration Harness

This package is the local integration layer for issue #622.

It runs real `routerd`, `routerctl`, and `routerd-bgp` binaries on one Linux
host, but each node runs inside its own network namespace. Namespaces are wired
with veth pairs and Linux bridges, so WireGuard, GoBGP, routing, iptables, and
kernel forwarding are exercised against real kernel networking state without
LXC, Docker, cloud APIs, or VMs.

The tests are skipped by default because they mutate host network namespace
state and require root:

```sh
sudo ROUTERD_NETNS_INTEGRATION=1 go test ./tests/integration/netnssam -count=1 -run TestNetNS
```

Initial topology mirrors the current full cloud/on-prem lab:

- `aws-rr`: `aws-rr-a`, `aws-rr-b`
- `aws-leaf`: `aws-leaf-a`, `aws-leaf-b`, `aws-client-a`, `aws-client-b`
- `azure-leaf`: `azure-leaf-a`, `azure-leaf-b`, `azure-client-a`, `azure-client-b`
- `oci-leaf`: `oci-leaf-a`, `oci-leaf-b`, `oci-client-a`, `oci-client-b`
- `pve-leaf`: `pve-leaf-a`, `pve-leaf-b`, `pve-client-a`, `pve-client-b`

Each site has a small fabric namespace between clients and leaf routers:

```text
[client] --veth-- [fabric-<site> netns] --veth-- [leaf-<site>]
```

The fabric namespace uses separate client-side and leaf-side L2 bridges. This
keeps client and leaf ARP domains apart while still letting the leaf sites reuse
`10.77.60.0/24` in independent namespaces. Provider captures are modeled in a
shared harness state file, representing the cloud/fabric side outside the VM.
The netns provider executor updates that state file, the provider inventory
plugin reports captures from it, and the fabric route reconciler programs
`<capture>/32 via <leaf-primary>` from it. Router namespaces may still configure
the same `/32` inside the VM via `configureOSAddress`, but that is intentionally
separate from provider ownership. Router namespaces also attach to a transport
bridge used only as the local replacement for cloud underlay reachability between
WireGuard endpoints.

The smoke test builds the binaries and the netns provider executor, creates the
namespaces, starts real `routerd-bgp` and `routerd` processes in the RR/leaf
namespaces, verifies transport reachability, polls real `routerctl status`, then
waits for WireGuard, BGP, mobility, provider action, and client-matrix
convergence.

The fault tests reuse the same setup independently. They cover leaf process
failure, no-preempt rejoin, forced capture rebalance, graceful mobility drain
through routerd graceful stop, and BFD-assisted liveness detection. The BFD-only
fault test uses GoBGP native BFD and requires `iptables` in addition to the base
netns tools.
