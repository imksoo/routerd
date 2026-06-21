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

Initial topology:

- `rr1`
- `leaf-a`
- `leaf-b`
- `client-a`
- `client-b`

All router namespaces share an underlay bridge. Each client namespace connects
to a leaf through a separate access bridge. The first test builds the binaries,
creates the namespaces, starts real `routerd-bgp` and `routerd` processes in
the router namespaces, verifies underlay reachability, and polls real
`routerctl status` through per-node Unix sockets.

This is the foundation for the next slices of #622:

1. assert WireGuard handshakes and GoBGP `Established` state from routerd
   status;
2. add route-table/proxy-ARP capture distribution assertions;
3. stop `routerd` in one leaf namespace for failover takeover;
4. restart it and assert no-preempt rejoin;
5. add explicit forced rebalance and distribution checks.

