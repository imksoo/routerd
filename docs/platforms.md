---
title: Supported platforms
---

# Supported platforms

routerd is designed to be cross-OS, but each platform uses a different host integration model. This page lists the concrete OS surfaces routerd uses on each platform, so you can review generated files and runtime ownership before applying a router configuration.

## Linux (Ubuntu / Debian)

Linux is the primary platform. Release installs land under `/usr/local` by default.
Install from the Linux release archive and run `sudo ./install.sh`.
The installer can install runtime packages with `apt-get`, `dnf`, or `pacman`.

routerd uses the following OS surfaces on Linux:

- systemd unit files
- `/run/routerd` and `/var/lib/routerd` for runtime and persistent state
- dnsmasq for DHCPv4, DHCPv6, DHCP relay, and Router Advertisement
- nftables for filtering and NAT
- conntrack for connection observation
- iproute2 for interfaces and routes
- pppd / rp-pppoe for PPPoE
- WireGuard, Tailscale, strongSwan, radvd

Even on Ubuntu, routerd does not assume packages are pre-installed.
For first bootstrap, `install.sh` installs a practical default set.
For ongoing declarative management, declare dependencies with the `Package` resource.
The reference list:

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS control | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`, `routerd-dhcpv4-client`, `routerd-pppoe-client`, and `routerd-healthcheck` run as systemd services on Linux.

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
- NixOS module generation for `Package`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`
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

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
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
- pf NAT rendering from `IPv4SourceNAT` and `NAT44Rule`
- automatic `pfctl -nf` validation and `pfctl -f` application for generated `pf.conf`
- conntrack-equivalent traffic flows from `pfctl -ss -v`
- `pflog0` ingestion through direct BPF reads for firewall logs; packet parsing avoids dependency on vendor-specific tcpdump text formats
- managed dnsmasq for DHCPv4, DHCPv6, and Router Advertisement
- dnsmasq lease persistence under `/var/db/routerd/dnsmasq`
- dnsmasq config validation with `dnsmasq --test` before service restart
- automatic pf holes for routerd-owned DHCP, DNS, RA, DHCPv6-PD, DS-Lite, WireGuard, and healthcheck traffic
- DNS resolver daemon builds on FreeBSD; `viaInterface` can target `fib:<n>` for FIB-bound upstream routing
- cloud VPN `IPsecConnection` validates and renders strongSwan `swanctl` connection definitions; live cloud gateway validation remains deployment-specific
- rc.d script generation, installation, and `service <name> onestart` activation from `SystemdUnit`
- rc.d script generation for `routerd-healthcheck`
- rc.d script generation for `routerd-firewall-logger` with direct `pflog0` input
- rc.d script generation for `TailscaleNode`
- dnsmasq rc.d ordering after `mpd5` for PPPoE coexistence
- Static DS-Lite gif tunnel rendering
- Dynamic DS-Lite apply from static AFTR IPv6, AFTR FQDN, or delegated-address local source

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

## Implementation guideline for OS abstraction

When you add a new OS-specific behaviour, do not branch on `runtime.GOOS` in business logic. Use the `pkg/platform` layer (`platform.Features`) or Go build tags to keep the boundaries explicit. Failing fast at validation or planning is preferred over surprising the operator at runtime on an unsupported OS.
