---
title: Configuration examples
sidebar_position: 0
---

# Configuration examples

This section is a catalog of small, copyable router patterns. It is intentionally
closer to a vendor "configuration example collection" than to a design document:
each page starts with a topology diagram, states what routerd can manage today,
then shows the smallest useful YAML shape.

Use these examples as starting points, not as drop-in production configs. Always
replace interface names, address ranges, ISP values, and management access
before applying them to a real router.

## How to read an example

Each example follows the same structure:

1. **Topology**: the physical or logical network layout.
2. **Diagram map**: numbered parts in the diagram and what each part means.
3. **Example config**: complete YAML in `examples/`, with numbered YAML excerpts in the page.
4. **Apply sequence**: validation and dry-run commands to run first.
5. **Checks**: commands that confirm the router converged.

The numbers in diagrams and YAML comments intentionally match. For example,
`[1]` in a diagram points to the same concept as `# [1]` in the config excerpt.

## Ready-to-try examples

| Example | Status | Use when |
| --- | --- | --- |
| [Basic IPv4 NAT gateway](./basic-ipv4-nat.md) | Works today | The WAN gets IPv4 by DHCP and the LAN uses private IPv4 with DHCPv4. |
| [LAN DHCP and local DNS](./lan-dns-dhcp.md) | Works today | You want routerd to serve DHCPv4, a local DNS zone, and DHCP-derived names on one LAN. |
| [DS-Lite home gateway](./dslite-home.md) | Works today with ISP-specific values | The access line is IPv6-first and IPv4 goes through a DS-Lite tunnel. |
| [PPPoE IPv4 NAT gateway](./pppoe-ipv4-nat.md) | Works today with ISP credentials | The WAN is an Ethernet access line and IPv4 comes from a PPPoE session. |
| [Port forward to an inside web server](./port-forward-web.md) | Works today with a known WAN address | You need to publish one inside HTTPS service and support hairpin access from LAN clients. |
| [Kubernetes API VIP with BGP](./kubernetes-api-vip.md) | Works today with FRR and keepalived installed | You want routerd to hold a Kubernetes API VIP, health-check control planes, and receive Service prefixes by BGP. |
| [Guest and IoT client isolation](./guest-isolation.md) | Works today on Linux nftables | A small set of MAC addresses should reach the internet but not the trusted LAN or management networks. |
| [Firewall rate limits and ICMP rules](./firewall-rate-limit.md) | Works today on Linux nftables | You need multi-port service openings, ICMP type matching, and SSH brute-force dampening. |
| [Multi-WAN IPv4 failover](./multi-wan-failover.md) | Works today; tune health checks carefully | Several IPv4 egress paths exist and routerd should select a healthy default route. |
| [Redirect public DNS to the local resolver](./local-dns-redirect.md) | Works today on Linux nftables | LAN clients try to query public plaintext DNS directly and you want port 53 to stay local. |
| [Tailscale subnet and exit node](./tailscale-subnet-exit.md) | Works today when Tailscale is installed | The router should advertise LAN routes or an exit-node service into a tailnet. |
| [WireGuard hub and spoke template](./wireguard-hub-spoke.md) | Template; replace keys and peer routes | You want a compact starting point for a routed WireGuard hub. |
| [Telemetry export to an OTLP collector](./telemetry-export.md) | Works today when a collector exists | You want routerd logs, metrics, and traces sent to an observability stack. |

## Patterns not ready as copyable examples

The following patterns are useful for first-time users, but they should not be
shown as ready-to-run YAML until the corresponding renderer or operational
guidance is complete:

| Pattern | Current state |
| --- | --- |
| MAP-E / v6plus-style IPv4 over IPv6 | Not implemented as a first-class resource yet. |
| OSPF or non-FRR dynamic routing | Not implemented. BGP through FRR is available for Kubernetes-style Service prefix import. |
| Full IPsec site-to-site cookbook | IPsec groundwork exists; production renderer parity is not documented as complete. |

## Safety checklist

Before applying an example on a router you are actively using:

- Keep console or hypervisor access available.
- Know which interface carries management traffic.
- Run `routerd validate`, `routerd plan`, and a dry-run apply first.
- Check that the plan does not remove the management interface address, route, or firewall opening.
- Apply from the release binary installed on the router, not from an unrelated development tree.

```bash
routerd validate --config router.yaml
routerd plan --config router.yaml
routerd apply --config router.yaml --once --dry-run
routerd apply --config router.yaml --once
routerctl status
```

## Related pages

- [Bring up the first router](../tutorials/first-router.md)
- [WAN-side services](../tutorials/wan-side-services.md)
- [LAN-side services](../tutorials/lan-side-services.md)
- [Basic NAT and firewall policy](../tutorials/basic-firewall.md)
