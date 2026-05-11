---
title: Changelog
---

# Changelog

routerd release history. The format follows [Keep a Changelog](https://keepachangelog.com/).
routerd uses date-and-time-based release versions in `vYYYYMMDD.HHmm` format.
The software is at the v1alpha1 stage; releases may contain breaking changes.

## v20260511.1820

## v20260511.1709

## v20260511.1428

## v20260511.1240

## v20260511.1041

## v20260511.1017

## v20260510.1956

## v20260510.1811

### Added

- Added the PVE live ISO serial-console validation log to `internal/notes/` so the walkthrough screenshots and execution log are preserved together as test evidence.

## v20260510.1802

### Changed

- Embedded the real PVE live ISO boot screenshots in the Japanese, Simplified Chinese, and Traditional Chinese diskless mini PC walkthroughs.
- Removed stale placeholder screenshot references from the diskless mini PC walkthroughs.

## v20260510.1750

### Added

- Added real PVE live ISO screenshots to the diskless mini PC walkthrough.
- Added missing Simplified and Traditional Chinese pages for positioning, USB persistence, and legal redistribution.

### Changed

- Changed the website footer copyright text to the conventional copyright-first form.
- Updated the diskless mini PC walkthrough to use VGA plus serial console so QEMU screenshots and `qm terminal` validation can be captured in one run.

### Fixed

- Fixed the live ISO configure wizard so DHCPv4 pool defaults are derived from the selected LAN address prefix.
- Re-ran the PVE live ISO boot test with `/tmp/iso-boot-test-20260510-1742.log`, QEMU screenshots, routerd apply, Healthy status, and USB persistence flush validation.

## v20260510.1722

### Added

- Added BSD 3-Clause SPDX identifiers to routerd Go sources, installer scripts, plugin scripts, and Web Console sources.
- Added a README license badge and linked the BSD 3-Clause license from the English and Japanese READMEs.
- Added public contributing documentation and linked it from the docs sidebar.
- Added SECURITY reporting details for email and GitHub Security Advisories.

### Changed

- Unified the root `LICENSE` copyright notice as `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`.
- Clarified the legal documentation that SPDX headers apply to routerd source files only; bundled third-party software remains covered by `THIRD_PARTY_LICENSES.md`.
- Removed product comparison tables from the README and kept the positioning text focused on routerd's own scope.

## v20260510.1626

### Added

- Added a public legal and redistribution page with release checklist.
- Added Go module source URLs to the generated third-party license inventory.
- Recorded an internal license audit note for the BSD routerd binary and aggregate live ISO distribution model.

## v20260510.1612

### Added

- Added an automated third-party license inventory for Go modules and Alpine packages used by the live ISO.
- Added release archive and live ISO license notice installation paths.
- Documented routerd BSD 3-Clause licensing and live ISO aggregate-distribution handling.

## v20260510.1547

### Added

- Expanded the public positioning material around routerd's own scope and deployment spectrum.
- Expanded hardware compatibility guidance for Intel NUC, N100 mini PCs, Raspberry Pi 5, thin clients, and Proxmox VMs.
- Added Chinese hardware compatibility pages and clarified the live ISO plus USB persistence path.

## v20260510.1534

## v20260510.1508

## v20260510.1451

## v20260510.1429

## v20260510.1412

## v20260510.1354

## v20260510.1310

## v20260510.1301

## 20260510.4

## 20260510.3

## 20260510.2

## 20260510.1

## 20260510.0

## 20260509.16

### Added

- Release archives now include fixed-name aliases such as `routerd-linux-amd64.tar.gz` in addition to versioned archives.
- Fixed-name archives and their `.sha256` files are uploaded to GitHub Releases, so documentation can use `releases/latest/download/...` URLs.

### Changed

- Quick start documentation now uses stable latest-download URLs instead of hardcoded release versions.
- The release workflow opts GitHub JavaScript actions into the Node.js 24 runtime where supported.

## 20260509.15

### Added

- Added a `CI` GitHub Actions workflow for branch pushes and pull requests.
- The CI workflow runs `go test ./...`, schema checks, example validation, and the website build on Ubuntu.
- Added an optional `scripts/pre-commit.sh` hook that runs Go tests and schema checks before local commits.
- Added development documentation that explains the split between CI, pre-commit checks, and tag-driven release publishing.

## 20260509.14

### Validated

- Validated `ClientPolicy` guest mode on router05, an Ubuntu lab router.
- Confirmed Linux nftables renders include-mode guest MAC sets, guest DNS/DHCP/NTP access, self-isolation, and RFC 1918 / ULA deny rules.
- Confirmed exclude-mode rendering with the focused nftables renderer test.

## 20260509.13

### Added

- Expanded the guest mode guide with use cases, implementation details, full `ClientPolicy` field reference, verification steps, troubleshooting, and security limits.
- Added documented examples for include mode, exclude mode, multiple guest devices, custom deny/allow lists, local discovery services, and IoT reservations.
- `ClientPolicy.spec.guestServices` now accepts `mdns` and `ssdp` in addition to `dhcp`, `dns`, and `ntp`.

## 20260509.12

### Added

- Added `ClientPolicy`, a Linux nftables-backed guest mode that classifies LAN clients by MAC address.
- Guest clients can keep DNS, DHCP, and NTP access while private IPv4 and ULA IPv6 destinations are denied by default.
- Added `examples/guest-mode.yaml` and documentation for include-mode and exclude-mode client classification.

### Changed

- FreeBSD pf now rejects `ClientPolicy` explicitly because pf does not provide the same MAC-based routed filtering model.

## 20260509.11

### Added

- Added focused example configurations for minimal Tailscale mesh membership, WireGuard hub-spoke routing, a VRF lab, and multi-WAN home fallback.
- Added `examples/README.md` to explain when each example should be used.

### Changed

- `make validate-example` now validates every YAML file under `examples/`.

## 20260509.10

### Added

- Web Console overview now shows browser-session trend charts for generation, resource phases, and HealthCheck state.
- The Config page can compare the current YAML file with the latest applied generation before an operator runs `routerd apply`.
- Resource tables now support kind/name/phase/detail search, phase filtering, and match highlighting.
- VPN pages now include visual peer status strips for Tailscale and WireGuard.

## 20260509.9

### Added

- Release archives now carry a `share/doc/TARGET` marker, and `install.sh` checks the archive OS and architecture against the host.
- GitHub Actions now builds Linux and FreeBSD archives for both `amd64` and `arm64`.
- Release CI runs `shellcheck` against the installer and uninstaller scripts.

### Changed

- `install.sh --list-deps` now prints a structured dependency plan with OS, architecture, package manager, packages, and checked commands.
- Installer dependency sets were expanded for practical router use, including PPPoE, RA, IPsec, packet capture, routing, and firewall tooling.

## 20260509.8

### Fixed

- Fixed zh-Hant and zh-Hans documentation links so translated pages no longer point at missing locale-local documents.
- Kept translated overview pages linked to the canonical English reference pages until full translations are available.

## 20260509.7

### Added

- Multi-stage WAN fallback can now model DS-Lite primary tunnels, RA-sourced DS-Lite, PPPoE, and direct WAN fallback candidates through `EgressRoutePolicy`.
- OpenTelemetry deployment was extended across the router fleet with declarative `Telemetry` resources and OTLP environment propagation.
- DS-Lite examples now use the RFC 6333 B4-AFTR link prefix `192.0.0.0/29` for tunnel inner IPv4 source addresses.
- `PPPoEInterface.disabled` and disabled route-policy candidates keep PPPoE fallback definitions in YAML without leaking a production PPPoE session.

### Changed

- Release versions moved away from `0.x.y` and toward date-based values.
- `routerd --version`, `routerctl --version`, and release archives now use the same release tag value.
- NAT44 rendering was tightened around per-interface rules on Linux nftables and FreeBSD pf.
- The 3-role firewall model was verified on Linux and FreeBSD, with service holes bound to the owning ingress interface instead of broad multi-interface zones.
- FreeBSD pf gained TCP MSS clamp rendering for `PathMTUPolicy`, aligning it with Linux nftables behavior.
- dnsmasq RA generation now propagates path MTU through the IPv6 RA MTU option.

### Fixed

- FreeBSD pf service-hole rendering no longer expands DHCPv6, WireGuard, and VXLAN holes across every member of the `wan` zone.
- FreeBSD NAT artifacts are reported as `pf.anchor/routerd_nat` instead of nftables artifacts.
- PPPoE interface aliases are resolved to the real OS interface name before NAT rendering.

## 0.4.0

### Added

- The implicit-deny log lines from nftables are now ingested by `routerd-firewall-logger` and stored in `firewall-logs.db`. On Linux the logger reads `nfnetlink` directly; on FreeBSD it consumes `pflog` directly through BPF.
- The Web Console gained a Connections tab (live conntrack / pf state), a Clients tab (DHCP lease + traffic statistics combined), and a Firewall tab (deny ranking plus a per-second timeline).
- `TailscaleNode` can now advertise a router as a Tailscale exit node and subnet router through a generated systemd unit. NixOS rendering enables `services.tailscale` and includes the generated unit path.
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
