# routerd examples

These examples are valid `routerd.net/v1alpha1` configurations. They are
starting points, not drop-in production files. Start with an isolated Ubuntu
Server VM or a spare host with a console; examples that change interfaces,
DHCP, RA, routes, or firewall behavior can interrupt a live network.

Before a daemon exists, validate an example directly and use a dry-run with
temporary state paths:

```sh
routerd validate --config examples/<name>.yaml

workdir=$(mktemp -d)
routerd apply --config examples/<name>.yaml --once --dry-run \
  --state-file "$workdir/state.db" \
  --ledger-file "$workdir/ledger.db" \
  --status-file "$workdir/status.json"
rm -rf "$workdir"
```

`routerctl validate` and `routerctl plan` are for a running local
`routerd.service`.

## First lab and small starting points

| File | Use when |
| --- | --- |
| `basic-static.yaml` | You have already created a `dummy0` device in a disposable lab and want the smallest managed interface/address example. It does not create `dummy0` for you. |
| `basic-dhcp.yaml` | You want to see a DHCPv4 client resource without a LAN server or WAN policy. |
| `example-basic-ipv4-nat.yaml` | You have an isolated two-NIC lab and want a complete IPv4 WAN DHCP, LAN DHCP, external DNS advertisement, and NAT example. |
| `dns-local-zone.yaml` | You want a local authoritative DNS zone with static records. |
| `dns-private-upstream.yaml` | You need conditional DNS forwarding or private upstream resolvers. |
| `guest-mode.yaml` | You want to inspect a MAC-based client policy. It controls traffic that reaches the router; it is not VLAN, Wi-Fi, or switch-port isolation. |
| `dhcp-lease-sync-ha.yaml` | You want active-to-standby DHCP lease sync gated by a VRRP `VirtualAddress` role. |
| `telemetry-export.yaml` | You want to send routerd telemetry to an OTLP collector. |
| `observability-loki.yaml` | You want routerd OTLP export plus routerd event log forwarding to Loki. |
| `ha-2-node.yaml` | You want a two-node routerd lease gate so only the leader applies host changes. |

## VPN and segmentation (read prerequisites first)

| File | Use when |
| --- | --- |
| `tailscale-minimal.yaml` | You only want the node to join a Tailscale tailnet. It does not advertise an exit node or subnet routes. |
| `tailscale-exit-subnet.yaml` | You want to advertise this router as a Tailscale exit node or subnet router. |
| `wireguard-hub-spoke.yaml` | You want a hub router with multiple WireGuard spokes and routed spoke prefixes. |
| `vrf-lab.yaml` | You want to separate guest, staff, and IoT interfaces with Linux VRF route tables. |
| `bgp-bfd.yaml` | You want BGP peers with FRR BFD session observation and tuned watcher limits. |
| `dualstack-bgp.yaml` | You want one BGP instance with mixed IPv4 and IPv6 unicast peers and policies. |
| `k8s-cluster-routes.yaml` | You want static Pod CIDR and Service CIDR routes toward Kubernetes worker nodes. |
| `k8s-api-vip-dualstack.yaml` | You want a Kubernetes API VIP pattern with IPv4 VRRP, IPv6 VRRPv3, DNS A/AAAA, and dual-stack BGP peers. |
| `multi-instance-bgp.yaml` | You want separate BGP instances for LAN speakers and a VRF-backed WAN peering domain. |
| `vrrp-tuning-presets.yaml` | You want VRRP/CARP timing presets for API VIP and conservative LAN failover patterns. |

## WAN and home-router patterns (ISP-specific or advanced)

| File | Use when |
| --- | --- |
| `dslite-lan-range-snat.yaml` | You need the optional DS-Lite inner-source form that uses an address carved from a LAN range. |
| `multi-wan-home.yaml` | You want a compact template for DS-Lite failover with DHCP WAN fallback. |
| `router-lab.yaml` | You want a compact Linux lab router with common WAN and LAN services. |
| `linux-dslite-policy.yaml` | You want a lab-style DS-Lite and policy-routing example. |
| `home-router.yaml` | You want a compact Ubuntu home-router reference with DS-Lite, LAN services, BGP, and Web Console. |

## OS-oriented examples

| File | Use when |
| --- | --- |
| `nixos-edge-configuration.nix` | You want the companion NixOS system configuration shape. NixOS support is groundwork, not the recommended first lab path. |
| `freebsd-edge.yaml` | You want a compact FreeBSD rc.d, pf, dnsmasq, and DS-Lite rendering example after checking feature support. |
| `freebsd-vrrp.yaml` | You want a minimal FreeBSD CARP-backed `VirtualAddress` example. |
