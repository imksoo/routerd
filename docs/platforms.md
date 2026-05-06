---
title: Supported platforms
---

# Supported platforms

routerd is designed to be cross-OS, but the implementation is at different maturity levels per platform. This page lists what is implemented, what is groundwork, and what is out of scope, so you can pick a platform with a clear understanding of the current limits.

## Linux (Ubuntu / Debian)

Linux is the primary platform. Source installs land under `/usr/local` by default.

routerd uses the following OS surfaces on Linux:

- systemd unit files
- `/run/routerd` and `/var/lib/routerd` for runtime and persistent state
- dnsmasq for DHCPv4, DHCPv6, DHCP relay, and Router Advertisement
- nftables for filtering and NAT
- conntrack for connection observation
- iproute2 for interfaces and routes
- pppd / rp-pppoe for PPPoE
- WireGuard, strongSwan, radvd

Even on Ubuntu, routerd does not assume packages are pre-installed. Declare dependencies with the `Package` resource. The reference list:

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS control | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`, `routerd-dhcpv4-client`, `routerd-pppoe-client`, and `routerd-healthcheck` run as systemd services on Linux.

## NixOS

NixOS is a first-class secondary platform. Instead of writing transient systemd units, routerd targets `/etc/nixos/routerd-generated.nix` and lets `nixos-rebuild test` / `nixos-rebuild switch` manage activation.

Implemented:

- systemd unit generation for `routerd-dhcpv6-client`
- NixOS module generation for `Package`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`
- `nixos-rebuild test` / `nixos-rebuild switch` integration
- DHCPv6-PD reaches `Bound`
- WireGuard and VXLAN coverage
- Partial VRF coverage

Not yet covered:

- Materialising every NixOS module to a running host (some modules are generated but not yet applied automatically)
- nftables, dnsmasq, DNS resolver, HealthCheck and other long-running daemons end-to-end
- Integration with NixOS `generation` rollback semantics

On NixOS, populate `systemd.services.routerd.path` with the commands routerd needs. When `Package` resources have `os: nixos`, routerd does **not** install packages at runtime. `routerd render nixos` produces the `environment.systemPackages` list instead.

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS control | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD is the other secondary platform. The DHCPv6-PD client runs under `daemon(8)` and reliably keeps a lease bound. Most generators have a working render path, but production-grade application is still maturing.

Implemented:

- DHCPv6-PD daemon with persistent lease
- WireGuard interop with Linux / NixOS
- VXLAN over WireGuard
- PPPoE skeleton
- `Package` install through `pkg`
- pf rendering from `FirewallZone`, `FirewallPolicy`, `FirewallRule`
- pf NAT rendering from `IPv4SourceNAT` and `NAT44Rule`
- automatic `pfctl -nf` validation and `pfctl -f` application for generated `pf.conf`
- conntrack-equivalent traffic flows from `pfctl -ss -v`
- `pflog0` ingestion via `tcpdump` for firewall logs
- rc.d script generation, installation, and `service <name> onestart` activation from `SystemdUnit`
- Static DS-Lite gif tunnel rendering

Not yet covered:

- Full FreeBSD-idiomatic network configuration generation
- Dynamic DS-Lite from AFTR FQDN or delegated address
- Vendor-specific pf log format variants
- DNS resolver, HealthCheck, and DHCP server long-running daemons on FreeBSD

FreeBSD does not use Linux-specific nftables, conntrack, or iproute2. The `Package` examples for FreeBSD only cover what is already ported or has a working skeleton.

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` produces:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `rc.d-*`

`routerd apply` installs the generated `pf.conf`, validates it with `pfctl -nf`, applies it with `pfctl -f`, and starts generated rc.d scripts with `service <name> onestart` when they are not already running. Use `routerd render freebsd` for review and offline validation before pointing real traffic at a FreeBSD host.

## Implementation guideline for OS abstraction

When you add a new OS-specific behaviour, do not branch on `runtime.GOOS` in business logic. Use the `pkg/platform` layer (`platform.Features`) or Go build tags to keep the boundaries explicit. Failing fast at validation or planning is preferred over surprising the operator at runtime on an unsupported OS.
