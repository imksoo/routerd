---
title: Design notes
---

# Design notes

This document records design decisions worth keeping. It is not a chronological log of past experiments — only the principles current code follows and that future changes should respect.

## 1. Daemon contract

Stateful work is carried out by dedicated daemons. Every daemon exposes the same surface so tooling can interact with them uniformly:

- HTTP+JSON API over a Unix domain socket
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- a state or lease file
- `events.jsonl` (append-only)

This contract is shared by `routerd-dhcpv6-client`, `routerd-dhcpv4-client`, `routerd-pppoe-client`, and `routerd-healthcheck`.

## 2. DHCPv6-PD

DHCPv6-PD is owned by `routerd-dhcpv6-client`. There is no longer a code path that generates configuration for an OS-bundled client.

For ordinary residential gateways, the standard solicit / advertise / request / renew sequence with lease persistence and T1 renewal is sufficient. Aggressive retries that were once needed to work around broken environments are no longer the default.

## 3. Honest LAN advertisement

When DHCPv6-PD is not `Bound`, routerd does not advertise stale IPv6 information to the LAN. This applies to RA, DHCPv6 server, AAAA records, and any LAN address derived from the prefix. The router stays "broken in an obvious way" rather than handing out addresses that no longer reach upstream.

## 4. DS-Lite

In some access networks the AFTR option is not returned in DHCPv6 information-request. `DSLiteTunnel` therefore treats a static `aftrFQDN` or `aftrIPv6` as a normal configuration path, not a fallback.

AFTR FQDNs frequently cannot be resolved by public DNS. Use `DNSResolver.spec.sources[].kind: forward` to send the AFTR domain to the in-network resolver.

## 5. Event coordination

routerd has an in-process bus. Controllers consume events and reconcile only the resources affected.

The following kinds work together for higher-level coordination:

- `EgressRoutePolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

`EventRule` consumes one stream of events and produces another. `DerivedEvent` synthesises asserted / retracted virtual events from observed state.

## 6. Tier S building blocks

WireGuard, Tailscale, IPsec, VRF, and VXLAN are the building blocks for the Tier S (SOHO / branch) capability. WireGuard and VXLAN-over-WireGuard interoperability is verified across the supported operating systems. `TailscaleNode` covers exit-node and subnet-router advertisement without turning every VPN into one abstract shape.

There is no abstract `VPNTunnel` resource. WireGuard, Tailscale, IPsec, and future SoftEther integrations are added as their own kinds. The motivation is that each of these has materially different state machines; collapsing them into one polymorphic kind would lose semantics.

## 7. Open work

- Stateful firewall in production: applied today, but still wants richer rule expressions, ICMP type matching, multiple ports per rule, and rate limiting.
- DoH proxying for clients on the LAN.
- BGP / OSPF integration via FRR for the Tier C tier.
- High availability (leader election, fault-tolerant control plane).
- Production observability: OpenTelemetry collector and remote log sinks.
- Long-running validation of routerd as the only WAN router on a residential link.
