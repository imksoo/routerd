---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records the
behavior changes and new resource shapes as the model takes shape.

## Unreleased

## 0.1.0 (2026-05-01)

First tagged release. The model and apply path now cover a 5-node
Linux/NixOS/FreeBSD lab in which one VXLAN overlay reaches a 25/25 ping
matrix and survives a router reboot. The `v1alpha1` API surface (router,
net, system, firewall) is the working baseline for that experience; later
releases may rename or replace any of these shapes without a deprecation
shim while routerd is in this clarity-first phase.

- Apply now auto-allows VXLAN underlay UDP and bridge ICMP in
  `inet routerd_filter` so a default-deny FirewallPolicy no longer drops
  the overlay control path. The bridge L2 filter rules render with
  nftables `counter` so DHCP/RA/ND drop hits are observable.
- Apply detects newly written systemd-networkd `.netdev` files and runs
  `systemctl restart systemd-networkd` instead of `networkctl reload`,
  because reload alone does not always materialize new VXLAN/bridge
  netdevs on every distribution.
- `routerd render nixos` now emits `networking.firewall.allowedUDPPorts`
  for each VXLANSegment underlay UDP port and
  `networking.firewall.trustedInterfaces` for every bridge that a
  VXLANSegment explicitly attaches to via `spec.bridge`. Bridges without
  a VXLAN attachment stay subject to the default firewall policy.
- The NixOS VXLAN netdev now renders `Independent = true` and a per-host
  `routerd-vxlan100-fdb` oneshot service that appends
  `00:00:00:00:00:00 dst <peer>` flood FDB entries for every configured
  remote, so multi-peer VXLAN overlays survive `nixos-rebuild` and host
  reboots.
- `routerd apply` skips the live network stage on NixOS and emits an
  info note pointing at `routerd render nixos` + `nixos-rebuild switch`
  as the persistent path. Other stages (nft, dnsmasq, runtime) continue
  to apply normally.
- `VXLANSegment`, `Bridge`, `IPv4StaticRoute`, `IPv6StaticRoute`, and
  `DHCPv4HostReservation` now declare ownership intents
  (`net.link`, `net.ipv4.route`, `net.ipv6.route`,
  `nft.table routerd_l2_filter`, `dnsmasq.dhcpv4.host`), so the orphan
  check no longer reports their managed objects as unowned. A
  VXLANSegment with `l2Filter: none` does not claim the bridge L2
  filter table.
- The FreeBSD VXLAN renderer no longer hard-fails when more than one
  remote is configured. It uses the first remote as the seed, ignores
  the rest, and surfaces the limitation as a `FreeBSDConfig.Warnings`
  entry that the applier propagates to `result.Warnings` and the event
  log.
- `make remote-install` now runs `sysrc routerd_enable=YES` post-extract
  on FreeBSD remotes, so `service routerd ...` works without the rc.d
  enable warning.
- `make check-remote-deps` now treats missing `mstpd` / `mstpctl` as a
  warning instead of a hard error. systemd-networkd falls back to
  in-kernel STP, which is correct on Ubuntu noble and other
  distributions that no longer ship the WIDE mstpd package.
- FreeBSD apply now records the routerd-managed sysrc keys it wrote in
  the routerd state store and runs `sysrc -x` for any keys present in
  the previous set but absent from the current render, so renaming a
  VXLAN/bridge interface no longer leaves stale `ifconfig_<old>` keys
  in `/etc/rc.conf`.
- `DnsmasqConfig` now returns warnings instead of erroring when a
  dnsmasq rule depends on an upstream DHCP lease that the router has
  not yet observed (`DNSConditionalForwarder.upstreamSource: dhcp4|dhcp6`,
  `IPv4DHCPScope.dnsSource: dhcp4`, `IPv4DHCPServer.dns.upstreamSource:
  dhcp4`). The missing rule is skipped and re-rendered on the next
  apply.
- A new `nixos-getting-started` section (3.4) documents that NixOS
  `networking.firewall` (`nixos-fw` chain) runs alongside routerd's
  nftables tables and is not bypassed; the page lists the expected
  `allowedUDPPorts` / `trustedInterfaces` settings and the diagnostic
  pattern when underlay packets are visible to tcpdump but never reach
  the routerd input chain.
- Added a `VXLANSegment` resource with Linux systemd-networkd, FreeBSD, and
  NixOS render paths. Linux also renders a default bridge-family nftables
  L2 filter that blocks DHCPv4, DHCPv6, RA, and neighbor discovery on VXLAN
  ports unless `spec.l2Filter: none` is set.
- Added `role: server|transit` to DHCPv4 and DHCPv6 server resources so
  shared L2 segments can name one designated DHCP/RA server while other
  routerd hosts remain transit-only.
- Added a `Bridge` resource with conservative STP/RSTP defaults and
  multicast-snooping disabled by default for virtualized IPv6 labs.
- Added `IPv4StaticRoute` and `IPv6StaticRoute` resources for explicit static
  routes on Linux systemd-networkd, FreeBSD, and NixOS render paths.
- Added `DHCPv4HostReservation` for dnsmasq-backed fixed IPv4 leases inside
  an existing `IPv4DHCPScope`.
- SQLite state objects now include `last_applied_path` metadata. This prepares
  routerd for kubectl-style additive apply and explicit delete workflows.
- Successful apply runs now populate `last_applied_path` for each resource in
  the SQLite state database.
- `routerd apply` and `routerctl apply` are additive: they update submitted
  resources and leave omitted, previously managed resources in place.
- `routerd delete <kind>/<name>` and `routerd delete -f <router.yaml>` now
  remove the selected resource objects from state and clean up matching
  routerd-owned artifacts from the ownership ledger.
- `routerctl delete <kind>/<name>` now calls the daemon delete endpoint, and
  `routerctl describe orphans` lists routerd-owned orphaned artifacts without
  removing them.
- `routerd serve` now observes WAN Router Advertisements for
  `IPv6PrefixDelegation`, accepts DHCPv6 client hook events over the local
  control API, tracks acquisition phase and stalled-renewal suspicion, and
  exposes those details in `routerctl describe ipv6pd/<name>`.
- Documentation now clarifies that `acquisitionStrategy: hybrid` observes the
  OS client's first Solicit path and only escalates to routerd's raw
  Request-with-claim helper after the retry budget is exhausted.
- `make check-remote-deps` now uses `CONFIG` or the remote router.yaml to make
  optional dependency checks resource-aware, so `pppd` is required only when a
  `PPPoEInterface` is configured and Linux `dhcp6c` is required only when that
  fallback client is selected.
- The NixOS renderer now rejects explicit `client: dhcp6c` because nixpkgs does
  not provide a built-in WIDE dhcp6c package path; NixOS NTT-profile examples
  use the `dhcpcd` default instead.
- `routerctl describe ipv6pd/<name>` now shows DHCPv6 identity, last
  Solicit/Request/Renew/Rebind/Release timestamps, T1/T2, preferred and valid
  lifetimes, and calculated lease deadlines.
- `routerd dhcp6` now supports `solicit` and `rebind` in addition to
  `request`, `renew`, and `release`. Solicit can be sent without a prior
  prefix or server identifier; Rebind omits Server Identifier while preserving
  non-zero IA_PD lifetimes.
- DHCPv6 active-control packets sent by routerd are now summarized into
  `IPv6PrefixDelegation` status as recent transactions so operators can see
  exactly which message, transaction ID, IAID, lifetimes, and warning markers
  were used.
- `routerd serve` now starts a passive DHCPv6 packet recorder for
  `IPv6PrefixDelegation` on supported platforms. The Linux implementation uses
  AF_PACKET to observe UDP 546/547 without binding those ports, and records
  observed transactions into the same status history.
- The passive DHCPv6 recorder now ignores DHCPv6 packets whose Client DUID
  does not match the resource's observed or expected DUID, keeping neighboring
  routers' traffic out of `routerctl describe ipv6pd/<name>`.
- The passive DHCPv6 packet recorder now has a FreeBSD BPF backend, so
  FreeBSD routers can record DHCPv6 transactions without binding UDP 546/547.
- WAN RA observation now uses the FreeBSD BPF backend as well, allowing
  FreeBSD routers to populate `wanObserved.*` and derived HGW Server ID state.
- `IPv6PrefixDelegation.spec.recovery.mode` now controls daemon-side hung
  recovery. The default `manual` mode records warnings only; `auto-request`
  and `auto-rebind` send rate-limited active DHCPv6 packets after hung
  detection and stop after three failed attempts.
- NixOS rendering now uses the same effective IPv6PrefixDelegation client
  default as apply, so omitted NTT-profile clients render `dhcpcd` packages
  and avoid enabling systemd-networkd DHCPv6-PD.
- Switching IPv6PrefixDelegation away from systemd-networkd now writes
  neutralizing networkd drop-ins for the WAN and delegated LAN interfaces, so
  stale `90-routerd-dhcp6-pd.conf` files cannot keep networkd sending DHCPv6-PD
  packets in parallel with `dhcp6c` or `dhcpcd`.
- The systemd-networkd renderer now resolves the same effective
  `IPv6PrefixDelegation` client default as apply. NTT-profile resources with an
  omitted client no longer render networkd DHCPv6-PD blocks; only the
  neutralizing drop-in remains as a stale-file guard.
- `routerd apply` now clears observed DHCPv6 identity fields in
  `IPv6PrefixDelegation` status when the effective client changes, preventing
  stale networkd IAID/DUID values from appearing after a move to `dhcpcd` or
  `dhcp6c`.
- Linux NTT-profile `IPv6PrefixDelegation` now defaults to `client: dhcpcd`,
  including on NixOS. `client: dhcp6c` remains a supported explicit fallback
  for migration and controlled comparison, but new examples should not select
  it by default.
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
- `IPv6PrefixDelegation` can now use `client: dhcpcd`. It is the Linux
  NTT-profile default and remains an explicit lab path on FreeBSD. routerd
  renders a per-resource `dhcpcd.conf`, hook placeholder, and either a systemd
  unit or FreeBSD rc.d script.
- Linux DHCPv6-PD client switching now stops stale managed units for the
  previous client, and the generated dhcpcd hook is file-global so dhcpcd 10
  actually invokes routerd's local event reporter.
- Documentation now includes Mermaid diagrams for the NTT HGW state model,
  the routerd DHCPv6-PD acquisition strategy, and the OS/client selection
  matrix, plus updated dhcpcd lab notes.
- `routerd apply` now resolves an omitted `IPv6PrefixDelegation.spec.client`
  from the host OS and profile, supports `--override-client` and
  `--override-profile` for one-shot lab runs, and records known-bad
  OS/client/profile combinations as warnings instead of validation failures.
- `routerd dhcp6 request|renew` can now override requested T1/T2 and IA Prefix
  lifetimes for lab packets. This is used to test whether an upstream DHCPv6-PD
  server honours shorter leases before waiting for a full production T1 cycle.
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
