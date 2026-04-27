# Design Notes

This document keeps early design notes that are not yet part of the stable
resource model.

## DS-Lite Without Prefix Delegation

Reference:

- [Hiroki Sato, FreeBSD Workshop 2017 material](https://people.allbsd.org/~hrs/sato-FBSDW20170825.pdf)

During PR-400NE / NTT Flets testing, some hosts can receive DHCPv6 Prefix
Delegation immediately after the home gateway restarts, while another host may
continue sending DHCPv6 Solicit for IA_PD without receiving Advertise or Reply.
When PD is unavailable, a possible fallback is:

- keep the WAN-side IPv6 address obtained by RA/SLAAC or DHCPv6 IA_NA;
- establish DS-Lite using that upstream IPv6 reachability;
- provide IPv4 service to the LAN through DHCPv4 and DS-Lite;
- bridge or otherwise pass through IPv6 to the LAN instead of routing a
  delegated prefix.

This is only a design note. It needs separate validation before it becomes a
resource model because IPv6 bridging changes the ownership boundary:

- routerd would no longer own a routed LAN IPv6 prefix;
- RA, DHCPv6, firewall, and neighbor-discovery behavior may be controlled by
  the upstream home gateway;
- firewall policy must avoid accidentally exposing LAN hosts when IPv6 is
  bridged;
- DS-Lite tunnel source selection must support an address that is not derived
  from a delegated LAN prefix.

Potential future resource shape:

- a WAN state label for "PD unavailable but upstream IPv6 usable";
- a DS-Lite local-address source that can use WAN SLAAC / IA_NA addresses;
- an IPv6 bridge/pass-through resource with explicit firewall defaults;
- a retry policy that records previously delegated prefixes, DUID, IAID, and
  lease metadata to prefer renewal-like behavior when a home gateway is
  sensitive to fresh DHCPv6-PD requests.

Current groundwork:

- `IPv6PrefixDelegation` records observed prefix state under
  `ipv6PrefixDelegation.<name>.*` in the routerd state store.
- The last known prefix is retained when no current downstream prefix is
  visible.
- `IPv6PrefixDelegation.spec.convergenceTimeout` keeps a recently observed
  current prefix alive for a short grace period while the OS DHCPv6 client is
  reacquiring PD. This timeout is separate from systemd-networkd or KAME
  `dhcp6c` retransmission timers; routerd does not currently tune those
  client-specific packet timers.
- For systemd-networkd, routerd records IAID/DUID material when it can be
  observed from networkd runtime files. For NTT profiles it also records the
  expected link-layer DUID derived from the uplink MAC address.
- For FreeBSD, routerd observes the delegated prefix from addresses that KAME
  `dhcp6c` has already placed on the downstream interface, then adds the
  configured stable suffix address as a secondary address.
- FreeBSD `dhcp6c` is rendered with `-n`, and routerd uses SIGUSR1 for
  required restarts, because normal stops send DHCPv6 Release and can make a
  home gateway keep a stale lease while the client falls back to fresh
  Solicit.
- PR-400NE testing showed DHCPv6 Advertise/Reply packets with UDP destination
  port 546 and a non-547 source port. Firewall policy must match the client
  destination port and must not require source port 547.
- PR-400NE testing also showed that a home gateway restart can make multiple
  `/60` PD leases appear at once, while fresh Solicit attempts before restart
  may look unanswered. The working model is to keep DUID/IAID and last-prefix
  memory, avoid unnecessary Release traffic, and leave renewal-like retry as a
  separate future step.

This still does not synthesize DHCPv6 Renew/Rebind packets. That should remain
a separate implementation step because it must preserve management
connectivity and must not fight the OS DHCPv6 client.
