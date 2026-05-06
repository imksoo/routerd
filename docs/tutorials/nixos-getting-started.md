---
title: Getting started on NixOS
---

# Getting started on NixOS

NixOS is a first-class secondary platform for routerd. The recommended path on NixOS is to drive routerd-managed services from declarative NixOS configuration rather than from transient systemd units.

## Recommended starting scope

On NixOS, start by managing the daemon-based WAN services through the declarative path. DHCPv6-PD, DHCPv4 client leases, PPPoE sessions, HealthCheck, dnsmasq, and firewall logging can now be represented in the generated NixOS module. Add more router resources after the base service set reaches a clean `nixos-rebuild test`.

## Generated artefacts

routerd writes systemd units into `/etc/nixos/routerd-generated.nix`. Apply them with:

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

The generated units launch routerd daemons with explicit binary paths and the appropriate `RuntimeDirectory`, `StateDirectory`, `ProtectSystem=strict`, and capability lists.

## Why not transient units

Units placed under `/run/systemd/system` on NixOS are not part of the system configuration; a reboot or a `nixos-rebuild switch` will remove them. To survive across reboots and rebuilds, the unit has to be declared in the NixOS configuration. routerd does that by writing to `/etc/nixos/routerd-generated.nix`.

## Coverage today

What is implemented:

- systemd unit generation for `routerd-dhcpv6-client`
- systemd unit generation for `routerd-dhcpv4-client`
- systemd unit generation for `routerd-pppoe-client`
- NixOS module generation for `Package`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`
- DHCPv6-PD reaches `Bound` after `nixos-rebuild switch`
- dnsmasq, DNS resolver, HealthCheck, and firewall logger services can be declared through the generated module
- WireGuard / Tailscale / VXLAN coverage tested across NixOS / Linux / FreeBSD

What is still rolling in:

- NixOS-native renderers for every Linux runtime feature
- Integration with NixOS `generation` rollback semantics

For the per-platform breakdown, see [supported platforms](../platforms.md).

## See also

- [Install](./install.md)
- [Bring up the first router](./first-router.md)
- [WAN-side services](./wan-side-services.md)
