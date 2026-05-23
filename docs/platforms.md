---
title: Supported platforms
---

# Supported platforms

routerd is designed to be cross-OS, but each platform uses a different host integration model. This page lists the concrete OS surfaces routerd uses on each platform, so you can review generated files and runtime ownership before applying a router configuration.

## Linux (Ubuntu / Debian)

Linux with systemd is the primary platform. Release installs land under `/usr/local` by default.
Install from the Linux release archive and run `sudo ./install.sh`.
The installer can install runtime packages with `apt-get`, `dnf`, `apk`, or `pacman`.

routerd uses the following OS surfaces on Linux:

- systemd unit files
- `/run/routerd` and `/var/lib/routerd` for runtime and persistent state
- dnsmasq for DHCPv4, DHCPv6, DHCP relay, and Router Advertisement
- nftables for filtering and NAT
- conntrack for connection observation
- iproute2 for interfaces and routes
- long-lived `routerd-bgp` GoBGP daemon for BGP peering and route installation
- keepalived for VRRP VIP ownership
- pppd / rp-pppoe for PPPoE
- WireGuard, Tailscale, strongSwan, radvd

Even on Ubuntu, routerd does not assume packages are pre-installed.
For first bootstrap, `install.sh` installs a practical default set.
For ongoing declarative management, declare dependencies with the `Package` resource.
The reference list:

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `keepalived`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS control | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`, `routerd-dhcpv4-client`, `routerd-pppoe-client`, and `routerd-healthcheck` run as systemd services on Linux.

Ubuntu 26.04 LTS (`resolute`) has been validated with the same Linux data-plane
renderers used by Ubuntu 24.04 for managed dnsmasq, nftables, DHCPv6-PD,
delegated LAN IPv6 address derivation, and the control API. The host bootstrap
did need one OS-level networking adjustment: on interfaces that routerd owns for
DHCPv6-PD or LAN RA/DHCPv6 service, configure installer netplan/systemd-networkd
so the OS does not run its own DHCPv6 client. Otherwise systemd-networkd can
bind UDP port 546 before `routerd-dhcpv6-client`.

For Ubuntu 26.04 router lab hosts, keep only the management interface on OS
DHCP and make routerd-owned WAN/LAN interfaces link-local-only at the OS layer:

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    ens18:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens19:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens20:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

For WAN links that still need the RA-learned IPv6 default route, declare the
WAN interface and DHCPv6 / RA resources. routerd derives a systemd-networkd
drop-in with `IPv6AcceptRA=yes` and `[IPv6AcceptRA] DHCPv6Client=no`, so RA is
accepted while the OS DHCPv6 client stays disabled.

## Alpine Linux

Alpine is a Linux target for the live ISO and minimal installed hosts. It does
not have Ubuntu feature parity yet: routerd uses the Linux data plane tools
where available, but service activation is OpenRC-oriented instead of systemd.

Implemented:

- live ISO boot and USB persistence flow on Alpine
- dependency bootstrap through `install.sh` with `apk`
- platform detection through `pkg/platform` with `HasOpenRC`
- `Package` resources with `os: alpine` and `manager: apk`
- CI smoke coverage for Alpine `install.sh --list-deps` and a minimal
  `Package` validate/dry-run apply path
- `routerd render alpine --out-dir` for OpenRC scripts and dnsmasq config
- OpenRC script rendering for generated routerd service artifacts, managed
  dnsmasq, `routerd-healthcheck`, DNS resolver, firewall logger, PPPoE, and
  Tailscale
- apply-time OpenRC activation through `rc-update` / `rc-service`, with
  idempotency checks before enable/start/restart operations
- `make alpine-vm-smoke` harness for installed Alpine guests
- Linux nftables, conntrack, iproute2, dnsmasq, `routerd-bgp` GoBGP, keepalived, PPP,
  WireGuard, strongSwan, radvd, and diagnostic package names documented for Alpine

Backlog before calling Alpine equivalent to Ubuntu:

- materialize DNS resolver runtime config before activating synthesized OpenRC
  DNS resolver services; the script is rendered but not enabled or started
  immediately
- persistent installed-host networking ownership outside the live ISO bootstrap
- promote the Alpine installed-host smoke harness into a regular VM CI job
  that exercises OpenRC activation and real package-manager command paths
- richer docs for systemd-only resources that stay unsupported on OpenRC

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `keepalived`, `ppp`, `ppp-pppoe`, `wireguard-tools`, `strongswan`, `radvd` |
| Diagnostics | `bind-tools`, `iputils`, `iputils-tracepath`, `tcpdump` |
| OS control | `alpine-conf`, `kmod`, `util-linux`, `e2fsprogs`, `dosfstools`, `exfatprogs` |

## NixOS

NixOS uses the same routerd resource model as Ubuntu, but activation goes
through the NixOS module system. Instead of writing transient systemd units,
routerd targets `/etc/nixos/routerd-generated.nix` and lets
`nixos-rebuild test` / `nixos-rebuild switch` manage activation.

Implemented:

- real-machine validation for NixOS activation, reboot recovery, DHCPv6-PD,
  dnsmasq LAN service, DNS resolver, DS-Lite, nftables NAT/firewall,
  health checks, Web Console generation diffs, and OpenTelemetry export
- systemd unit generation for `routerd-dhcpv6-client`
- systemd unit generation for `routerd-dhcpv4-client`
- systemd unit generation for `routerd-pppoe-client`
- NixOS module generation for `Package` overrides, `SysctlProfile`, derived host runtime artifacts, and generated service artifacts
- automatic `nixos-rebuild test` from `routerd apply --dry-run`
- automatic `nixos-rebuild switch` from `routerd apply`
- rollback attempt with `nixos-rebuild switch --rollback` when a NixOS switch fails
- generation tracking before and after `nixos-rebuild`
- DHCPv6-PD reaches `Bound`
- generated `routerd-dnsmasq` service when DHCP or RA resources require dnsmasq
- generated `routerd-dnsmasq` service uses an absolute NixOS system-profile
  binary path and explicit root execution options so hardened systemd
  activation does not depend on shell `PATH` lookup or privilege dropping
- generated DNS resolver, HealthCheck, firewall logger, Tailscale, DHCPv4 client, DHCPv6 client, and PPPoE client services
- generated `networking.nftables.enable = true` when NAT, firewall, policy routing, or Path MTU resources require nftables
- WireGuard, Tailscale, VXLAN, and native systemd-networkd VRF generation
- Linux runtime resources that are not native NixOS network declarations are reconciled by `routerd.service` after NixOS activation

On NixOS, populate `systemd.services.routerd.path` with the commands routerd needs.
`install.sh` warns instead of calling `nix-env`, because NixOS package state should remain declarative.
When `Package` resources have `os: nixos`, routerd does **not** install packages imperatively at runtime.
It writes them to `environment.systemPackages` in `/etc/nixos/routerd-generated.nix`, then lets `nixos-rebuild` activate the system profile.

NixOS post-activation inventory:

| Area | Current owner | Notes |
| --- | --- | --- |
| Packages and routerd service path | Generated NixOS module | `Package` resources become `environment.systemPackages`; routerd does not call `nix-env`. |
| Helper daemon service definitions | Generated NixOS module | DHCPv4, DHCPv6, PPPoE, HealthCheck, firewall logger, Tailscale, and dnsmasq are expressed as systemd services in Nix. |
| nftables enablement | Generated NixOS module | NAT, firewall, policy routing, and Path MTU resources set `networking.nftables.enable = true` when needed. |
| Runtime-only network mutations | `routerd.service` after activation | Dynamic DS-Lite, transient route decisions, and other status-derived mutations still need runtime reconciliation. |
| Legacy runtime dnsmasq unit cleanup | `routerd.service` after activation | Kept temporarily to remove older `/run/systemd/system/routerd-dnsmasq.service` artifacts during migration. Remove after deployed hosts have passed one release cycle. |

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `keepalived`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS control | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD uses the same routerd resource model as Ubuntu, mapped onto FreeBSD
host mechanisms. The DHCPv6-PD client runs under `daemon(8)` and reliably keeps
a lease bound. routerd maps resources to FreeBSD-native `rc.conf`, `rc.d`,
`pf`, `mpd5`, `ifconfig`, and dnsmasq surfaces instead of using Linux tools.
Install from the FreeBSD release archive and run `sudo ./install.sh`.
The installer uses `pkg` for ports packages and leaves base-system tools alone.

Implemented:

- DHCPv6-PD daemon with persistent lease
- WireGuard interop with Linux / NixOS
- VXLAN over WireGuard
- PPPoE via generated `mpd5.conf`, `mpd_enable`, and `mpd5` service restart
- `Package` install through `pkg`
- `render freebsd --out-dir` emits `install-packages.sh` for reviewable `pkg install` bootstrap
- FreeBSD-idiomatic `rc.conf.d` output for `gateway_enable`, `ipv6_gateway_enable`, `cloned_interfaces`, `ifconfig_*`, `static_routes`, `ipv6_static_routes`, `pf_enable`, `pflog_enable`, and `mpd_enable`
- `dhclient.conf`, `mpd5.conf`, `pf.conf`, dnsmasq config, and generated `rc.d` scripts from `routerd render freebsd --out-dir`
- pf rendering from `FirewallZone`, `FirewallPolicy`, `FirewallRule`
- pf NAT rendering from `NAT44Rule`
- automatic `pfctl -nf` validation and `pfctl -f` application for generated `pf.conf`
- conntrack-equivalent traffic flows from `pfctl -ss -v`
- `pflog0` ingestion through direct BPF reads for firewall logs; packet parsing avoids dependency on vendor-specific tcpdump text formats
- managed dnsmasq for DHCPv4, DHCPv6, and Router Advertisement
- dnsmasq lease persistence under `/var/db/routerd/dnsmasq`
- dnsmasq config validation with `dnsmasq --test` before service restart
- automatic pf holes for routerd-owned DHCP, DNS, RA, DHCPv6-PD, DS-Lite, WireGuard, and healthcheck traffic
- DNS resolver daemon builds on FreeBSD; `DNSUpstream.spec.sourceInterface` can target `fib:<n>` for FIB-bound upstream routing
- cloud VPN `IPsecConnection` validates and renders strongSwan `swanctl` connection definitions; live cloud gateway validation remains deployment-specific
- rc.d script generation, installation, and `service <name> onestart` activation from generated service artifacts
- rc.d script generation for `routerd-healthcheck`
- rc.d script generation for `routerd-firewall-logger` with direct `pflog0` input
- rc.d script generation for `TailscaleNode`
- CARP-backed `VirtualAddress` in `mode: vrrp`, configured on the parent interface with `vhid`
- dnsmasq rc.d ordering after `mpd5` for PPPoE coexistence
- Static DS-Lite gif tunnel rendering
- Dynamic DS-Lite apply from static AFTR IPv6, AFTR FQDN, or delegated-address local source

`ClientPolicy` is the one firewall feature that is intentionally Linux-only
for now. It depends on nftables Ethernet source address sets for MAC-based
guest isolation. The FreeBSD pf renderer rejects the resource with an explicit
error instead of applying a weaker no-op policy.

FreeBSD does not use Linux-specific nftables, conntrack, or iproute2. The
`Package` examples declare FreeBSD-native replacements: `pf` and `pflog0` from
the base system, `mpd5` for PPPoE, `ifconfig gif` for DS-Lite, dnsmasq for LAN
DHCP/RA service, and ports packages for WireGuard, Tailscale, and strongSwan.

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` produces:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerd apply` installs the generated `pf.conf`, validates it with `pfctl -nf`, applies it with `pfctl -f`, validates dnsmasq with `dnsmasq --test`, starts generated rc.d scripts with `service <name> onestart`, and applies dynamic DS-Lite tunnels with `ifconfig gif` when static `rc.conf` rendering is not enough. Use `routerd render freebsd` for review and offline validation before pointing real traffic at a FreeBSD host.

## Platform parity backlog

These are the current known level differences to track when comparing Ubuntu,
NixOS, FreeBSD, and Alpine:

| Area | Current gap | Backlog |
| --- | --- | --- |
| CI/runtime coverage | CI runs unit tests and Linux static checks on Ubuntu. Alpine now has a host-independent installer dependency smoke plus minimal `Package` validate/dry-run coverage and an installed-host smoke harness, but Alpine activation is not yet a regular VM job. FreeBSD is cross-built in release, and NixOS activation is not yet a regular VM job. | Add FreeBSD VM, NixOS VM, and Alpine VM smoke jobs that run validate, plan, dry-run apply, real package-manager checks, service activation, and renderer syntax checks. |
| Alpine service manager | Alpine now has OpenRC rendering for generated routerd service artifacts, managed dnsmasq, `routerd-healthcheck`, DNS resolver, firewall logger, PPPoE, and Tailscale. Apply-time activation uses `rc-update` / `rc-service` and avoids duplicate enable/start/restart work when state is unchanged. DNS resolver scripts are rendered but not enabled or started until runtime config materialization is in place. | Materialize DNS resolver runtime config for OpenRC, broaden installed-host networking ownership, and promote the Alpine smoke harness to CI. |
| NixOS imperative leftovers | NixOS renders the module and lets `nixos-rebuild` activate it. Runtime-only network mutations and legacy dnsmasq unit cleanup still run from `routerd.service` after activation. The cleanup is intentionally kept for the first release that contains generated NixOS dnsmasq service ownership. | Remove legacy dnsmasq cleanup after that release cycle, reduce post-activation reconciliation where NixOS has native declarations, and keep tests around remaining runtime-only resources. |
| FreeBSD feature exceptions | `ClientPolicy` remains Linux-only because it depends on nftables Ethernet source address sets. | Keep rejecting it explicitly, and only add pf support after a design that preserves the same isolation semantics. |
| Package bootstrap | Ubuntu, Alpine, and FreeBSD can install packages imperatively; NixOS intentionally renders package declarations instead. Schema, validation, examples, installer dependency lists, and CI smoke coverage now include `apk`. | Keep schema, validation, installer package lists, examples, and generated docs in sync for `apt`, `apk`, `pkg`, and Nix declarations. |

## Implementation guideline for OS abstraction

When you add a new OS-specific behaviour, do not branch on `runtime.GOOS` in business logic. Use the `pkg/platform` layer (`platform.Features`) or Go build tags to keep the boundaries explicit. Failing fast at validation or planning is preferred over surprising the operator at runtime on an unsupported OS.
