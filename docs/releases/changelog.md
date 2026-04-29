---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records the
behavior changes and new resource shapes as the model takes shape.

## Unreleased

- NTT DHCPv6-PD profiles now omit systemd-networkd
  `PrefixDelegationHint=` by default. The profile still assumes `/60`
  delegation length, but the initial Solicit is kept closer to the
  commercial-router packet shape observed in the lab.
- DHCPv6 DUID handling now preserves explicit `duidRawData` overrides and
  restores real MAC-derived DUID-LL when the override is removed. FreeBSD KAME
  `dhcp6c` DUID files are backed up and rewritten when they differ from the
  desired DUID.
- Host OS inventory is now collected at apply time and stored as
  `routerd.net/v1alpha1/Inventory/host` in the SQLite objects table.
  `routerctl describe inventory/host` shows OS, kernel, virtualization,
  service-manager, DMI, and command availability observations.
- `IPv6PrefixDelegation.spec.releasePolicy` was added to control DHCPv6
  Release behavior. NTT profiles default to `never`, rendering
  systemd-networkd `SendRelease=no` and FreeBSD `dhcp6c -n`; other profiles
  default to `always`.
- Breaking: removed the HGW workaround fields
  `IPv6PrefixDelegation.spec.convergenceTimeout`, `spec.hintFromState`,
  `spec.preferredLifetime`, and `spec.validLifetime`. routerd leaves DHCPv6
  Renew/Rebind behavior to the OS client.
- CLI and control verbs now use `apply`: `routerd reconcile`,
  `routerctl reconcile`, and the control API apply action were renamed to
  `routerd apply`, `routerctl apply`, and `/apply`. The YAML
  `spec.reconcile` policy name stays unchanged.
- `routerctl get` and `routerctl describe` were added, splitting the CLI into
  kubectl-style verbs: `get` for desired config, `describe` for human-readable
  status/events/ledger details, and `show` for the existing combined view.
- SQLite storage was redesigned around Kubernetes-style generations, objects,
  artifacts, and events. Apply generations and events are now first-class
  records, and the previous two-table SQLite schema is migrated automatically
  into the new shape.
- `routerctl show` now uses `routerctl show <kind>` and
  `routerctl show <kind>/<name>`, combining resource spec, observed host state,
  ownership ledger entries, and routerd state history. It supports table, JSON,
  YAML, diff, ledger-only, and adoption-candidate views. NAPT/conntrack
  inspection moved under `IPv4SourceNAT` observed state.
- DHCPv6-PD state now migrates scattered prefix and identity keys into the
  structured `ipv6PrefixDelegation.<name>.lease` value.
- FreeBSD/KAME `dhcp6c` DUID files are now managed for NTT profiles whose
  effective DUID type is `link-layer`; non-DUID-LL files are backed up before
  routerd writes a MAC-derived DUID-LL.
- FreeBSD remote install hardening: `ROUTERD_OS=freebsd` now builds
  FreeBSD binaries and uses FreeBSD runtime directories even when invoked
  from a Linux workstation.
- Remote dependency checks now cover `jq`, FreeBSD `dhcp6c`, and `sysrc`.
- FreeBSD DHCPv6-PD rendering now emits KAME `dhcp6c` syntax accepted by
  the packaged client.
- FreeBSD PPPoE rendering now emits `mpd5` configuration and can start the
  `mpd5` rc.d service for managed `PPPoEInterface` sessions.
- FreeBSD apply now observes delegated prefixes from downstream
  `ifconfig` output, applies stable `IPv6DelegatedAddress` aliases, and avoids
  restarting `dhcp6c` unless its configuration changed or the service is down.
- FreeBSD apply can now derive LAN-side `IPv6DelegatedAddress` aliases from
  stored PD lease state, manages dnsmasq through a `routerd_dnsmasq` rc.d
  service, and nudges RA default-route learning with `rtsol` when no IPv6
  default route is present.
- FreeBSD `dhcp6c` is now started with `-n`, and required restarts use SIGUSR1
  before starting the service again to avoid unnecessary DHCPv6 Release
  traffic.
- FreeBSD DHCPv6-PD identity observation now records the configured IAID and
  the `dhcp6c` DUID file in routerd state.
- Resource ownership and adoption foundation: every resource kind now
  emits artifact intents, the local ownership ledger records routerd-owned
  host artifacts, `routerd adopt --candidates` reports adoption candidates
  read-only, and apply reports orphan candidates for managed routing
  and nftables artifacts.
- `routerd adopt --apply` records matching adoption candidates in the
  ledger without changing host state. Successful non-dry-run apply
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
- NixOS rendering now disables reverse-path firewall checks for router
  hosts, avoiding pre-input drops for routed and DHCPv6-PD traffic.
- The home-router firewall preset now permits WAN-side DHCPv6 client
  replies to UDP destination port 546 without constraining the server
  source port, and permits ICMPv6 control-plane traffic.
- dnsmasq rendering now suppresses IPv6 DHCP/RA scopes until a delegated
  LAN prefix is observable, allowing IPv4 DHCP and DNS to keep running
  while DHCPv6-PD is still unavailable.
- NixOSHost can now render an optional local `routerd.service` for
  source-installed lab hosts, so `routerd serve` can resume apply
  automatically after reboot without importing the flake module.
- The managed dnsmasq systemd unit no longer owns `/run/routerd`, avoiding
  accidental removal of the routerd control socket when dnsmasq is
  restarted.
- Apply now removes ledger-owned orphaned DS-Lite ipip6 tunnels before
  creating desired DS-Lite tunnels, so renaming a tunnel does not fail when
  the old tunnel still owns the same local and remote endpoints.
- The FreeBSD rc.d script now tracks the child routerd PID and redirects
  daemon output to `/var/log/routerd.log`, making `service routerd status`
  and SSH-driven starts behave normally.
- IPv4 default route selection now ignores route-set candidates whose
  target interfaces do not exist, so DS-Lite fallback can use DHCPv4
  while prefix delegation is still unavailable.
- Apply now records observed IPv6 prefix-delegation state per
  `IPv6PrefixDelegation` resource, including the current prefix, last known
  prefix, uplink/downstream interface names, and prefix length. The last
  known prefix is retained when the current prefix disappears, which is
  groundwork for DHCPv6-PD renewal behavior with home gateways that remember
  prior leases.
- IPv6 prefix-delegation state now also records DHCP identity material for
  systemd-networkd clients when observable, including IAID, DUID, textual
  networkd DUID, identity source, and the expected link-layer DUID for NTT
  profiles.
- `IPv6PrefixDelegation` can now pin DHCP identity fields with `spec.iaid`,
  `spec.duidType`, and `spec.duidRawData`. systemd-networkd renders all
  three; FreeBSD `dhcp6c` uses `iaid` for the `ia-pd` / `id-assoc pd`
  identifier.
- DS-Lite health checks now ping the AFTR from the tunnel's configured local
  IPv6 source address, and IPv4 default-route route-set candidates skip
  DS-Lite targets whose local source address cannot be resolved. This avoids
  selecting stale DS-Lite tunnels after prefix delegation disappears.

## 0.1.0 planning baseline

- Initial resource model for interfaces, static IPv4, DHCP stubs,
  plugins, dry-run, status JSON, and the systemd service layout.
