---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records the
behavior changes and new resource shapes as the model takes shape.

## Unreleased

- FreeBSD remote install hardening: `ROUTERD_OS=freebsd` now builds
  FreeBSD binaries and uses FreeBSD runtime directories even when invoked
  from a Linux workstation.
- Remote dependency checks now cover `jq`, FreeBSD `dhcp6c`, and `sysrc`.
- FreeBSD DHCPv6-PD rendering now emits KAME `dhcp6c` syntax accepted by
  the packaged client.
- Resource ownership and adoption foundation: every resource kind now
  emits artifact intents, the local ownership ledger records routerd-owned
  host artifacts, `routerd adopt --candidates` reports adoption candidates
  read-only, and reconcile reports orphan candidates for managed routing
  and nftables artifacts.
- `routerd adopt --apply` records matching adoption candidates in the
  ledger without changing host state. Successful non-dry-run reconcile
  also updates the ledger automatically.
- Ledger-backed orphan cleanup for DS-Lite tunnels, routerd nftables
  tables, and routerd systemd services.
- `PathMTUPolicy` for IPv6 RA MTU advertisement and nftables TCP MSS
  clamping.
- Minimal firewall resources `Zone`, `FirewallPolicy`, and
  `ExposeService` under `firewall.routerd.net/v1alpha1`.
- `HealthCheck.spec.role` to distinguish link, next-hop, internet,
  service, and policy-aggregation semantics.
- Docusaurus website scaffold for routerd.net, configured for Cloudflare
  Pages with English and Japanese locales.
- `NTPClient` for static `systemd-timesyncd` configuration.
- Explicit `listenInterfaces` allow-listing on dnsmasq-backed DHCP and
  DNS, with DNS bind addresses scoped to router self addresses.
- Remote syslog support through `LogSink`.
- `IPv4DefaultRoutePolicy` candidates that reference `IPv4PolicyRouteSet`,
  preserving conntrack marks for healthy targets.
- PPPoE interface rendering and routerd-managed systemd unit.
- NixOS renderer groundwork for host settings, systemd-networkd links,
  dependency packages, and persistent sysctl values.
- IPv4 default route selection now ignores route-set candidates whose
  target interfaces do not exist, so DS-Lite fallback can use DHCPv4
  while prefix delegation is still unavailable.

## 0.1.0 planning baseline

- Initial resource model for interfaces, static IPv4, DHCP stubs,
  plugins, dry-run, status JSON, and the systemd service layout.
