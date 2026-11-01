# routerd 1.5 Linux PVE reboot recovery qualification

## Scope

This record covers reboot recovery of the current fresh routerd 1.5
configuration on the isolated Linux PVE router VM219. The router binary
SHA-256 was
`65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`.
It matches a `CGO_ENABLED=0` rebuild of source revision
`428273825467d5b23e9bf8d30baf579696cffcad`.

PVE managed the reboot out of band. VM220 supplied separate VLAN 3131 and
3132 namespaces after the router had restarted. The LAN namespace used MAC
address `02:00:00:15:00:01`, matching the configured DHCPv4 reservation. The
WAN namespace ran an independent HTTP server at `198.18.16.1:8080` that
recorded the remote source address.

## Observed result

- Before reboot, VM219 reported `Healthy`, generation 18, and 21 resources.
- PVE rebooted VM219. Its QEMU Guest Agent became available again, then the
  actual `routerd` systemd service became active and reported `Healthy` with
  21 resources.
- The post-reboot status display showed generation 0 for the current startup
  application. This was not a loss of rollback history: `routerctl rollback
  --list` read saved generations 1 through 18, including the selected
  configuration and the generation-18 latest attempt.
- The independent VLAN 3131 client obtained reserved address
  `198.18.15.150/24`, a default route through `198.18.15.1`, and a
  120-second DHCP lease.
- Direct DNS queries from that client returned `198.18.15.1` for
  `router.home.arpa` over both UDP and TCP.
- An HTTP request from the LAN client to the independent VLAN 3132 server
  succeeded. Both the response and the upstream log recorded source address
  `198.18.16.2`, proving NAT44 recovery after the reboot.

The temporary VLAN namespaces, transient peer services, and test files were
removed. VM219 was left with active `routerd`, `Healthy`, and 21 resources.

## Unverified scope

This result covers one controlled VM reboot, DHCPv4 reservation, local DNS,
and NAT44 recovery. It does not prove interrupted write recovery, every
saved-generation rollback after reboot, DHCP lease expiry, IPv6 recovery,
high availability, a physical target device, FreeBSD, or complete routerd 1.0
feature preservation.
