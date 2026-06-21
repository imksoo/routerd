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

Each site has an independent L2 bridge. The leaf sites intentionally reuse
`10.77.60.0/24` on separate bridges to model same-subnet cloud/on-prem sites.
Router namespaces also attach to a transport bridge used only as the local
replacement for cloud underlay reachability between WireGuard endpoints.

The first test builds the binaries and the netns provider executor, creates the
namespaces, starts real `routerd-bgp` and `routerd` processes in the RR/leaf
namespaces, verifies transport reachability, polls real `routerctl status`, then
waits for WireGuard and BGP convergence.

This is the foundation for the next slices of #622:

1. assert WireGuard handshakes and GoBGP `Established` state from routerd
   status;
2. add route-table/proxy-ARP capture distribution assertions;
3. stop `routerd` in one leaf namespace for failover takeover;
4. restart it and assert no-preempt rejoin;
5. add explicit forced rebalance and distribution checks.
