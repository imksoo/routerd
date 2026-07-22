---
title: Supported platforms
---

# Supported platforms

![Diagram showing supported platforms with Linux systemd primary integration, FreeBSD rc.d and pf groundwork, and pkg/platform feature-gated implementation rules](/img/diagrams/platforms.png)

routerd is designed to be cross-OS, but each platform uses a different host integration model. This page lists the concrete OS surfaces routerd uses on each platform, so you can review generated files and runtime ownership before applying a router configuration.

## Linux (Ubuntu / Debian)

Linux with systemd is the primary platform. Release installs land under `/usr/local` by default.
Install from the Linux release archive and run `sudo ./install.sh`.
The installer can install runtime packages with `apt-get`, `dnf`, or `pacman`.

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
- native `route -n get` evidence for health checks and BGP FIB ownership through
  `RTF_PROTO1`, including replace/withdraw/foreign-route preservation
- FRR `bfdd` reconciliation and observed Up → Down → Up recovery on a native
  FreeBSD peer
- FreeBSD-native doctor checks, KernelModule `kldload` reconciliation, and
  BGP-specific `routerd_bgp` rc.d generation
- explicit rejection of non-local DNS resolver binds because FreeBSD has no
  Linux `IP_FREEBIND` equivalent

The ARP and RA observer daemons capture through the FreeBSD base-system
tcpdump/libpcap BPF path; proactive ARP writes retain a separate direct-BPF
descriptor. The provisioned native CI exercises both daemons in a disposable
VNET and requires the expected ARP observation and rogue-RA events. The tagged
native DPI backend supports the FreeBSD ports `ndpi` 5.0 ABI and is verified by
the same native gate with a TLS/SNI classification self-test.

`ClientPolicy` is supported on FreeBSD with an address-backed pf
approximation. Each IPv4 guest or isolated classification references a
`DHCPv4Reservation`; an IPv6 guest identity must instead be declared explicitly
as `classification[].ipv6Addresses`. For a FreeBSD-targeted policy, those
stable address fields are the identity contract: MAC, OUI, hostname, and DHCP
fingerprint match selectors are rejected instead of being silently ignored.
routerd never infers an IPv6 identity from an IPv4 reservation, MAC address,
hostname, OUI, or DHCP fingerprint. The
FreeBSD renderer uses those literal IPv6 addresses for family-safe `inet6`
guest-egress deny rules. This is not Linux-equivalent MAC-based isolation: pf
does not provide the Ethernet-source matching model used by nftables in the
routed filter path. Privacy or unlisted IPv6 addresses are therefore outside
this FreeBSD ClientPolicy slice and need separate network segmentation.

FreeBSD does not use Linux-specific nftables, conntrack, or iproute2. The
`Package` examples declare FreeBSD-native replacements: `pf` and `pflog0` from
the base system, `mpd5` for PPPoE, `ifconfig gif` for DS-Lite, dnsmasq for LAN
DHCP/RA service, and ports packages for WireGuard, Tailscale, and strongSwan.

| Category | Packages |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Optional native DPI | `ndpi` |
| Diagnostics | `bind-tools` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` produces:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerctl apply` installs the generated `pf.conf`, validates it with `pfctl -nf`, applies it with `pfctl -f`, validates dnsmasq with `dnsmasq --test`, starts generated rc.d scripts with `service <name> onestart`, and applies dynamic DS-Lite tunnels with `ifconfig gif` when static `rc.conf` rendering is not enough. Use `routerd render freebsd` for review and offline validation before pointing real traffic at a FreeBSD host.

## Platform parity backlog

These are the current known level differences to track when comparing Ubuntu
and FreeBSD:

| Area | Current gap | Backlog |
| --- | --- | --- |
| CI/runtime coverage | Pull requests compile FreeBSD amd64/arm64 binaries and run a provisioned FreeBSD 14.3 amd64 VM with the full unfiltered `go test ./...`, live routerd smoke, ARP/RA observers, and native nDPI. Retained VM115 evidence additionally covers route lookup, BFD, and supported PF dataplane slices. | Native PR runtime certification is currently amd64; arm64 remains compile-only in PR CI. |
| FreeBSD feature limitations | `ClientPolicy` uses DHCPv4 reservations for IPv4 and explicit `classification[].ipv6Addresses` for IPv6 pf rules. It cannot match MAC addresses or infer IPv6 identity from DHCPv4. | Keep the explicit-address and MAC/L2 limitation visible; require separate segmentation for unlisted or privacy IPv6 addresses ([#849](https://github.com/imksoo/routerd/issues/849)). |
| Package bootstrap | Ubuntu and FreeBSD can install packages imperatively. | Keep schema, validation, installer package lists, examples, and generated docs in sync for `apt` and `pkg`. |

## Implementation guideline for OS abstraction

When you add a new OS-specific behaviour, do not branch on `runtime.GOOS` in business logic. Use the `pkg/platform` layer (`platform.Features`) or Go build tags to keep the boundaries explicit. Failing fast at validation or planning is preferred over surprising the operator at runtime on an unsupported OS.
