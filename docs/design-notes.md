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

## 7. OpenRC service rendering

Alpine uses OpenRC rather than systemd. The OpenRC renderer intentionally starts
as a renderer, not an applier: `routerd render alpine --out-dir` writes reviewable
init scripts and related config so installed-host behavior can be tested before
routerd starts mutating OpenRC state.

The first supported OpenRC surface is narrow:

- explicit `generated service artifacts` resources mapped to OpenRC scripts
- synthesized `routerd-healthcheck` scripts
- synthesized managed dnsmasq scripts when DHCP or RA resources require dnsmasq
- synthesized scripts for DHCPv4/DHCPv6 clients, firewall logger, PPPoE, and
  Tailscale
- DNS resolver scripts, with enable/start deferred until routerd can materialize
  the resolver runtime config outside the controller loop

This keeps the code out of a compatibility trap. `generated service artifacts` remains the API
shape for now, but OpenRC only maps fields that have clear init-script meaning:
`ExecStart`, `ExecStartPre`, environment, working directory, user/group, and
runtime/state/log directories. systemd sandboxing, networkd, resolved, and
timesyncd semantics are not emulated on OpenRC.

Apply-time activation is gated by `HasOpenRC`. It writes scripts only when
content or mode changes, checks `rc-update show default` before adding or
removing a service, and checks `rc-service <name> status` before start,
restart, or stop. This keeps OpenRC behavior aligned with the systemd
idempotency rule: no repeated service-manager commands when desired state and
files are unchanged.

The next implementation step is promoting the Alpine installed-host smoke
harness into a regular VM job.

## 8. Open work

- Stateful firewall in production: `FirewallRule` now covers ICMP type matching,
  multiple ports per rule, nftables rate limiting, and per-source connection
  limits. Future work should focus on rule grouping and higher-level policy
  ergonomics rather than basic expression coverage.
- DoH proxying for clients on the LAN.
- BFD for BGP peers that need sub-second failure detection.
- Operator-controlled IngressService backend drain mode through `routerctl` without editing YAML.
- More production examples for VRRP `advertInterval`, `preempt`, and `preemptDelay` tuning.
- Validation for listen-port collisions between `IngressService`, local service redirects, and routerd-managed daemons.
- IPv6 BGP and VRRPv3 for dual-stack Kubernetes clusters.
- OSPF integration via FRR for the Tier C tier.
- High availability (leader election, fault-tolerant control plane).
- Production observability: OpenTelemetry collector and remote log sinks.
- Long-running validation of routerd as the only WAN router on a residential link.
