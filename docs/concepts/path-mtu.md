# Path MTU and TCP MSS

routerd derives path MTU behavior from the resources that create tunnel paths.
DS-Lite, PPPoE, and WireGuard interfaces provide the effective tunnel MTU, and
firewall zones identify the LAN-to-WAN forwarding direction.

When a trusted interface forwards through an untrusted tunnel, routerd renders
TCP MSS clamping automatically. For IPv4 TCP, MSS is `MTU - 40`. For IPv6 TCP,
MSS is `MTU - 60`.

Router advertisements also receive a derived MTU when the trusted interface has
a `DHCPv6Scope` or `IPv6RouterAdvertisement` and the forwarding path uses a
smaller tunnel MTU. The config should declare the LAN, WAN, tunnel, firewall
zones, and RA/DHCPv6 intent; it should not contain a separate MTU policy
resource.

Reverse path filter sysctls follow the same rule: routerd derives conservative
router defaults and tunnel-specific `rp_filter=0` settings from router and
tunnel resources.
