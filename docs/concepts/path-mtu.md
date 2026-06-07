# Path MTU and TCP MSS

![Diagram showing how routerd derives tunnel MTU, TCP MSS clamp, router advertisement MTU, and optional IPv4 force fragmentation](/img/diagrams/concept-path-mtu.png)

routerd derives path MTU behavior from the resources that create tunnel paths.
DS-Lite, PPPoE, WireGuard, and `TunnelInterface` underlays (`ipip`, `gre`,
`fou`, `gue`) provide the effective tunnel MTU, and firewall zones identify the
LAN-to-WAN forwarding direction.

When a trusted interface forwards through an untrusted tunnel, routerd renders
TCP MSS clamping automatically. For IPv4 TCP, MSS is `MTU - 40`. For IPv6 TCP,
MSS is `MTU - 60`. The effective value is computed per source and destination
path as the lower of the source interface MTU and destination path MTU. The
Linux nftables renderer only rewrites SYN packets whose advertised MSS is
higher than that derived value, so a lower MSS from another small-MTU interface
is never raised and does not force unrelated LAN paths down.

For trusted overlay paths that still black-hole oversized non-TCP IPv4 traffic
with the DF bit set, `OverlayPeer.spec.pathMTU.forceFragmentIPv4` and
`TunnelInterface.spec.pathMTU.forceFragmentIPv4` enable an explicit last-resort
fallback. On Linux, routerd renders an `ip routerd_forcefrag` nftables table
that matches the derived forwarded path, clears DF only when `ip length` is
larger than the derived path MTU, and then lets the kernel fragment on egress.
This is IPv4-only and default-off. Prefer correct MTU, PMTUD, and TCP MSS clamp
first; use force fragmentation only on a trusted overlay or underlay where
fragmentation is an accepted tradeoff.

Router advertisements also receive a derived MTU when the trusted interface has
a `DHCPv6Server` or `IPv6RouterAdvertisement` and the forwarding path uses a
smaller tunnel MTU. The config should declare the LAN, WAN, tunnel, firewall
zones, and RA/DHCPv6 intent; it should not contain a separate MTU policy
resource.

Reverse path filter sysctls follow the same rule: routerd derives conservative
router defaults and tunnel-specific `rp_filter=0` settings from router and
tunnel resources.
