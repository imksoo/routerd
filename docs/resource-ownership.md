---
title: Resource ownership
slug: /reference/resource-ownership
---

# Resource ownership and the apply model

routerd associates host-side artefacts with the resource that produced them. Recording who owns what makes diffs, deletions, and incident debugging tractable.

## Ownership categories

| Category | Meaning |
| --- | --- |
| Created | routerd produced the artefact itself. |
| Adopted | routerd took over an existing artefact and now manages it. |
| Observed | routerd only reads the state; it does not change it. |

## Resource → host artefact map

| Resource | Host artefact |
| --- | --- |
| `Interface` | OS interface name and admin state |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` socket, lease, events |
| `DHCPv4Lease` | `routerd-dhcpv4-client` socket, lease, events |
| `PPPoESession` | `routerd-pppoe-client` socket, state, pppd/ppp config |
| `HealthCheck` | `routerd-healthcheck` socket, state, events |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | Managed dnsmasq configuration |
| `DNSZone` | `routerd-dns-resolver` local authoritative zone |
| `DNSResolver` | `routerd-dns-resolver` socket, state, events, listener configuration |
| `DSLiteTunnel` | Linux `ip6tnl` interface |
| `IPAddressSet` | nftables IPv4/IPv6 named sets when referenced by a Linux renderer |
| `IPv4Route` | Kernel route |
| `NAT44Rule` | nftables `routerd_nat` table |
| `PortForward` / `IngressService` | Linux nftables `routerd_nat` / `routerd_filter` DNAT, optional hairpin SNAT, or FreeBSD `pf.conf` `rdr pass` / optional NAT reflection rules |
| `BGPRouter` / `BGPPeer` | FRR BGP configuration under `/run/routerd/frr/routerd.conf`, applied with `frr-reload.py`; `/etc/frr/daemons` `bgpd` / `bfdd` toggles and `frr.service` restart when BFD daemon state changes |
| `VirtualIPv4Address` | Static VIP through `ip addr` or `ifconfig`; VRRP VIP ownership through keepalived on Linux or CARP on FreeBSD |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard configuration |
| `TailscaleNode` | `routerd-tailscale-<name>` service unit/script and `tailscale up` arguments |
| `VRF` | Linux VRF device and routing table |
| `VXLANTunnel` | VXLAN device |
| `Package` | apt / apk / dnf / pkg / Nix install state |
| `Sysctl` | One sysctl value |
| `SysctlProfile` | A set of sysctl values |
| `KernelModule` | Runtime module load state and `/etc/modules-load.d/90-routerd-<name>.conf` on Linux |
| `NetworkAdoption` | systemd-networkd / systemd-resolved drop-ins |
| `SystemdUnit` | systemd unit, FreeBSD rc.d script, or OpenRC init script and enabled state |
| `NTPClient` | NTP client configuration |

## How removal works

routerd does **not** silently delete artefacts it does not own. When a resource is removed from the YAML, only artefacts that routerd previously created (or explicitly adopted) are eligible for deletion.

Full configuration rollback is not a current goal. For changes that affect production traffic, follow this order:

1. Validate.
2. Inspect the plan.
3. Run a dry-run apply.
4. Confirm the management connection survives the change.
5. Apply.
6. Verify state and connectivity.

## Legacy configurations

Older experimental DHCPv6 packages and renderers have been removed. The current DHCPv6-PD path is `routerd-dhcpv6-client`. Examples that referenced `dhcpcd` or `dhcp6c` directly are no longer part of the supported configuration set; the legacy resources have been retired without aliases.
