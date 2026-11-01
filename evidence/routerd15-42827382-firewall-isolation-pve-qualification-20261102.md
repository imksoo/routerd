# routerd 1.5 Linux PVE firewall isolation qualification

## Scope

This record covers the configured default firewall policy in the isolated
Linux PVE router on VM219. The router binary SHA-256 was
`65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`.
It matches a `CGO_ENABLED=0` rebuild of source revision
`428273825467d5b23e9bf8d30baf579696cffcad`.

VM220 provided three independent namespaces connected through PVE VLANs 3131
(LAN), 3132 (WAN), and 3133 (management). Each namespace ran a distinct HTTP
server. The LAN client used MAC address `02:00:00:15:00:01` and obtained its
configured DHCPv4 reservation.

## Observed result

- The independent LAN client obtained `198.18.15.150/24` with gateway and DNS
  server `198.18.15.1`.
- LAN DNS queries for `router.home.arpa` succeeded over both UDP and TCP.
- LAN-to-WAN HTTP succeeded. The WAN server returned and logged
  `198.18.16.2`, proving NAT44.
- Management-to-LAN HTTP succeeded and the LAN server observed
  `198.18.17.2`.
- WAN-to-LAN HTTP and LAN-to-management HTTP both timed out. Neither target
  HTTP log contained the denied request path.
- WAN queries to the router DNS address timed out over both UDP and TCP.
- The actual router nftables counters confirmed the rejection paths:
  `lan_to_admin` dropped three packets, `wan_to_lan` dropped three packets,
  and `wan_to_self` dropped two packets.

The test removed all three VLAN namespaces, temporary files, and transient
peer services. VM219 remained active with `routerd` healthy and 21 resources.

## Unverified scope

This result covers the tested default IPv4 zone paths and router DNS exposure.
It does not cover all firewall rule actions, port forwarding, rate limits,
IPv6 filtering, configuration changes while sessions are active, high
availability, a physical target device, FreeBSD, or complete routerd 1.0
feature preservation.
