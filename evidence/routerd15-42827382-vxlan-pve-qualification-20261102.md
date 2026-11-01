# routerd 1.5 Linux PVE VXLAN qualification

## Scope

This record covers a unicast `VXLANSegment` lifecycle on the isolated Linux
PVE router VM219 and an independent Linux VTEP on VM220. The router binary
SHA-256 was
`65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`.
It matches a `CGO_ENABLED=0` rebuild of source revision
`428273825467d5b23e9bf8d30baf579696cffcad`.

The temporary 24-resource routerd 1.5 candidate added `VXLANSegment/overlay`,
an `IPv4StaticAddress` on it, and a trust firewall zone. Its VTEP used
`198.18.16.2` on router interface `rd15-wan`, VNI 4242, UDP port 4789, remote
`198.18.16.1`, interface name `rd15-vx`, and address `10.242.42.1/24`.

VM220 created its peer VTEP with the stock `ip` command in an independent
VLAN 3132 namespace. The peer VTEP used `198.18.16.1`, VNI 4242, and address
`10.242.42.2/24`; it was not implemented by routerd.

## Observed result

- `routerctl validate` accepted the 24-resource candidate.
- Applying it created `rd15-vx`, up with the declared VNI, local address,
  underlay device, UDP port, MTU, and routerd VXLAN ownership alias.
- The routerd-managed static address `10.242.42.1/24` was present on that
  link. VM219 reported `Healthy`, generation 19, and 24 resources.
- ICMP from VM219 to the independent VTEP and back completed 3/3 in each
  direction, with no packet loss.
- An HTTP request from VM219 to the independent VTEP succeeded. The router
  FDB included the configured permanent flood destination `198.18.16.1` and
  the learned peer MAC address. Both routerd and peer VXLAN links recorded
  transmit and receive packets.
- Reapplying the 21-resource baseline removed `rd15-vx`. VM219 then reported
  `Healthy`, generation 20, and 21 resources.

The independent peer namespace, transient HTTP service, and both temporary
configuration directories were removed after verifying the routerd link was
absent.

## Unverified scope

This result covers one Linux unicast VXLAN peer, direct L3 traffic, and
declaration withdrawal. It does not cover a bridged segment, multiple or
multicast remotes, MTU or fragmentation behavior, VTEP restart, conditional
activation, firewall policy variants, high availability, FreeBSD, or complete
routerd 1.0 feature preservation.
