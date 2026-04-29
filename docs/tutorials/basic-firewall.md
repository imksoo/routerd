---
title: Basic firewall
sidebar_position: 4
---

# Basic firewall

The router has WAN connectivity, a LAN address, and dnsmasq. LAN clients
can talk to it, but their traffic still cannot reach the upstream. This
tutorial adds:

- IPv4 SNAT for traffic leaving the WAN.
- A small default-deny home-router firewall preset built from a
  `Zone` and a `FirewallPolicy`.
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

## 2. Add zones and the home-router firewall policy

```yaml
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: Zone
      metadata:
        name: lan
      spec:
        interfaces:
          - lan

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: Zone
      metadata:
        name: wan
      spec:
        interfaces:
          - wan

    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallPolicy
      metadata:
        name: default-home
      spec:
        preset: home-router
        input:
          default: drop
        forward:
          default: drop
```

The preset is a small default-deny config:

- Drop new connections inbound on the WAN.
- Allow established and related flows in both directions.
- Allow LAN → WAN.
- Allow LAN → router for DHCP and DNS (the LAN-side services from the
  previous tutorial).
- **Allow SSH (TCP/22) from the LAN to the router.** The
  `FirewallPolicy.spec.routerAccess.ssh.fromZones` field defaults to
  `["lan"]` when omitted, so a LAN host can `ssh` into the router as
  soon as this policy is applied.
- Allow ICMPv6 across the input chain.

It is intentionally conservative. Anything beyond that — for example
SSH from the WAN, or exposing a service — needs an explicit resource.

### About SSH access

With the YAML above:

| From | To router (SSHd) | Allowed? |
|---|---|---|
| LAN host | `ssh root@<router LAN IP>` | ✅ yes (LAN is the default `fromZones`) |
| WAN host | `ssh root@<router WAN IP>` | ❌ no (input on WAN is `drop`) |

You do **not** need a separate management interface for LAN-side SSH.
A separate `mgmt` zone is only useful if you want a dedicated
out-of-band path (a different NIC reserved for administration) and
want routerd's apply guardrails to keep that path open even if you
make a mistake in another resource. To enable that guardrail:

```yaml
spec:
  apply:
    protectedZones:
      - mgmt
  resources:
    # ...your usual zones, plus a Zone named mgmt...
```

`protectedZones` makes routerd always accept TCP/22 from the listed
zones, regardless of what the active `FirewallPolicy` says. The list
should match `Zone` resources you have defined.

## 3. Apply

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

Inspect the rendered nftables:

```bash
sudo nft list ruleset
```

You should see jump rules into `routerd_*` chains, the default-deny
input/forward policies, and an `iifname "<lan-iface>" tcp dport 22 accept`
line for SSH.

## 4. Test from a LAN client

```bash
# IPv4 outbound
curl -v https://example.com

# IPv6 outbound (if you set up PD in the previous tutorial)
curl -v https://[2606:2800:220:1:248:1893:25c8:1946]/

# DNS through the router
dig @192.168.10.1 example.com

# SSH into the router (allowed by default)
ssh <user>@192.168.10.1
```

The home-router preset allows none of the LAN's services to be
reachable from the WAN side. Inbound exposure is opt-in.

## 5. Open one inbound port (optional)

To expose, say, an HTTPS service on the WAN, add an `ExposeService`
resource. The full set of firewall kinds is in the
[API reference](../reference/api-v1alpha1#zone).

## What's left

You now have a working small router with:

- WAN DHCPv4 (and optionally IPv6 PD).
- LAN static address with DHCP/DNS/RA.
- IPv4 SNAT and a default-deny firewall.
- SSH from the LAN side allowed by default.

Common next steps:

- Multi-WAN with health checks (`IPv4DefaultRoutePolicy` with
  `healthChecks`).
- DS-Lite, MAP-E, PPPoE for specific upstream technologies.
- Conditional DNS forwarding for split-horizon names.

Each of those is a separate resource in the YAML; layer them in the
same "add one, apply, verify" pattern these three tutorials used.

## Next

- [Router lab](./router-lab) — a more realistic full configuration.
- [API reference](../reference/api-v1alpha1) — the complete kind catalog.
- [Resource ownership](../reference/resource-ownership) — what `apply`
  promises before you trust it on a remote router.
