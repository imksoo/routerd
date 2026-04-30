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
- FreeBSD NTT-profile rendering now starts KAME `dhcp6c` with `-n` so service
  restarts do not send DHCPv6 Release while Renew/Rebind timing remains
  delegated to `dhcp6c`.
- Linux `IPv6PrefixDelegation` can now use `client: dhcp6c`. This renders a
  managed WIDE/KAME-style `dhcp6c.conf` and systemd unit so NTT home-gateway
  profiles can avoid systemd-networkd Renew/Rebind packets with zero IA Prefix
  lifetimes.
- `IPv6PrefixDelegation` now has manual `serverID`, `priorPrefix`, and
  `acquisitionStrategy` fields for the DHCPv6 active-controller path. Renderers
  can receive the resource status and prefer explicit spec overrides before
  falling back to observed lease state.
- Added the first DHCPv6 active-controller command path: `routerd dhcp6
  request|renew|release --resource <name>`. Request/Renew packets use fresh
  transaction IDs, non-zero T1/T2 and IA Prefix lifetimes, and Reconfigure
  Accept; Release sends zero lifetimes without Reconfigure Accept.
- FreeBSD apply no longer rewrites `dhcp6c_flags="-n"` on every loop. This
  prevents unnecessary `dhcp6c` restarts and preserves the DHCPv6 client's
  in-memory lease state for natural Renew/Rebind.
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
  DUID-LL by default, omit exact prefix hints, suppress DHCPv6 hostname
  sending, and keep `duidRawData` / `iaid` only as explicit operator
  overrides for migration or HA cases. NTT profiles also disable networkd
  DHCPv6 option-use knobs that are not needed for PD where networkd exposes
  them.
- `IPv6RAAddress` now models WAN-side RA/SLAAC separately from DHCPv6-PD so
  DS-Lite AFTR DNS lookups can rely on an upstream IPv6 address and RA default
  route.
- Router diagnostics are now part of the expected host toolset: Linux remote
  checks require `dig`, `ping`, `tcpdump`, and `tracepath`; FreeBSD checks
  require `dig` alongside the base `ping`, `ping6`, `tcpdump`, and
  `traceroute` tools. Host inventory records additional troubleshooting
  commands when present.
- dnsmasq conditional forwarding now renders IPv6 upstream DNS addresses in the
  dnsmasq `server=/domain/addr` form without URL-style brackets.
- Apply now derives delegated LAN IPv6 addresses and DS-Lite tunnel source
  addresses from the current PD state object when available, and removes
  stale routerd-derived IPv6 addresses that share managed suffixes after a PD
  change.
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
