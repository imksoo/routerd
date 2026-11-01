# routerd 1.5 current Linux PVE DHCPv4 qualification

## Scope

This record qualifies a Linux DHCPv4 server, reservation, local DNS, and NAT
path in the binary built from source revision
`d58cf470c09d347efd20c42190204af50310df28`. The isolated PVE configuration
used the standalone 21-resource routerd 1.5 configuration on VM219.

VM220 supplied an independent client namespace over VLAN 3131 and an
independent upstream HTTP namespace over VLAN 3132. The client used MAC address
`02:00:00:15:00:01`, which matched the configured `DHCPv4Reservation`.

## Observed result

- The independent client obtained `198.18.15.150/24` with a 120-second lease
  and default route through `198.18.15.1`.
- Direct DNS queries to the router returned `198.18.15.1` for
  `router.home.arpa` and `198.18.15.150` for the DHCP-derived
  `printer.home.arpa` record.
- HTTP traffic from the independent VLAN 3131 client to the VLAN 3132 upstream
  peer succeeded. The peer observed source address `198.18.16.2`, proving the
  configured NAT44 path.
- An explicit DHCP client rebind retained the reservation. routerd recorded
  the lease renewal and its DHCP-derived DNS record remained available.
- After a routerd service restart, the independent client rebound to the same
  reservation. DNS and upstream NAT traffic recovered, and VM219 reported
  `Healthy` with 21 resources.

## Unverified scope

The `dhcpcd` client could not apply its DNS configuration through the host
systemd-resolved instance in a separate network namespace. Direct DNS queries
were successful. Its release control request also re-acquired immediately, so
explicit release behavior is not qualified by this check.

Pool allocation for non-reserved clients, lease expiry, conflict handling,
server configuration withdrawal, high availability, target-device
qualification, and FreeBSD behavior remain separate work.
