---
title: Changelog
---

# Changelog

routerd release history. The format follows [Keep a Changelog](https://keepachangelog.com/).
The software is at the v1alpha1 stage; minor `0.x` releases may contain breaking changes.

## Unreleased

### Added

- The implicit-deny log lines from nftables are now ingested by `routerd-firewall-logger` and stored in `firewall-logs.db`. On Linux the logger reads `nfnetlink` directly; on FreeBSD it consumes `pflog` through `tcpdump`.
- The Web Console gained a Connections tab (live conntrack / pf state), a Clients tab (DHCP lease + traffic statistics combined), and a Firewall tab (deny ranking plus a per-second timeline).
- `WebConsole.spec.listenAddressFrom` and the listen address of `DNSResolver` resources can now be derived from `Interface/<name>.status.ipv4Addresses`. Reference fields can be used in place of literal IP values.
- Conntrack accounting (`net.netfilter.nf_conntrack_acct=1`) is enabled in the default `SysctlProfile/router-linux` profile, so `TrafficFlowLog` can record `bytesOut` and `bytesIn`.

### Changed

- The live connection view in API and CLI is unified under the name `connections` (previously `conntrack-snapshot`). Use `/api/v1/connections` and `routerctl connections`. IPv6 connections are surfaced in the same table.
- NixOS rendering was extended. `Package` (NixOS-style declarations), `SysctlProfile`, `NetworkAdoption`, and `SystemdUnit` now flow into the `routerd render nixos` output. On NixOS the `Package` resource is no longer installed at runtime; its content is owned by the generated NixOS configuration instead.
- `SystemdUnit` resources can now produce FreeBSD `rc.d` scripts via `routerd render freebsd --out-dir`.

### Fixed

- `IPv6DelegatedAddress` no longer skips applying the delegated address to a host interface when the upstream `Link/<name>` status is empty.
- `SystemdUnit` no longer restarts an already-active unit when nothing has changed.

## 0.3.0

### Added

- `Package` and `SysctlProfile` resources for declarative OS bootstrap. They cover apt, dnf, nix, and pkg package declarations as well as router-oriented sysctl tuning (`nf_conntrack_max`, socket buffers, TCP/UDP timeouts, `ip_forward`, etc.) in a single resource.
- `NetworkAdoption` disables systemd-networkd's DHCP / RA from YAML. `SystemdUnit` lets routerd render, install, and enable its own unit files.
- `routerctl events --limit N --topic X --resource K/N -o json` reports bus events without requiring `sqlite3`.
- `routerd plan --diff` previews the diff that an apply would produce.
- `DNSResolver` accepts a bootstrap forwarder so internal DNS can be tried first while public DNS acts as a fallback.

### Changed

- `${...status.field}` string references inside the configuration were replaced by typed `*From` fields (`addressFrom`, `ipv4From`, `ipv6From`, `upstreamFrom`, `prefixFrom`, `rdnssFrom`, `dependsOn`). No backwards-compatible aliases.
- The controller chain was rebuilt as a pure event-loop. A common `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) and an `eventedStore` wrapper guarantee that any persisted state change emits `routerd.resource.status.changed`, which downstream controllers consume.
- Bus events are emitted to the systemd journal through `slog`. `journalctl -u routerd.service -f | grep "routerd event"` traces the controller chain. High-frequency topics are at the debug level.
- All binaries are now statically linked (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`). The OS-specific package list (`dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan-swanctl`, `radvd`, `tcpdump`, etc.) is documented per Ubuntu / NixOS / FreeBSD.
- `HealthCheck.sourceInterface` is written as a resource name in YAML and resolved to an OS interface name at runtime.

### Fixed

- The `RuntimeDirectory` collision between `SystemdUnit` resources that previously deleted sockets across restarts is solved declaratively via `runtimeDirectoryPreserve`.
- `SystemdUnit` with `state: absent` is now correctly detected as Drifted and unit removal is included in the plan.
- `SysctlProfile` observation no longer reports spurious drift caused by type coercion.

## 0.2.0

### Added

- Stateful firewall: `FirewallZone`, `FirewallPolicy`, and `FirewallRule` generate the `inet routerd_filter` table for nftables.
- `EgressRoutePolicy` (formerly `WANEgressPolicy`) gained `destinationCIDRs`, `gateway`, and `gatewaySource`. `HealthCheck` accepts `via`, `sourceInterface`, and `sourceAddress` to scope the probe path.
- The DNS subsystem was reorganised. `DNSZone` (authoritative zone definition) and `DNSResolver` (forwarder / cache) cover local zones, conditional forwarding, DoH / DoT / DoQ, and plain UDP DNS. dnsmasq is now scoped to DHCPv4 / DHCPv6 / RA / relay only.
- DS-Lite (`DSLiteTunnel`), PPPoE (`PPPoESession`, `routerd-pppoe-client`), DHCPv4 client (`routerd-dhcpv4-client`, `DHCPv4Lease`).
- NAT44 (`NAT44Rule`) and conntrack observation. The observer falls back to a sysctl-derived summary when `/proc/net/nf_conntrack` is unavailable.

### Changed

- `WANEgressPolicy` was renamed to `EgressRoutePolicy`. No backwards-compatible aliases.
- DHCP client kinds and binary names were aligned with RFC notation: `routerd-dhcpv4-client`, `routerd-dhcpv6-client`. No backwards-compatible aliases.

## 0.1.0

The first v1alpha1 implementation.

- Introduced the DHCPv6-PD client, the daemon contract, the event bus, and the controller framework.
- Implemented the controller chain that turns DHCPv6-PD into LAN address derivation and DNS responses.
- Added DHCPv6 information request, prototype DS-Lite, IPv4 routing, RA, DHCPv6 server, `HealthCheck`, `EventRule`, and `DerivedEvent`.

API names and implementation strategies have changed substantially since this version as part of pre-release cleanup. For current usage, refer to the `Unreleased` section above and the `examples/` directory.
