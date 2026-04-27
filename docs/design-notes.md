# Design Notes

This document keeps early design notes that are not yet part of the stable
resource model.

## DS-Lite Without Prefix Delegation

Reference:

- Hiroki Sato, FreeBSD Workshop 2017 material:
  <https://people.allbsd.org/~hrs/sato-FBSDW20170825.pdf>

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
