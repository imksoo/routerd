---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records the
behavior changes and new resource shapes as the model takes shape.

## Unreleased

- Breaking: routerd now uses `apply` as the user-facing verb. The old
  `reconcile` CLI and control API actions were replaced by `routerd apply`,
  `routerctl apply`, and `/apply`; the YAML `spec.reconcile` policy name stays
  unchanged.
- Breaking: removed obsolete pre-release DHCPv6-PD workaround fields. DHCPv6
  Renew/Rebind and Release behavior is delegated to the OS client.
- `routerctl` now has kubectl-style `get`, `describe`, and `show` verbs.
  `show` combines desired config, observed host state, ownership ledger data,
  state history, and events; NAPT/conntrack inspection is reported under
  `IPv4SourceNAT` observed state.
- Local state and ownership storage moved to SQLite with Kubernetes-style
  generations, objects, artifacts, events, and reserved access logs. Apply
  generations and events are first-class records, and `routerctl describe
  inventory/host` shows collected OS inventory.
- DHCPv6-PD state is stored in the structured
  `ipv6PrefixDelegation.<name>.lease` object. NTT profiles use MAC-derived
  DUID-LL by default, omit exact prefix hints, and keep `duidRawData` only as
  an explicit operator override for migration or HA cases.
- FreeBSD groundwork now uses KAME `dhcp6c` for DHCPv6-PD, `dhclient` or the
  configured IPv4 DHCP client for IPv4, `mpd5` for PPPoE, and rc.d-managed
  dnsmasq for LAN services. FreeBSD remote install builds the proper target
  binaries and checks required tools.
- Resource ownership and adoption now have a common foundation: resource kinds
  emit artifact intents, the local ledger records owned artifacts,
  `routerd adopt --candidates` lists adoption candidates, `routerd adopt
  --apply` records matching candidates, and apply reports or cleans known
  orphaned artifacts.
- Networking features added in the current model include DS-Lite tunnels,
  PPPoE, IPv4 source NAT, IPv4 default route policy with route-set candidates,
  path MTU and TCP MSS policy, reverse-path filter resources, health check
  roles, minimal firewall resources, NTP client configuration, log sinks,
  dnsmasq-backed DHCP/DNS with explicit listen interfaces, and safer DHCPv6
  client firewall handling.
- NixOS rendering groundwork can emit host settings, systemd-networkd links,
  packages, persistent sysctl values, reverse-path firewall relaxation for
  router hosts, and an optional local `routerd.service`.
- Added the Docusaurus documentation site for routerd.net with English and
  Japanese content.

## 0.1.0 planning baseline

- Initial resource model for interfaces, static IPv4, DHCP stubs,
  plugins, dry-run, status JSON, and the systemd service layout.
