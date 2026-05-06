---
title: Host inventory
slug: /operations/inventory
---

# Host inventory

routerd inspects the host's operating system, available commands, and network features. This inventory is used by the renderers and the apply path to make OS-specific decisions explicit instead of failing at runtime.

## What routerd checks

- Operating system and release
- Service-management scheme (systemd, rc.d, NixOS modules)
- Available commands (iproute2, nftables, conntrack, dnsmasq, radvd, pppd, WireGuard, strongSwan, etc.)
- Kernel features (IPv6, VRF, VXLAN, WireGuard)
- Whether `/run/routerd` and `/var/lib/routerd` are usable

## How it informs behaviour

- On Ubuntu, routerd targets systemd and the Linux networking stack.
- On NixOS, declarative generation takes priority over runtime mutation.
- On FreeBSD, routerd uses `daemon(8)` and rc.d for service control.

If a configuration depends on a feature the host does not provide, routerd reports the gap during validation or planning rather than failing halfway through `apply`.

## Common commands routerd looks for

| Command | Purpose |
| --- | --- |
| `ip`, `bridge` | Addresses, routes, DS-Lite, VRF, VXLAN |
| `nft` | NAT, firewall, route marks |
| `dnsmasq` | DHCPv4, DHCPv6, RA |
| `conntrack` | IPv4/IPv6 connection observation |
| `pppd`, `ppp` | PPPoE |
| `wg` | WireGuard |
| `swanctl` | IPsec |
| `radvd` | Optional radvd RA path |
| `sysctl` | Kernel settings |
| `systemctl`, `resolvectl`, `networkctl`, `journalctl` | systemd environment management |
| `service`, `sysrc`, `pfctl` | FreeBSD environment management |
| `dig`, `ping`, `ping6`, `tcpdump`, `tracepath`, `traceroute`, `netstat`, `sockstat` | Diagnostics |

## See also

- [Supported platforms](../platforms.md)
- [Reconcile and removal](./reconcile.md)
