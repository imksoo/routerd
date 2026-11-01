# routerd 1.5 current Linux PVE WireGuard qualification

## Scope

This record qualifies the WireGuard interface, peer, static IPv4 address, and
firewall-zone paths in the Linux binary built from source revision
`d9623bbd867d450ff8d1be03c59222592dbf43ad`.

The test used a standalone routerd 1.5 configuration. It did not read or
import a routerd 1.0 configuration, database, handoff marker, or package.

## Executed setup

- Router: isolated Linux PVE VM running the actual `routerd` system service.
- Peer: separate isolated Linux PVE VM in the WAN VLAN namespace.
- Underlay: `198.18.16.2:51821` on the router and `198.18.16.1:51820` on the
  independent peer.
- Tunnel: `WireGuardInterface/wg-lab` with kernel interface `rd15-wg`.
- Tunnel addresses: router `10.44.0.1/24`, peer `10.44.0.2/24`.
- Security policy: the routerd-managed WAN listener accepted the WireGuard UDP
  port; a separate `FirewallZone/vpn` with role `trust` owned `wg-lab`.

The router created the kernel interface and its static address itself. The
address status recorded `ifname: rd15-wg`, a native interface index, and a
WireGuard parent ownership token.

## Observed result

The independent peer and router each sent three ICMP echo requests through the
encrypted tunnel. Both directions received all three replies. The measured
round-trip times were below one millisecond in this isolated PVE topology.

`wg show` on both endpoints reported the configured endpoint and allowed IPs,
a current authenticated handshake, and nonzero received and sent byte counters.
The router reported `IPv4StaticAddress/wg-lab-ipv4` as `Applied` with the
expected `rd15-wg` native receipt.

After the test, routerd removed the WireGuard interface, peer, static address,
and `vpn` zone through a replacement of the temporary configuration with the
original standalone 21-resource routerd 1.5 configuration. The peer namespace
interface and temporary key files were removed. The router then reported:

```text
STATUS  HEALTHY  generation=3  resources=21
PASS: live lease, DNS UDP/TCP, NAT and management positive control
PASS: WAN/LAN/management and DNS isolation
```

## Unverified scope

This check establishes a single Linux WireGuard peer and encrypted IPv4
connectivity in the isolated PVE topology. It does not establish multi-peer
routing, key rotation, high availability behavior, other tunnel types, or
FreeBSD behavior.
