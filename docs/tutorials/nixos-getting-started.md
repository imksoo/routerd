---
title: Getting started on NixOS
---

# Getting started on NixOS

NixOS is a first-class secondary platform for routerd. The recommended path on NixOS is to drive routerd-managed services from declarative NixOS configuration rather than from transient systemd units.

## Recommended starting scope

On NixOS, start by managing the DHCPv6-PD client through the declarative path. This is the most fully covered NixOS integration today and gives you observable end-to-end behaviour. Other resources can be added as the corresponding NixOS module support lands.

## Generated artefacts

routerd writes systemd units into `/etc/nixos/routerd-generated.nix`. Apply them with:

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

The generated unit launches `routerd-dhcpv6-client` with an explicit binary path and the appropriate `RuntimeDirectory`, `StateDirectory`, `ProtectSystem=strict`, and capability list.

## Why not transient units

Units placed under `/run/systemd/system` on NixOS are not part of the system configuration; a reboot or a `nixos-rebuild switch` will remove them. To survive across reboots and rebuilds, the unit has to be declared in the NixOS configuration. routerd does that by writing to `/etc/nixos/routerd-generated.nix`.

## Coverage today

What is implemented:

- systemd unit generation for `routerd-dhcpv6-client`
- NixOS module generation for `Package`, `SysctlProfile`, `NetworkAdoption`, `SystemdUnit`
- DHCPv6-PD reaches `Bound` after `nixos-rebuild switch`
- WireGuard / VXLAN coverage tested across NixOS / Linux / FreeBSD

What is still rolling in:

- nftables / dnsmasq / DNS resolver / HealthCheck end-to-end
- `Package` resolution for the full Ubuntu reference list
- Integration with NixOS `generation` rollback semantics

For the per-platform breakdown, see [supported platforms](../platforms.md).

## See also

- [Install](./install.md)
- [Bring up the first router](./first-router.md)
- [WAN-side services](./wan-side-services.md)
