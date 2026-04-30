# DHCPv6-PD client implementations and how to choose

routerd delegates WAN-side DHCPv6 Prefix Delegation to the operating system's DHCPv6 client.
This page summarises lab observations from a NTT FLET'S Hikari Next + PR-400NE home gateway
deployment, the differences between client implementations seen there, and why routerd's
NTT profiles recommend KAME/WIDE `dhcp6c`.

## Comparison

| Implementation | Licence | Upstream status | Behaviour under NTT NGN |
| --- | --- | --- | --- |
| KAME/WIDE `dhcp6c` (`wide-dhcpv6-client` / `net/dhcp6`) | BSD-style | Upstream `wide-dhcpv6` stopped at 2008-06-15. Distros patch-maintain. The `opnsense/dhcp6c` fork is BSD-3 and active | Carries IA Prefix lifetimes from the previous Reply into Renew/Rebind. Sends packet shapes similar to NEC IX commercial routers |
| systemd-networkd | LGPL-2.1+ | Active | Has been observed to send Renew/Rebind with IA Prefix `pltime=0 vltime=0` (systemd issue #16356). NTT HGW silently drops these as a "release-like" signal |
| dhcpcd | BSD 2-clause | Active | Handles IPv4/RA/DHCPv6/PD in one daemon. Lab tests show its Solicit includes Vendor-Class and ORO codes (e.g. opt_82, opt_83), so it is not the minimal shape that proves to work |
| odhcp6c | GPL-2.0 | Active under the OpenWrt project, rarely packaged outside OpenWrt | Widely used in OpenWrt. NTT FLET'S Cross (10G Hikari) deployments report eight-hour disconnections (OpenWrt issue #13454). Needs more validation under PR-400NE |

## Behaviour of NTT home gateways (PR-400NE family)

From lab observations and public documentation, the working model is:

1. **Acquisition window after reboot.** After a HGW reboot the LAN-side DHCPv6 server takes
   a few minutes to begin answering fresh Solicits. Once ready, even the minimal Solicit form
   gets Advertise/Reply quickly.
2. **Renew/Request always honoured during normal uptime.** Renew packets that include the
   Server Identifier and the existing IA_PD Prefix are answered by the HGW regardless of time.
   This was confirmed by observing a NEC IX router refresh its lease at the T1 boundary.
3. **`pltime=0 vltime=0` IA Prefix is silently dropped.** The HGW treats this as the client
   indicating it no longer wants the prefix. systemd-networkd has been observed sending Renew
   with `pltime=0 vltime=0` and getting no Reply.
4. **Reply source UDP port is ephemeral.** Advertise/Reply arrives at UDP destination 546 but
   the source port is not 547 (observed example: 49153). Captures filtered on `udp port 547`
   alone miss the responses; use `udp port 546 or udp port 547`.

## Failure modes

| Symptom | Where to look | Likely cause |
| --- | --- | --- |
| Solicit gets no reply right after HGW reboot | Acquisition window not yet open | HGW still preparing. Wait a few minutes and re-evaluate |
| Solicit gets no reply during normal uptime | Solicit has no Server Identifier | Client lost its lease state. Recovery typically requires a HGW reboot to reopen the acquisition window |
| Renew gets no reply during normal uptime | Renew with Server Identifier present, but no Reply | Inspect IA_PD Prefix lifetimes. If `pltime:0 vltime:0` then the client is silently dropped |
| HGW lease table has the MAC but the LAN side has no prefix | OS-side lease pickup is broken | Example: networkd + netplan logging `Could not set DHCP-PD address: Invalid argument` |

## How routerd handles this

- The default Linux client is `systemd-networkd`. NTT profiles (`ntt-ngn-direct-hikari-denwa`,
  `ntt-hgw-lan-pd`) recommend `IPv6PrefixDelegation.spec.client: dhcp6c` so that KAME/WIDE
  `dhcp6c` is used instead.
- FreeBSD always uses `dhcp6c` from `net/dhcp6`. Base `dhclient` does not handle DHCPv6-PD.
- `routerd apply` preserves the OS DHCPv6 client's in-memory lease. Services are not restarted
  unless rc.conf or drop-in files actually change.
- Observability does not treat a derived LAN address as proof of a healthy PD lease.
  Last Reply timestamp, OS client lease state, and `routerctl describe ipv6pd/<name>`
  output are the planned sources of truth.

## References

- systemd issue #16356 — DHCPv6 Renew not resetting valid/preferred lifetimes
- OpenWrt issue #13454 — odhcp6c eight-hour disconnections under NTT 10G Hikari (FLET'S Cross)
- OpenWrt forum: "Server Unicast DHCPv6 option causes my ISP to ignore Renew packages"
- `opnsense/dhcp6c` — actively maintained BSD-3 fork of the KAME WIDE-DHCPv6 client
- NEC UNIVERGE IX FLET'S IPv6 IPoE configuration guide — example of a working commercial router on the same network
