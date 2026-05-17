# router06 Ubuntu 26.04 lab VM - 2026-05-17

## Summary

- Created `router06` on `pve05`.
- VMID: `151`
- OS: Ubuntu 26.04 LTS (`resolute`)
- Install ISO: `qnap:iso/ubuntu-26.04-live-server-amd64.iso`
- Disk: `local-lvm:vm-151-disk-0`, 32 GiB
- CPU/memory: 4 vCPU, 4096 MiB
- Boot: `scsi0`

## Network

VM NIC layout mirrors `router05`:

| Guest | MAC | PVE bridge | Purpose |
| --- | --- | --- | --- |
| `ens18` | `BC:24:11:06:18:00` | `vmbr0` | WAN-side lab link |
| `ens19` | `BC:24:11:06:19:00` | `svnet3` | LAN-side lab link |
| `ens20` | `BC:24:11:06:20:00` | `svnet1` | management |

Management address after DHCP:

- `192.168.123.111/24` on `ens20`

`ens18` and `ens19` are configured in installer netplan with `dhcp4: false`,
`dhcp6: false`, `accept-ra: false`, and IPv6 link-local only. This prevents
systemd-networkd from occupying UDP/546 so `routerd-dhcpv6-client` can own
DHCPv6-PD like `router05`.

routerd then adopts `ens18` with a systemd-networkd drop-in that re-enables
IPv6 RA while keeping `DHCPv6Client=no`. This is required for the NGN IPv6
default route and for reaching the DHCPv6 Information DNS server used to
resolve the AFTR name.

## routerd

Installed current Linux binaries under `/usr/local/sbin` and config at:

- `/usr/local/etc/routerd/router.yaml`

Local source config:

- `local/router06.yaml`

The config was copied from `local/router05.yaml` with these lab-safe changes:

- `router05` renamed to `router06`
- `routerd.node=router06`
- LAN IPv4 subnet changed from `192.168.160.0/24` to `192.168.161.0/24`
- router LAN IPv4 changed from `192.168.160.5` to `192.168.161.6`
- VXLAN test subnet changed from `10.99.100.0/24` to `10.99.101.0/24`
- LAN IPv4 is explicitly managed with `IPv4StaticAddress/lan-ipv4` so
  `routerd-dns-resolver` can bind `192.168.161.6:53`.
- The DNSResolver AFTR forwarder includes both `gw.transix.jp` and
  `transix.jp`, with upstreams sourced from `DHCPv6Information/wan-info`.

Ubuntu 26.04 worked with the same Linux data-plane renderer paths used by
router05 on Ubuntu 24.04 for managed dnsmasq, nftables, DHCPv6-PD, delegated
LAN IPv6 addressing, and the control API.

The only compatibility adjustment was OS bootstrap state: Ubuntu 26.04's
systemd-networkd could bind UDP/546 on `ens18`/`ens19` unless installer netplan
explicitly set `accept-ra: false`. Keeping routerd-owned WAN/LAN links
link-local-only at the installer netplan layer leaves DHCPv6-PD ownership to
`routerd-dhcpv6-client`. routerd then uses NetworkAdoption to re-enable WAN RA
without re-enabling systemd-networkd's DHCPv6 client.

## Validation

After reboot:

- SSH as `nwadmin@192.168.123.111` works.
- `qemu-guest-agent.service` is active.
- `routerd.service` is active.
- `routerd-dhcpv6-client@wan-pd.service` is active.
- `routerd-dnsmasq.service` is active.
- `routerctl status` reports `Healthy`.
- DHCPv6-PD bound prefix observed: `2409:10:3d60:1280::/60`
- LAN delegated address observed on `ens19`: `2409:10:3d60:1281::1/64`
- `routerd-dns-resolver` listens on `127.0.0.1:53`, `192.168.161.6:53`, and
  `2409:10:3d60:1281::1:53`.
- `dig AAAA gw.transix.jp @127.0.0.1` resolves through the DHCPv6 Information
  DNS server and returns Transix AFTR IPv6 addresses.
- `DSLiteTunnel/ds-lite` is `Up` with device `ds-routerd-test`.
- `routerctl diagnose egress ipv4-default` reports `Applied`, selected
  candidate `ds-lite`, selected device `ds-routerd-test`.
- `NAT44Rule/lan-to-wan` is `Active` and nftables table `ip routerd_nat`
  exists.

## Systemd and journal checks

- `systemctl --failed` reports no failed units.
- `routerd.service`, `routerd-dhcpv6-client@wan-pd.service`, and
  `routerd-dnsmasq.service` are active with `Result=success` and
  `NRestarts=0`.
- `routerd-dnsmasq.service` now has
  `ConditionPathExists=/run/routerd/dnsmasq.conf`. On boot it cleanly skips
  until routerd materializes the runtime dnsmasq config, then starts without
  the previous `cannot read /run/routerd/dnsmasq.conf` failure.
- Remaining current-boot warnings are host/VM noise or expected lab traffic:
  ACPI/MMCONFIG, SCSI/multipath ID warnings, cron `EXTRA_OPTS`, early
  systemd-networkd nftables capability warning, and firewall deny logs for WAN
  DHCP broadcasts.
