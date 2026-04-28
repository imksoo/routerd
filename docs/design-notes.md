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
- For FreeBSD `dhcp6c`, routerd records IAID from configuration and DUID from
  `/var/db/dhcp6c_duid`. This matters because a home gateway may treat a
  remembered DUID/IAID pair as an existing lease that should be renewed rather
  than a new client that should receive a fresh lease.
- For FreeBSD, routerd observes the delegated prefix from addresses that KAME
  `dhcp6c` has already placed on the downstream interface, then adds the
  configured stable suffix address as a secondary address.
- FreeBSD `dhcp6c` is rendered with `-n`, and routerd uses SIGUSR1 for
  required restarts, because normal stops send DHCPv6 Release and can make a
  home gateway keep a stale lease while the client falls back to fresh
  Solicit.
- `IPv6PrefixDelegation.spec.hintFromState` defaults to `true`. When routerd
  has a last observed prefix whose valid lifetime has not elapsed, it feeds
  that prefix back to systemd-networkd or KAME `dhcp6c` as a prefix hint. If
  the lease memory is missing or too old, routerd falls back to a prefix-length
  hint. This keeps the request harmless when the upstream forgot the old lease.
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

### PR-400NE Lab Observation: Prefix Hints

Router01, a FreeBSD host using KAME `dhcp6c`, was tested behind a PR-400NE on
2026-04-28. The management interface was kept on a separate network and the
test changed only the WAN-side DHCPv6-PD client configuration. Before the test,
router01 had no locally recorded `lastPrefix`; its state store only retained
the stable DUID and IAID `0`.

Observed behavior:

- With no state-derived prefix hint, router01 sent DHCPv6 Solicit from UDP
  port 546 to `ff02::1:2` port 547. The IA_PD option used IAID `0` and a
  length-only prefix hint `::/60`. The PR-400NE did not send Advertise or Reply
  during the 40 second capture.
- After seeding routerd's state store with the previously seen
  `2409:10:3d60:1240::/60`, routerd rendered `prefix
  2409:10:3d60:1240::/60 infinity;` in `dhcp6c.conf`. tcpdump confirmed that
  Solicit carried IA_PD IAID `0` and the specific prefix hint
  `2409:10:3d60:1240::/60`. The PR-400NE still did not send Advertise or Reply
  during the 40 second capture.
- With the same seeded prefix but a temporary IAID `1`, tcpdump confirmed
  Solicit with IA_PD IAID `1` and the same specific prefix hint. The PR-400NE
  again did not send Advertise or Reply during the capture. The configuration
  was restored to IAID `0` after the test.
- No DHCPv6 Release packets were observed in these captures. The client was
  restarted using no-release behavior.

Interpretation:

- KAME `dhcp6c` does put the rendered `prefix ... infinity;` value into the
  IA_PD prefix hint. This validates routerd's FreeBSD renderer behavior.
- In this PR-400NE state, a prefix hint alone did not revive an unobservable
  delegated prefix. The likely behavior is that the home gateway either had no
  active binding for this client at that moment or was not responding to fresh
  Solicit, even when the Solicit included the remembered prefix.
- The next useful test is a real Renew/Rebind path while a lease is still
  active, not another fresh Solicit. routerd should therefore keep the prefix
  hint mechanism, but still needs an OS-client-safe renewal operation or an
  observation point that captures Renew/Rebind before the lease expires.

## DHCPv6 Prefix Delegation Behavior in Other Routers

This note compares open-source and commercial DHCPv6 Prefix Delegation
implementations to guide routerd's next PD design steps.

References:

- [RFC 9915: Dynamic Host Configuration Protocol for IPv6](https://datatracker.ietf.org/doc/html/rfc9915)
- [OpenWrt odhcp6c README](https://github.com/openwrt/odhcp6c)
- [OpenWrt odhcp6c source cross-reference](https://lxr.openwrt.org/source/odhcp6c/src/dhcpv6.c)
- [OpenWrt odhcpd README](https://github.com/openwrt/odhcpd)
- [OpenWrt odhcpd source cross-reference](https://lxr.openwrt.org/source/odhcpd/src/)
- [systemd.network manual](https://www.freedesktop.org/software/systemd/man/254/systemd.network.html)
- [FreeBSD dhcp6c(8)](https://man.freebsd.org/cgi/man.cgi?manpath=freebsd-release-ports&query=dhcp6c&sektion=8)
- [FreeBSD dhcp6c.conf(5)](https://man.freebsd.org/cgi/man.cgi?query=dhcp6c.conf)
- [pfSense advanced networking documentation](https://docs.netgate.com/pfsense/en/latest/config/advanced-networking.html)
- [OPNsense DHCP documentation](https://docs.opnsense.org/manual/isc.html)
- [MikroTik RouterOS DHCP documentation](https://help.mikrotik.com/docs/display/ROS/DHCP)
- [MikroTik RouterOS IP pools documentation](https://help.mikrotik.com/docs/display/ROS/IP%2BPools)
- [Cisco IOS XE DHCPv6 Prefix Delegation](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/ipaddr_dhcp/configuration/xe-16-9/dhcp-xe-16-9-book/ip6-dhcp-prefix-xe.html)
- [Cisco DHCPv6 PD configuration example](https://www.cisco.com/c/en/us/support/docs/ip/ip-version-6-ipv6/113141-DHCPv6-00.html)
- [Juniper Junos IA_NA and Prefix Delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-iana-prefix-delegation-addressing.html)
- [Juniper Junos subscriber LAN prefix delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-prefix-delegation-lan-addressing.html)
- [Juniper Junos DHCPv6 client reference](https://www.juniper.net/documentation/us/en/software/junos/cli-reference/topics/ref/statement/dhcpv6-client-edit-interfaces.html)

### Observed Patterns

| Implementation | Lifetime and lease state | Restart behavior | Identity handling | Renew / Rebind / Solicit / Release policy | Loss handling |
| --- | --- | --- | --- | --- | --- |
| OpenWrt `odhcp6c` | Exposes prefixes with preferred and valid lifetimes to scripts and ubus. Packet counters include Solicit, Request, Renew, Rebind, Reply, and Release. | Event-driven state scripts update the rest of the system when the DHCPv6 state changes. | Client ID is exposed as DHCPv6 option 1; OpenWrt keeps the client daemon as the owner of DHCPv6 packet state. | Implements the normal DHCPv6 state machine and reports `bound`, `updated`, `rebound`, `unbound`, and `stopped`. | `unbound` explicitly means all DHCPv6 servers were lost and the client will restart. |
| OpenWrt `odhcpd` | Server-side leases are persisted in a lease file; PD leases include interface, DUID, IAID, expiration, assignment, length, and active prefixes. | Dynamic reconfiguration happens when prefixes change. | Server bindings are keyed by DUID and IAID. | Server and relay behavior is separated from the client. | Can relay RA, DHCPv6, and NDP when no delegated prefix is available. |
| systemd-networkd | Owns the DHCPv6 client and downstream prefix assignment. The manual documents upstream and downstream PD sections, prefix hints, subnet IDs, and RA announcement. | Recreates runtime state from network files and DHCPv6 exchange; it does not expose a stable lease database suitable as routerd's source of truth. | `DUIDType`, `DUIDRawData`, and `IAID` can be configured. | `SendRelease` exists; systemd changed release behavior over time, so routerd should render it explicitly when relying on no-release behavior. | PD changes are handled inside networkd; external orchestration needs to observe resulting addresses/routes and logs. |
| FreeBSD / KAME `dhcp6c` | Stores client DUID in `/var/db/dhcp6c_duid`; configured IA_PD and `prefix-interface` drive downstream assignment. | `SIGHUP` reinitializes and `SIGTERM` stops; both normally send Release. `SIGUSR1` stops without Release. | IAID is configured in `dhcp6c.conf`; DUID is in the DUID file. | `-n` prevents Release on exit. This is important for home gateways that remember leases but do not answer fresh Solicit reliably. | Resource removal is local; another process must observe whether downstream prefixes remain present. |
| pfSense / OPNsense | Both expose DHCPv6-PD through a firewall UI. pfSense documents DUID editing and a no-release option. OPNsense documents active prefix leases and DUID matching for static mappings. | Configuration is rendered into underlying DHCP components; operators can preserve a DUID across reinstall. | pfSense accepts raw DUID, DUID-LLT, DUID-EN, DUID-LL, and DUID-UUID. OPNsense server-side static mapping uses DUID, with documentation noting active leases. | pfSense explicitly documents that `dhcp6c` sends Release by default and offers an option to prevent it. | The user-facing model surfaces DHCPv6 status and allows debugging logs. |
| MikroTik RouterOS | Client shows prefix, `expires-after`, and status. Server-side bindings contain DUID, IAID, lifetime, expiration, and last-seen. Dynamic pools receive an expiration time. | Received prefixes populate dynamic pools; addresses can be built from pools and old prefixes are advertised with zero lifetime when removed. | Client supports custom DUID, interface-based DUID, custom IA_PD ID, and custom IA_NA ID. Server bindings use DUID plus IAID. | Client states include searching, requesting, bound, renewing, rebinding, stopping. `renew` attempts renewal and then reinitializes if renewal fails; `release` explicitly releases and restarts. | Status and scripts expose whether PD is valid and the current prefix value. |
| Cisco IOS XE | DUID is stable and formed from the lowest-numbered interface MAC address. Different IAIDs with the same DUID are treated as different clients. | General prefixes can be learned by a DHCPv6 client and then referenced by interfaces. | DUID stability and IAID separation are documented. | Four-message exchange is default; rapid commit can be enabled. Examples expose IA_PD T1/T2 in show output. | Operational commands show client state, delegated prefix, DNS/domain options, and general prefix state. |
| Juniper Junos | Lease time is used to set address/prefix lifetimes and Renew/Rebind timers. Multiple IA_NA/IA_PD requests can have independent lease timers. | Subscriber delegated prefix preservation can preserve a prefix after logout and allocate the same delegated prefix after login. | DHCPv6 client identifier DUID type is configurable; subscriber systems track bindings and leases. | Junos documentation prefers a single Solicit containing both IA_NA and IA_PD in some subscriber cases. | Operational commands expose DUIDs, lease timers, and binding state. |
| VyOS | Current VyOS uses generated configuration around DHCPv6 client behavior; historical discussions and bugs show WIDE `dhcp6c`-style PD, `sla-id`, and downstream interface rendering. | PD depends on interface readiness; PPPoE and downstream interface timing have been recurring design concerns. | WIDE-style DUID files and IA_PD configuration appear in user-visible logs and generated behavior. | The practical lesson is to avoid blind daemon restarts around interface bring-up and to keep identity stable. | PD rendering bugs tend to surface as wrong downstream prefix length or missing downstream address. |
| dnsmasq / WIDE DHCPv6 | dnsmasq is useful for DHCPv6 server/RA and DUID control, but it is not the right source of truth for WAN PD client state in routerd. WIDE/KAME `dhcp6c` is relevant for client PD. | dnsmasq lease data is server-side; WIDE/KAME client state depends on DUID file, config, and live daemon state. | dnsmasq can configure server DUID; WIDE/KAME has a client DUID file. | Keep dnsmasq for LAN services and do not make it responsible for WAN PD acquisition. | LAN DHCP/RA should react to routerd-observed PD state. |

### Design Conclusions for routerd

Adopt:

- Store a structured PD lease record in routerd state: resource name,
  interface, client implementation, DUID, IAID, server DUID when observable,
  current prefix, previous prefix, preferred lifetime, valid lifetime, T1, T2,
  last observed time, last missing time, and acquisition state.
- Feed a valid previous prefix to the OS DHCPv6 client as a prefix hint. This
  is useful for home gateways that remember a DUID/IAID/prefix tuple, and it
  degrades to an ordinary Solicit when the upstream ignores the hint.
- Treat DUID and IAID as first-class desired state. Render them explicitly for
  systemd-networkd and KAME `dhcp6c` where possible, and warn when observed
  identity differs from the profile expectation.
- Keep no-release behavior explicit. For FreeBSD/KAME, continue using `-n` and
  SIGUSR1 for required restarts. For systemd-networkd, render `SendRelease=no`
  if the target systemd version supports it and document fallback behavior.
- Add a `renew` operation abstraction that asks the owning OS client to renew
  when the OS has a safe command for it. It should not synthesize DHCPv6
  packets in the first implementation.
- Expose PD lifecycle through the control API: current state, timers, last
  packet-level transition if known, and warnings. This mirrors RouterOS,
  Cisco, Junos, and OpenWrt operational visibility.
- Keep the existing convergence timeout, but make it secondary to real
  preferred/valid lifetimes once those are observable.
- When downstream prefixes disappear, deprecate or withdraw LAN-side RA/DHCPv6
  data intentionally rather than leaving stale service state. MikroTik's
  "advertise old prefix with zero lifetime" behavior is a good model for the
  future RA renderer.
- Keep firewall rules broad enough for valid DHCPv6 replies: allow UDP
  destination port 546 for the client and do not require source port 547.

Defer:

- Do not generate Renew/Rebind packets inside routerd yet. That requires a
  full DHCPv6 client state machine, server-id handling, retransmission timers,
  authentication/reconfigure behavior, and careful interaction with OS clients.
- Do not make dnsmasq responsible for WAN PD state. It remains a LAN DHCP/RA/DNS
  component.
- Do not assume every home gateway honors prefix hints or remembers prefixes in
  a standards-friendly way. Profiles should express quirks, but the default
  model must still follow DHCPv6 lifetimes and the OS client state machine.
- Do not implement commercial-router-style subscriber accounting until routerd
  has a stable lease record and control API for one router's own PD lifecycle.

### Backlog Items

- Add `routerctl show pd` with DUID, IAID, prefix, lifetimes, T1/T2, last
  observed time, last missing time, client status, and warnings.
- Extend the internal `PDLease` state model with server DUID, preferred
  lifetime, valid lifetime, T1, T2, and acquisition state when each OS client
  exposes them.
- Add OS-specific renew hooks:
  - systemd-networkd: investigate whether DBus, `networkctl`, or service reload
    can request renewal without releasing state.
  - FreeBSD/KAME `dhcp6c`: investigate control socket support and safe signal
    behavior beyond SIGUSR1/no-release restart.
- Add explicit `releasePolicy` or `sendRelease` configuration with conservative
  defaults for NTT/home-gateway profiles.
- Add profile fields for prefix hint, IA_NA/IA_PD combined request preference,
  DUID type, IAID, no-release behavior, convergence timeout, and firewall reply
  matching.
- Add downstream deprecation behavior for removed PD prefixes before removing
  addresses and RA/DHCPv6 service data.
