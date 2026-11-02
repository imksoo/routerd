# routerd 1.5 Linux DHCP, DNS, NAT and firewall PVE qualification

Date: 2026-11-02

## Scope and result

This record covers the Linux candidate built from source revision
`3763f1025592b3b9b7ffa6aa361e0d87333e99c8`. It is a scoped pass for
the recorded DHCPv4-server, lease-derived DNS, UDP upstream forwarding,
NAT44 masquerade, WAN-to-LAN DNS denial, service restart, and declared
configuration withdrawal/recovery paths. It is not a whole-feature,
whole-platform, or routerd 1.0 parity verdict.

The revision fixes an observed ownership regression: an exact routerd-generated
dnsmasq systemd unit from before `PartOf=routerd.service` was incorrectly
reported as foreign. The fix accepts only that byte-exact predecessor; modified
or otherwise foreign units remain rejected.

## Environment

- PVE VLAN 3131: VM219 candidate LAN and independent VM224 BusyBox DHCP client.
- PVE VLAN 3132: VM219 candidate WAN and independent VM220 DNS/HTTP peer.
- VM219 candidate binaries:
  - `routerd`: `96be90aeccec53eab640110c0beb746c75289360142a8f394ad2ad342e17e302`
  - `routerctl`: `20530210d9636b3fe4d81f7d9056a65fd6865205f4cebe60a90538fe483da019`
  - `routerd-dns-resolver`: `9fb2e46e077e534dfef60597f436968f32d28a95abca5f3ab684a737acbcf086`
  - `routerd-dhcp-event-relay`: `b04477afb47e55dbd6fbb1f5265f0fdcf5d0cf2a10bd50057bef29791bd137f4`
- Candidate interfaces and addresses: `rd15-lan` / `198.18.15.1/24` and
  `rd15-wan` / `198.18.16.2/24`.
- DHCPv4 pool: `198.18.15.100` through `198.18.15.110`, with gateway and DNS
  `198.18.15.1`, domain `home.test`, and a 90-second configured lease.
- VM220 supplied UDP DNS `*.upstream.test -> 198.18.92.2` and an HTTP endpoint
  returning the observed source address.

## Observed communication

- VM224 obtained `198.18.15.104` from `198.18.15.1`, including the declared
  gateway, DNS server, `home.test` domain, and a 120-second effective lease.
- `dhcpclient.home.test` returned `198.18.15.104` after the DHCP lease event.
  UDP and TCP DNS replies were both observed.
- VM224 resolved new `*.upstream.test` names to `198.18.92.2` through the
  candidate resolver. The resolver runtime configuration recorded
  `viaInterface: rd15-wan` for the logical `sourceInterface: wan` declaration.
- VM224 HTTP traffic to VM220 returned `198.18.16.2`, proving NAT44 masquerade.
- From VM220, a routed UDP DNS request to the LAN resolver timed out. The
  candidate `wan_to_self` firewall counter increased, while the LAN client
  continued to resolve the local name.
- After `systemctl restart routerd`, `routerd` and `routerd-dnsmasq` were
  active. VM224 reacquired `198.18.15.104`, then local DNS, upstream DNS, and
  NAT44 HTTP succeeded again.
- A running VM224 client sent `DHCPRELEASE`. routerd recorded
  `routerd.dhcp.lease.removed`; the resolver retained a removal observation and
  `dhcpclient.home.test` returned no answer from a non-pool LAN address.
- Applying an empty configuration removed `routerd-dnsmasq`, UDP/67, and the
  LAN DNS listener. VM224 made two DHCP discoveries and received no lease. The
  retained firewall table had drop policies, which is expected fail-closed
  behavior and produced the reported orphan-artifact warning.
- Reapplying the candidate configuration restored the DHCP service, UDP and TCP
  DNS replies, upstream DNS, and the NAT44 HTTP source observation.

## Restoration

VM219, VM220, and VM224 were rolled back to snapshots taken before this test.
VM219 and VM220 were restarted; VM224 was returned to the stopped VLAN-trunk
configuration. The snapshots and PVE temporary HTTP staging were removed.
The restored VM219 hashes are `a1cbdd287e777d9eb090512ec7b7fb69f2340f0f12a71ed5567a8e2dcd2626f8`
for `routerd` and `7de346416d215f842deaee95f615dad4c5bbb3c8d78f376d2a43ded55554b8f9`
for `routerctl`.

## Remaining scope

Router-host reboot or interrupted-apply recovery with active dynamic leases,
WAN DHCP client behavior, all DNS query policies and transports, negative DNS
answers, IPv6, FreeBSD, and whole-product parity remain separate qualifications.
