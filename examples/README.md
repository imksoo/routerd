# routerd examples

These examples are valid `routerd.net/v1alpha1` configurations. They are
intended as starting points, not as drop-in production files.

Run validation before adapting an example:

```sh
routerd validate --config examples/<name>.yaml
routerd plan --config examples/<name>.yaml
```

## Small starting points

| File | Use when |
| --- | --- |
| `basic-static.yaml` | You want the smallest possible managed interface and static IPv4 address. |
| `basic-dhcp.yaml` | You want to see DHCP client and DHCP server resources without WAN policy. |
| `dns-local-zone.yaml` | You want a local authoritative DNS zone with static records. |
| `dns-private-upstream.yaml` | You need conditional DNS forwarding or private upstream resolvers. |
| `guest-mode.yaml` | You want MAC-based guest isolation that keeps DHCP/DNS/NTP local services available while blocking private-address egress. |
| `telemetry-export.yaml` | You want to send routerd telemetry to an OTLP collector. |
| `observability-loki.yaml` | You want routerd OTLP export plus routerd event log forwarding to Loki. |
| `ha-2-node.yaml` | You want a two-node routerd lease gate so only the leader applies host changes. |

## VPN and segmentation

| File | Use when |
| --- | --- |
| `tailscale-minimal.yaml` | You only want the node to join a Tailscale tailnet. It does not advertise an exit node or subnet routes. |
| `tailscale-exit-subnet.yaml` | You want to advertise this router as a Tailscale exit node or subnet router. |
| `wireguard-hub-spoke.yaml` | You want a hub router with multiple WireGuard spokes and routed spoke prefixes. |
| `vrf-lab.yaml` | You want to separate guest, staff, and IoT interfaces with Linux VRF route tables. |
| `bgp-bfd.yaml` | You want FRR BGP peers with BFD-based sub-second failure detection and tuned watcher limits. |
| `dualstack-bgp.yaml` | You want one FRR BGP instance with mixed IPv4 and IPv6 unicast peers and policies. |
| `k8s-cluster-routes.yaml` | You want static Pod CIDR and Service CIDR routes toward Kubernetes worker nodes. |
| `k8s-api-vip-dualstack.yaml` | You want a Kubernetes API VIP pattern with IPv4 VRRP, IPv6 VRRPv3, DNS A/AAAA, and dual-stack BGP peers. |
| `multi-instance-bgp.yaml` | You want separate FRR BGP instances for LAN speakers and a VRF-backed WAN peering domain. |
| `vrrp-tuning-presets.yaml` | You want VRRP/CARP timing presets for API VIP and conservative LAN failover patterns. |

## WAN and home-router patterns

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
| `nixos-edge.yaml` | You want a routerd configuration that exercises the NixOS render path. |
| `nixos-edge-configuration.nix` | You want the companion NixOS system configuration shape. |
| `freebsd-edge.yaml` | You want a compact FreeBSD rc.d, pf, dnsmasq, and DS-Lite rendering example. |
| `freebsd-vrrp.yaml` | You want a minimal FreeBSD CARP-backed `VirtualAddress` example. |
