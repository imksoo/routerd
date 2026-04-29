---
title: Basic firewall
sidebar_position: 4
---

# Basic firewall

The router has WAN connectivity, a LAN address, and dnsmasq. LAN clients
can talk to it, but their traffic still cannot reach the upstream. This
tutorial adds:

- IPv4 SNAT for traffic leaving the WAN.
- A small default-deny home-router firewall preset.
- IPv6 forwarding (no NAT — IPv6 hosts have global addresses).

## 1. Source-NAT IPv4 outbound

Add an `IPv4SourceNAT` resource:

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4SourceNAT
      metadata:
        name: lan-out
      spec:
        outInterface: wan
        sourceInterface: lan
```

routerd renders this into nftables. The kernel masquerades LAN-side
traffic as it leaves the WAN.

## 2. Add the home-router firewall preset

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: HomeRouterFirewall
      metadata:
        name: home
      spec:
        wan: wan
        lan: lan
```

The preset is a small default-deny config:

- Drop new connections inbound on the WAN.
- Allow established and related flows in both directions.
- Allow LAN → WAN.
- Allow LAN → router for DNS, DHCP, ICMP.
- Drop spoofed source addresses (uRPF).

It is intentionally conservative. Anything beyond that needs an explicit
resource.

## 3. Apply

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

Inspect the rendered nftables:

```bash
sudo nft list ruleset
```

You should see jump rules into a `routerd_*` chain set, and the
home-router preset rules.

## 4. Test from a LAN client

```bash
# IPv4 outbound
curl -v https://example.com

# IPv6 outbound (if you set up PD in the previous tutorial)
curl -v https://[2606:2800:220:1:248:1893:25c8:1946]/

# DNS through the router
dig @192.168.10.1 example.com
```

The home-router preset allows none of the LAN's services to be reachable
from the WAN side. Inbound exposure is opt-in.

## 5. Open one inbound port (optional)

To expose, say, an SSH service on the WAN, add a `Service` and a
`PortForward` (or use an explicit firewall rule). The full set of
firewall kinds is in the
[API reference](../reference/api-v1alpha1#zone).

## What's left

You now have a working small router with:

- WAN DHCPv4 (and optionally IPv6 PD).
- LAN static address with DHCP/DNS/RA.
- IPv4 SNAT and a default-deny firewall.

Common next steps:

- Multi-WAN with health checks (`Ipv4DefaultRoutePolicy` with
  `healthChecks`).
- DS-Lite, MAP-E, PPPoE for specific upstream technologies.
- Conditional DNS forwarding for split-horizon names.

Each of those is a separate resource in the YAML; layer them in the same
"add one, apply, verify" pattern these three tutorials used.

## Next

- [Router lab](./router-lab) — a more realistic full configuration.
- [API reference](../reference/api-v1alpha1) — the complete kind catalog.
- [Resource ownership](../reference/resource-ownership) — what `apply`
  promises before you trust it on a remote router.
