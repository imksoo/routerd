# routerd 1.5 current Linux PVE IPv6 router-advertisement qualification

## Scope

This record qualifies `IPv6RouterAdvertisement` in the Linux binary built from
source revision `d9623bbd867d450ff8d1be03c59222592dbf43ad`.

The temporary test configuration was a standalone routerd 1.5 configuration.
It used no routerd 1.0 configuration, state database, handoff marker, or
cross-version migration path.

## Observed result

On the isolated PVE LAN VLAN, routerd applied
`IPv6RouterAdvertisement/lan-ula-ra` with prefix `fd42:18:0:1::/64`.
The independent Linux client namespace received a non-tentative SLAAC address
in that prefix and installed an IPv6 default route learned from the router's
link-local address.

The temporary resource was then removed by replacing the temporary
configuration with the original standalone 21-resource routerd 1.5
configuration. The independent client no longer had that RA-derived default
route. The router reported `STATUS HEALTHY`, generation 5, and 21 resources.

## Unverified scope

This check covers RA emission, SLAAC reception, and withdrawal only. It does
not establish delegated-prefix acquisition, IPv6 DNS reachability, IPv6
forwarding to an upstream, renumbering, high availability, or FreeBSD behavior.
