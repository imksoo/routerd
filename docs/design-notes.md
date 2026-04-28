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

## State and Ownership Storage

routerd stores local state and ownership information in SQLite at
`/var/lib/routerd/routerd.db` on Linux and `/var/db/routerd/routerd.db` on
FreeBSD. The schema now follows the same broad storage idea as Kubernetes:
apply attempts create generations, resource-like records live as objects,
and events record notable changes. The goal is to make "what did routerd want,
what did it observe, and when did that happen" queryable without inventing a
new side channel for every resource type.

- `generations` records each apply attempt, its phase, warnings, and a hash
  of the config used for that attempt.
- `objects` stores resource-scoped status JSON. For example,
  `IPv6PrefixDelegation/wan-pd` keeps its lease, DUID, IAID, and timestamps
  under one object row instead of scattered state keys.
- `artifacts` stores the local ownership ledger for host objects managed by
  routerd, split into owner API version, kind, and name.
- `events` records apply warnings and PD observations. Higher-level
  describe commands can use this later.
- `access_logs` is present for the future local HTTP API audit trail. It is
  created now but not populated yet.

JSON columns are stored as text and can be inspected through SQLite JSON1:

```sh
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.lastPrefix') from objects where kind = 'IPv6PrefixDelegation' and name = 'wan-pd';"
```

`state.json` and `artifacts.json` are import-only legacy files. The earlier
two-table SQLite schema is also treated as an input format: routerd copies
`state` rows into `objects`, copies the old `artifacts` table into the new owner
columns, and drops the old tables. JSON files are still renamed to
`.migrated`. This keeps first startup automatic while allowing the storage model
to move forward during the pre-release period.

The runtime does not require the `sqlite3` command-line tool. It is useful for
human debugging, especially when looking at JSON fields through `json_extract`.
`jq` remains a dependency because trusted local plugins use JSON on standard
input and output, and shell-based plugins commonly need it to construct their
responses.

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

### PR-400NE Lab Observation: Renew Hooks

On 2026-04-28, routerd's first OS-level renew hooks were tested on router01
and router02 after installing the latest binaries on router01, router02, and
router03 and restarting their routerd services. The test preserved the
management paths and restored the state databases afterward.

Router01, FreeBSD with KAME `dhcp6c`, was seeded with a structured lease for
`2409:10:3d60:1230::/60`, `lastObservedAt` two hours in the past, and
`validLifetime` 14400 seconds. Apply recorded `lastRenewAttemptAt`, so
routerd did run the hook. tcpdump on `vtnet0` showed:

- an initial set of Solicit packets with the length-only `::/60` hint, caused
  by the first apply after the new binary changed the rendered `dhcp6c`
  configuration;
- a later set of Solicit packets with IA_PD IAID `0` and the seeded
  `2409:10:3d60:1230::/60` prefix hint;
- no Renew packet, no Rebind packet, and no Advertise or Reply from the
  PR-400NE during the capture.

This confirms that the hook and hint feed can push KAME `dhcp6c` into sending
a hint-bearing Solicit, but it did not recover the PD lease in this PR-400NE
state. The test was not a clean Renew path because the client had no visible
active PD lease and the first apply also had to repair generated
configuration.

Router02, systemd-networkd, was seeded with the same style of lease memory.
Apply recorded `lastRenewAttemptAt`, so routerd did call
`networkctl renew ens18`. tcpdump on `ens18` captured no DHCPv6 packets during
the test window. In this state, `networkctl renew` did not produce an
observable DHCPv6-PD Renew/Rebind/Solicit packet.

Design consequence:

- Keep `lastRenewAttemptAt`; it is useful to prove that routerd tried exactly
  once for a missing lease episode.
- Treat the current renew hooks as best-effort nudges, not as a reliable PD
  recovery mechanism.
- For KAME `dhcp6c`, a no-release restart plus a prefix hint may be the
  practical recovery action when no in-memory lease exists, but it is still a
  fresh Solicit path and depends on the home gateway responding.
- For systemd-networkd, routerd needs either a better control path or a
  scheduled check before the lease expires while networkd still has live
  DHCPv6 state.
- Add explicit logging for renew hook invocation and result so future packet
  captures can be correlated without relying only on state changes.

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

- Extend `routerctl show ipv6pd` with lifetimes, T1/T2, client status, and
  warnings. It already reports the core DUID, IAID, current prefix, last
  prefix, and observation timestamps from the local lease record.
- Extend the internal `PDLease` state model with server DUID, preferred
  lifetime, valid lifetime, T1, T2, and acquisition state when each OS client
  exposes them.
- Continue hardening OS-specific renew hooks:
  - systemd-networkd: routerd currently uses `networkctl renew <link>` when the
    local lease memory says the prior lease should still be valid.
  - FreeBSD/KAME `dhcp6c`: routerd currently sends SIGHUP to the running client
    under the same condition. Investigate control socket support before adding
    stronger policy.
- Add explicit `releasePolicy` or `sendRelease` configuration with conservative
  defaults for NTT/home-gateway profiles.
- Add profile fields for prefix hint, IA_NA/IA_PD combined request preference,
  DUID type, IAID, no-release behavior, convergence timeout, and firewall reply
  matching.
- Add downstream deprecation behavior for removed PD prefixes before removing
  addresses and RA/DHCPv6 service data.

## NTT FLET'S DHCPv6-PD Notes for PR-400NE

This note records what public specifications and router configuration examples
say about NTT FLET'S-style DHCPv6 Prefix Delegation, and separates those facts
from hypotheses about the PR-400NE home gateway used in the lab.

References:

- [RFC 8415: Dynamic Host Configuration Protocol for IPv6](https://www.rfc-editor.org/rfc/rfc8415.html)
- [NTT East technical references](https://www.ntt-east.co.jp/gisanshi/)
- [NTT East FLET'S series technical reference, volume 3](https://flets.com/pdf/ip-int-3.pdf)
- [NTT West technical reference: IP communication network service interface](https://www.ntt-west.co.jp/info/katsuyo/pdf/23/tenpu16-1.pdf)
- [NTT West: NGN IPv6 ISP tunnel interface](https://www.ntt-west.co.jp/open/ngn/pdf/ipv6_tunnel_uni.pdf)
- [Yamaha RT Series DHCPv6 features](https://www.rtpro.yamaha.co.jp/RT/docs/dhcpv6/index.html)
- [Yamaha IPv6 IPoE features](https://www.rtpro.yamaha.co.jp/RT/docs/ipoe/index.html)
- [NEC UNIVERGE IX FLET'S Hikari Next IPv6 IPoE configuration guide](https://jpn.nec.com/univerge/ix/Support/ipv6/native/ipv6-internet_dh.html)
- [NEC IX-R/IX-V DHCPv6 feature description](https://support.necplatforms.co.jp/ix-nrv/manual/fd/02_router/14-1_dhcpv6.html)
- [Internet Multifeed transix DS-Lite service](https://www.mfeed.ad.jp/transix/dslite/)
- [Internet Multifeed transix DS-Lite device list](https://www.mfeed.ad.jp/transix/dslite-models/)
- [Internet Multifeed transix glossary](https://www.mfeed.ad.jp/transix/faq/glossary/)
- [Sorah's Diary: FLET'S Hikari Next with Hikari Denwa DHCPv6-PD observation](https://diary.sorah.jp/2017/02/19/flets-ngn-hikaridenwa-kill-dhcpv6pd)
- [rixwwd: PR-400NE / Dream Router DHCPv6 packet observation](https://rixwwd.hatenablog.jp/entry/2023/04/09/211529)
- [SEIL: NGN IPv6 native IPoE example](https://www.seil.jp/blog/10.html)

### What the Public Documents Establish

- RFC 8415 defines the normal four-message exchange as
  Solicit, Advertise, Request, and Reply. Rapid Commit can shorten this to
  Solicit and Reply, but a client must not depend on Rapid Commit being used.
- RFC 8415 defines UDP port 546 as the client listening port and UDP port 547
  as the server and relay listening port. A firewall rule that accepts only
  packets with source port 547 is therefore an implementation shortcut, not a
  safe interpretation of the client receive path.
- RFC 8415 allows an IA_PD option to contain IA Prefix values as a prefix or
  prefix-length hint. It also allows a length-only hint by using prefix
  `::/length`.
- RFC 8415 says Renew extends leases with the original server, Rebind asks any
  available server after Renew fails, and Solicit is used again after all valid
  lifetimes have expired.
- Confirm is address-oriented. It can validate assigned addresses for a link,
  but it is not the recovery mechanism routerd should depend on for delegated
  prefixes.
- The NTT East and NTT West public interface documents describe the FLET'S
  network side and reference DHCPv6 and DHCPv6-PD RFCs for IPv6 ISP
  connectivity. They do not specify every LAN-side quirk of a PR-400NE home
  gateway.
- The current NTT East and NTT West FLET'S series interface documents say that
  terminal-side DUID generation must follow DUID-LL or DUID-LLT and must be
  based on the MAC address. DUID-EN, UUID-derived DUIDs, and machine-id-derived
  DUIDs are therefore outside the documented FLET'S endpoint model.
- The same documents describe a network-side delegated prefix of 48 or 56 bits
  for services that use DHCPv6-PD. The /60 prefixes observed behind PR-400NE
  are best interpreted as home-gateway downstream subdivisions, not as the
  raw NGN-facing prefix size.
- Sorah's 2017 field report is stronger than the current public interface
  text: it reports that NGN DHCPv6-PD silently ignores Solicit messages whose
  client identifier is not DUID-LL, including DUID-LLT and DUID-EN. Because the
  current official text permits DUID-LLT, routerd should treat "DUID-LL only"
  as a strict NTT profile quirk rather than as the generic DHCPv6 rule.
- NEC's official IX guide states that FLET'S Hikari Next changes behavior based
  on the Hikari Denwa contract: without Hikari Denwa the router uses RA, and
  with Hikari Denwa it uses DHCPv6-PD. The same guide obtains DNS through the
  DHCPv6 client, advertises the delegated prefix downstream with RA, and sets
  the RA other-config flag for DHCPv6 DNS delivery.
- NEC's guide filters DHCPv6 with source 547 to destination 546 and source 546
  to destination 547. That is a reasonable vendor example, but it conflicts
  with PR-400NE packet observations where Advertise may be sent from an
  ephemeral source port. routerd should therefore accept UDP destination 546
  regardless of the source port in the NTT home-gateway profile.
- Yamaha's DHCPv6 documentation confirms that RT-series routers support
  DHCPv6-PD, downstream use of a DHCPv6-PD-acquired prefix, and NGN-specific
  configuration through `ngn type` when using FLET'S Hikari Next or FLET'S
  Hikari Cross.
- transix / Internet Multifeed documentation establishes the DS-Lite service
  model and supported devices. It does not define the PR-400NE downstream
  DHCPv6-PD exchange. For routerd, transix DNS and AFTR handling should remain
  separate from PD acquisition.

### Close Reading of NTT Official Specifications

This pass reread the locally fetched NTT East `ip-int-3.pdf` and NTT West
`tenpu16-1.pdf` text with the client-side DHCPv6-PD questions in mind. Page
numbers below are the printed page numbers in the PDF text, not viewer page
indexes.

| Question | Relevant sections | Reading for routerd |
| --- | --- | --- |
| IA_NA and IA_PD in the same request | NTT East, FLET'S Hikari 25G, `2.4.1.1.2` on pp. 5-6 says the endpoint receives a prefix with DHCPv6-PD and cannot obtain a 128-bit address through DHCPv6. The DHCPv6 option table in `2.4.1.1.5` on pp. 7-8 lists IA_NA, IA_TA, IA Address, and Rapid Commit with note 2, whose footnote says they are not used by this interface specification. NTT West, FLET'S Hikari Cross, `2.4.2.1.2` on pp. 21-22 has the same "prefix only, no 128-bit DHCPv6 address" shape. | The official text does not give a reason to request IA_NA together with IA_PD for the NTT profile. It points the other way: PD is the useful request, and IA_NA is not part of the documented endpoint model for these sections. |
| DHCPv6 retransmission timers | The searched NTT East and West PDFs contain no DHCPv6 constants such as SOL_TIMEOUT or REQ_TIMEOUT. The visible timer sections are for other protocols or service operations, not DHCPv6 client retransmission. | Use RFC DHCPv6 retransmission behavior in any in-process client. Keep routerd's profile-level acquisition window configurable because PR-400NE/HGW timing is an operational issue, but do not invent NTT-specific packet timers from these PDFs. |
| UDP source port | The PDFs reference RFC3315/RFC3633 for DHCPv6 and DHCPv6-PD but do not add a client source-port rule. No text was found requiring client packets to originate only from UDP 546 beyond the normal DHCPv6 client/server port model. | routerd should still send from the DHCPv6 client port when it owns the socket, but firewall receive rules must not require reply source port 547. This remains based on RFC behavior and PR-400NE packet observations, not on a special NTT PDF rule. |
| RA M/O flags and Solicit timing | NTT East, Hikari Cross `4.4.2.1.2` on p. 22 and NTT West, Hikari Cross `2.4.2.1.2` on pp. 21-22 say RA may have the O and M flags set, while Information-Request is not supported in those sections. NTT East and West, FLET'S Hikari Next `2.4.2.1.2` on pp. 60-61 and pp. 59-60 say O=1 recommends Information-Request and M=1 recommends DHCPv6-PD. They also say voice-service cases use DHCPv6-PD and receive a 48-bit or 56-bit prefix. | The M flag is a good standards-facing trigger for PD. The documents are not uniform about Information-Request across service families, so routerd should not make Information-Request mandatory for the NTT HGW profile. A future client should support both RA-gated start and forced Solicit, with forced Solicit remaining useful for HGW LAN-side testing. |
| Rapid Commit | NTT East option tables such as `2.4.1.1.5` on pp. 7-8 list Rapid Commit but mark it with the same "not used" note as IA_NA. No section found says that the network will use or require two-message Rapid Commit acquisition. | Do not request or require Rapid Commit in `ntt-flets-with-hikari-denwa`. If a server sends Rapid Commit anyway, record it as an observed event. |
| Server Identifier in Solicit | DUID sections such as NTT East `2.4.1.1.4` on p. 6 and NTT West `2.4.2.1.4` on p. 22 say the network DUID is stable and MAC based. The option table lists Server Identifier as the network DUID, but no text found requires an initial Solicit to include a Server Identifier. | Follow DHCPv6 semantics: include Client Identifier in Solicit, use Server Identifier after the server is known, and persist the server identifier for Renew. Do not fake a server identifier in a fresh Solicit. |
| Confirm support | No Confirm message support was found in the searched NTT East or West PDF text. The explicit acquisition sequence diagrams show Solicit, Advertise, Request, and Reply for DHCPv6-PD. | Confirm is not part of the NTT PD recovery plan. Use Renew/Rebind while a lease is alive, and Solicit with prefix hints after that state is lost. |

Implementation option for IA_NA+IA_PD:

- Keep the NTT profile default as IA_PD-only. The official specifications
  repeatedly say a 128-bit address cannot be obtained through DHCPv6, and the
  option tables mark IA_NA as unused.
- If testing later proves that a particular HGW or upstream path reacts better
  to combined requests, add an explicit profile field such as
  `requestNonTemporaryAddress: true`; do not infer it from the NTT profile.
- For systemd-networkd, that mode would render DHCPv6 address use in addition
  to prefix delegation, instead of the current PD-only shape.
- For KAME `dhcp6c`, that mode would add `send ia-na <iaid>;` and an
  `id-assoc na <iaid> { };` block next to the existing `send ia-pd` and
  `id-assoc pd` block.
- Renderer tests should assert both modes: NTT default renders only IA_PD,
  while the explicit experiment switch renders IA_NA and IA_PD together.

### Packet Comparison After the 2026-04-29 HGW Restart

After another PR-400NE restart, the HGW screen showed all three lab routers
with delegated `/60` prefixes:

- router01: `2409:10:3d60:1220::/60`
- router02: `2409:10:3d60:1230::/60`
- router03: `2409:10:3d60:1240::/60`

router03 captured DHCPv6 traffic on its WAN interface during the restart
window. The capture did not include a separate IX2215 or Aterm client DUID, so
it is not yet a byte-for-byte comparison against a working commercial router.
It did capture a complete router03 exchange with the PR-400NE:

| Item | router02 local Solicit sample | router03 successful restart exchange |
| --- | --- | --- |
| Client DUID | DUID-LL, MAC `bc:24:11:30:5d:76` | DUID-LL, MAC `bc:24:11:40:32:de` |
| UDP ports | source 546, destination 547 | source 546, destination 547 |
| Request contents | IA_PD only. No IA_NA, no Rapid Commit, no Vendor Class, no Reconfigure Accept. | IA_PD only. No IA_NA, no Rapid Commit, no Vendor Class, no Reconfigure Accept in Solicit. |
| Option order in Solicit | IA_PD, Client FQDN, Option Request, Client Identifier, Elapsed Time | IA_PD, Client FQDN, Option Request, Client Identifier, Elapsed Time |
| Option Request | SIP domain, SIP address, DNS, SNTP, NTP, option 82, option 103, option 144 | DNS, SNTP, NTP, option 82, option 103 |
| Prefix hint | `2409:10:3d60:1230::/60` | initially `2409:10:3d60:1220::/60`, then the HGW delegated `2409:10:3d60:1240::/60` |
| Server response | no response in the comparison sample | Advertise and Reply came from link-local `fe80::1eb1:7fff:fe73:76d8`, UDP source port 49153, destination port 546 |
| Server options | none observed | Server Identifier DUID-LL MAC `1c:b1:7f:73:76:d8`, DNS, SNTP, IA_PD T1 7200, T2 12600, preferred lifetime 14400, valid lifetime 14400. Reply also included Reconfigure Accept. |

Important observations:

- The successful router03 Solicit is structurally the same as router02's
  routerd-rendered networkd Solicit. Both use DUID-LL, IA_PD only, UDP source
  port 546, and the same main option order.
- The PR-400NE again replied from UDP source port 49153, so the rule remains:
  inbound DHCPv6 client traffic must match destination port 546, not source
  port 547.
- The first router03 Solicit carried the old state-derived prefix hint
  `2409:10:3d60:1220::/60`, but the HGW delegated `2409:10:3d60:1240::/60`.
  This confirms that an exact hint is advisory. routerd must accept a different
  `/60`, update state immediately, and not treat the mismatch as an error.
- router02's restored prefix stayed `2409:10:3d60:1230::/60`, matching its
  previous `lastPrefix`. That is useful evidence that `hintFromState` can help
  return to the same prefix when the HGW still has, or recreates, a matching
  binding. It is not proof that the hint was the only cause.
- The capture also showed a router03 Release shortly after a Reply, followed
  by another Solicit and successful Request/Reply. That is a warning for the
  systemd-networkd path: even when acquisition works, uncontrolled client
  restarts can send Release and churn the HGW binding.

### State Validation After PD Recovery

The same recovery window was checked from routerd state and OS state.

| Host | routerd PD state | LAN-side IPv6 | dnsmasq | DS-Lite | IPv6 reachability |
| --- | --- | --- | --- | --- | --- |
| router01 / FreeBSD | `routerctl describe ipv6pd/wan-pd` reported current and last prefix `2409:10:3d60:1220::/60`. | `vtnet1` still had only link-local IPv6; the expected `2409:10:3d60:1220::1/64` was missing. | `routerd_dnsmasq` was not present and `dnsmasq` was not running. | no DS-Lite tunnel observed. | `ping6 ipv6.google.com` failed with no route. |
| router02 / NixOS | `routerctl describe ipv6pd/wan-pd` reported current and last prefix `2409:10:3d60:1230::/60`. | `ens19` had `2409:10:3d60:1230::2/64` plus DS-Lite source addresses `::100`, `::101`, and `::102`. | active; config listened on `192.168.160.2` and `2409:10:3d60:1230::2`, advertised RA on `ens19`, and handed out DNS `2409:10:3d60:1230::2`. | `ds-lite-a`, `ds-lite-b`, and `ds-lite-c` were up with MTU 1454 and sources `::100`, `::101`, `::102`. | `ping6 ipv6.google.com` succeeded. |
| router03 / Ubuntu | `routerctl describe ipv6pd/wan-pd` reported current and last prefix `2409:10:3d60:1240::/60`. | `ens19` had `2409:10:3d60:1240::3/128`; the kernel also had the delegated `/64` route. | active; config listened on `192.168.160.3` and `2409:10:3d60:1240::3`, advertised RA on `ens19`, and handed out DNS `2409:10:3d60:1240::3`. | `ds-lite-a`, `ds-lite-b`, and `ds-lite-c` were up with MTU 1454 and source `2409:10:3d60:1240::3`. | `ping6 ipv6.google.com` succeeded. |

The current development host did not have a global IPv6 address or default IPv6
route on the tested LAN-facing links, so a downstream SLAAC client check could
not be completed from that host in this pass.

Follow-up for renewal observation:

- HGW replies reported T1 7200 seconds and T2 12600 seconds, with 14400 second
  preferred and valid lifetimes.
- Schedule an observation window before T1 and through T2 after a successful
  lease. A simple operator-side cron or systemd timer can run tcpdump around
  T1/T2 and save `/tmp/routerd-pd-renew-<host>-<timestamp>.pcap`.
- The important packet-level questions are whether the OS client sends Renew
  to the remembered server, whether Rebind appears after T2, whether routerd
  state updates `lastObservedAt` without a new Solicit, and whether the HGW
  keeps the same `/60`.
- FreeBSD LAN propagation was fixed after this observation: router01 can now
  derive the LAN address from stored PD state, run the managed dnsmasq rc.d
  service, learn an IPv6 default route, and pass IPv6 traffic.

### Lab DUID Validation on 2026-04-28

The lab hosts were checked before changing DUID management code:

| Host | Client | Rendered configuration | Observed DUID | Packet result |
| --- | --- | --- | --- | --- |
| router02 / NixOS | systemd-networkd | `/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf` contains `DUIDType=link-layer` and a prefix hint for `2409:10:3d60:1230::/60`. | `networkctl status ens18` reports `DUID-LL:0001bc2411305d76`. tcpdump shows `client-ID hwaddr type 1 bc2411305d76`. | routerd's networkd renderer is active and the on-wire DUID is DUID-LL. No Advertise or Reply was seen during the 60 second restart capture. |
| router03 / Ubuntu | systemd-networkd | `/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcp6-pd.conf` contains `DUIDType=link-layer` and a prefix hint for `2409:10:3d60:1220::/60`. | `networkctl status ens18` reports `DUID-LL:0001bc24114032de`. tcpdump shows `client-ID hwaddr type 1 bc24114032de`. | routerd's networkd renderer is active and the on-wire DUID is DUID-LL. No Advertise or Reply was seen during the 60 second restart capture. |
| router01 / FreeBSD | KAME `dhcp6c` | `/usr/local/etc/dhcp6c.conf` requests IA_PD with a `::/60` hint. | `/var/db/dhcp6c_duid` starts with length `0e 00` followed by DUID type `00 01`, so it is DUID-LLT. tcpdump shows `client-ID hwaddr/time type 1 time 830607215 bc2411e3c238`. | The FreeBSD path does not yet manage the DUID file. router01 is still sending DUID-LLT and may be filtered by stricter NTT/HGW behavior. |

Conclusion:

- The systemd-networkd code path is not the current DUID problem for router02
  or router03. It already renders `DUIDType=link-layer`, networkd uses it, and
  tcpdump confirms DUID-LL on the wire.
- DUID alone does not explain the current no-reply captures for router02 and
  router03. They had previously received PD with the same DUID-LL values, so
  home-gateway lease state, timing after HGW restart, and Solicit-vs-Renew
  behavior remain active hypotheses.
- The FreeBSD/KAME path is a real gap. router01 still has a generated
  DUID-LLT file and sends DUID-LLT in Solicit. The next implementation should
  manage or adopt `/var/db/dhcp6c_duid` for NTT profiles before testing more
  HGW behavior on FreeBSD.

Follow-up after implementation:

- routerd now backs up the FreeBSD KAME DUID file when it is not DUID-LL and
  writes `0a 00 00 03 00 01 <uplink-mac>` before starting `dhcp6c` for NTT
  link-layer DUID profiles.
- On router01, `/var/db/dhcp6c_duid` changed from DUID-LLT to
  `0a 00 00 03 00 01 bc 24 11 e3 c2 38`, and the previous file was preserved
  as `/var/db/dhcp6c_duid.bak.20260428T094248Z`.
- A post-change tcpdump confirmed `client-ID hwaddr type 1 bc2411e3c238`,
  which is DUID-LL on the wire. No Advertise or Reply was seen in that 60
  second capture, so HGW lease state and Solicit-vs-Renew behavior remain
  open hypotheses after the FreeBSD DUID fix.

### Manual Renew Lab Test on 2026-04-29

The three lab routers were tested with a 60 second tcpdump on the WAN-side
interface while manually asking the OS DHCPv6 client to refresh its lease.

| Host | Client and command | Packet observation | State after the test | Interpretation |
| --- | --- | --- | --- | --- |
| router01 / FreeBSD | KAME `dhcp6c`; `kill -HUP $(cat /var/run/dhcp6c.pid)` | `dhcp6c` did not send Renew. It restarted acquisition with Solicit to `ff02::1:2`, UDP source port 546, DUID-LL `bc:24:11:e3:c2:38`, IA_PD IAID 0, and an exact hint for `2409:10:3d60:1220::/60`. No Advertise or Reply was seen within 60 seconds. | routerd still observed current and last prefix `2409:10:3d60:1220::/60`; `lastObservedAt` advanced because the prefix remained present on the host. | HUP is not a true renew path for KAME `dhcp6c` in this setup. It is a controlled restart that falls back to hint-bearing Solicit. |
| router02 / NixOS | systemd-networkd; `networkctl renew ens18` | No DHCPv6 packets were captured on `ens18`. The command returned success and wrote no new systemd-networkd journal entries. | routerd still observed current and last prefix `2409:10:3d60:1230::/60`; `lastObservedAt` advanced from host observation, not from a visible Reply. | `networkctl renew` did not trigger an on-wire DHCPv6-PD renewal in this systemd-networkd version/configuration. |
| router03 / Ubuntu | systemd-networkd; `networkctl renew ens18` | No DHCPv6 packets were captured on `ens18`. The command returned success and wrote no new systemd-networkd journal entries. | routerd still observed current and last prefix `2409:10:3d60:1240::/60`; `lastObservedAt` advanced from host observation, not from a visible Reply. | Same as router02: this is not a reliable manual PD renew mechanism for networkd-backed routerd. |

This test did not prove the normal T1/T2 renewal path. It did show that the
current OS-backed manual hooks are weak:

- FreeBSD `dhcp6c` can be nudged, but the nudge behaves as Solicit with a
  prefix hint, not Renew.
- systemd-networkd accepted `networkctl renew` but did not put a DHCPv6-PD
  packet on the wire during the capture window.
- routerd's state can still look fresh because apply observes the delegated
  prefix already installed on the host. That is useful for local state, but it
  must not be confused with proof that the upstream lease was renewed.

Design consequences:

- Keep using OS-backed hooks only as best-effort recovery nudges.
- Store T1, T2, preferred lifetime, valid lifetime, server identifier, and the
  last DHCPv6 transition when available. Without those fields, routerd cannot
  distinguish a real upstream renewal from a local host observation.
- The future in-process DHCPv6 client should implement and log real Renew and
  Rebind exchanges itself. That remains the clean path for PR-400NE/HGW
  debugging.
- Add a backlog item to test passive T1/T2 renewal by leaving tcpdump running
  across the expected T1 window instead of relying on `networkctl renew`.

### Passive T1/T2 Renewal Window Capture on 2026-04-29

The lab routers started a four-hour passive DHCPv6 capture on the WAN-side
interface at about `2026-04-28T15:50Z`:

| Host | WAN interface | Prefix at start | Capture files |
| --- | --- | --- | --- |
| router01 / FreeBSD | `vtnet0` | `2409:10:3d60:1220::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |
| router02 / NixOS | `ens18` | `2409:10:3d60:1230::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |
| router03 / Ubuntu | `ens18` | `2409:10:3d60:1240::/60` | `/tmp/pd-renew-window.pcap`, `/tmp/pd-renew-window-routerctl.log` |

The current routerd state does not yet store explicit T1, T2, preferred
lifetime, or valid lifetime values. For this capture window, the estimate uses
the observed PR-400NE/IX2215 values from the lab: preferred lifetime 14400
seconds, valid lifetime 14400 seconds, T1 7200 seconds, and T2 12600 seconds.
The prefixes were reacquired shortly after the HGW restart, around
`2026-04-28T15:02Z` to `15:07Z`, so the expected windows are:

| Event | Estimated UTC window | Notes |
| --- | --- | --- |
| T1 | `2026-04-28T17:02Z` to `17:07Z` | A normal client should send Renew to the remembered server. |
| T2 | `2026-04-28T18:32Z` to `18:37Z` | If Renew failed, a normal client should send Rebind. |
| Valid lifetime expiry | `2026-04-28T19:02Z` to `19:07Z` | If neither renewal path succeeds, the prefix should become invalid. |

Initial capture check:

| Host | Initial DHCPv6 message counts | Immediate note |
| --- | --- | --- |
| router01 | Solicit 8, Request 0, Renew 0, Rebind 0, Reply 0 | KAME `dhcp6c` was still sending exact-hint Solicit after the earlier HUP test. This must be treated separately from a clean T1 Renew. |
| router02 | Solicit 0, Request 0, Renew 0, Rebind 0, Reply 0 | No DHCPv6 traffic yet. |
| router03 | Solicit 0, Request 0, Renew 0, Rebind 0, Reply 0 | No DHCPv6 traffic yet. |

The capture uses packet-immediate pcap writing so intermediate counts can be
read while tcpdump is still running. The routerd state monitor logs a
`routerctl describe ipv6pd/wan-pd` snapshot every ten minutes.

### PR-400NE Behavior Hypotheses

These are working hypotheses for the lab profile
`ntt-flets-with-hikari-denwa`. They must stay configurable because they are
derived from public examples plus observed PR-400NE behavior, not from an
explicit PR-400NE LAN-side protocol specification.

| Scenario | Expected client sequence | Why this is plausible | routerd reproduction requirement |
| --- | --- | --- | --- |
| Fresh acquisition after HGW restart | Solicit with IA_PD for /60, optional prefix hint, then Advertise, Request, Reply. | RFC 8415 four-message exchange is the baseline. The lab HGW allocates /60 prefixes to downstream routers after restart. NEC and SEIL examples use DHCPv6-PD for Hikari Denwa environments. | Send standards-compliant Solicit, accept Advertise/Reply to UDP destination 546 from any source port, and keep acquisition timeouts long enough for a slow HGW restart window. |
| Known lease recovery | If DUID, IAID, and `lastPrefix` are still valid, send IA_PD with the last prefix as a hint. If the OS client still has server state, prefer Renew before falling back to Solicit. | RFC 8415 explicitly permits prefix hints. PR-400NE appears to remember DUID/IAID/prefix bindings across short restarts. | Persist `PDLease`, render the hint while valid lifetime remains, and expose whether recovery used Renew, Rebind, or Solicit. |
| Normal renewal | At T1, Renew to the server that issued the lease. If no Reply is received by T2, Rebind to any server. | RFC 8415 defines T1/T2-driven Renew/Rebind for IA_PD. The HGW lease table shows finite lifetimes, so losing renewal would eventually remove PD. | Store T1/T2 and lifetimes. Schedule renew attempts before expiry instead of waiting for the prefix to disappear. |
| Expired or forgotten lease | After valid lifetime expiry, use Solicit again. Include a length-only `::/60` hint or the previous prefix only if the profile allows stale hints. | RFC 8415 terminates the exchange when all valid lifetimes expire. A stale exact hint may be harmless, but it should be a profile choice. | Default to no exact hint after valid lifetime expiry; retain a length-only /60 request for the NTT profile. |
| Release avoidance | Do not send Release on daemon restart or controlled shutdown unless explicitly requested. | Some home gateways behave better when existing bindings age out or renew than when clients repeatedly release and create new bindings. pfSense and KAME `dhcp6c` expose no-release behavior for similar operational reasons. | Provide `sendRelease: false` or equivalent in the profile and make OS-specific renderers honor it. |
| Strict DUID selection | Use DUID-LL by default for NTT profiles. Treat DUID-EN and UUID-derived DUIDs as invalid for FLET'S-style PD. Allow DUID-LLT only when the profile explicitly relaxes the strict behavior. | Current NTT interface documents allow MAC-based DUID-LL or DUID-LLT, while Sorah's direct-NGN observation reports DUID-LL-only filtering. systemd-networkd's default DUID-EN is definitely outside the documented NTT model. | Default `spec.duidType` to `link-layer` for NTT profiles, render that choice explicitly, and report a warning when observed DUID does not start with DUID-LL type 3. |
| IA_NA plus IA_PD | Start with IA_PD-only. Make combined IA_NA+IA_PD a profile option. | Some commercial subscriber examples request both, but the lab only needs delegated prefixes and the HGW may be sensitive to small client differences. | `routerd_dhcp6c_client` must support both modes, with IA_PD-only as the NTT home-gateway default until tests prove otherwise. |
| Rapid Commit | Support it if the server offers it, but do not request or require it by default. | RFC 8415 allows Rapid Commit, but NTT and router vendor examples do not require it for the observed use case. | Leave Rapid Commit off by default for `ntt-flets-with-hikari-denwa`; record if a server sends it. |
| UDP receive filtering | Accept DHCPv6 replies with destination UDP port 546 from any source port. | RFC 8415 fixes listener ports, while PR-400NE community packet captures report Advertise from source port 49153. | Firewall and packet parser must not require source port 547 on inbound DHCPv6 client replies. |

### Requirements for `routerd_dhcp6c_client`

The future in-process client based on `insomniacslk/dhcp` should reproduce
the useful parts of OS clients while making NTT profile behavior observable and
testable:

- Keep DUID and IAID stable. For NTT profiles, DUID-LL should be the default
  even though the current NTT interface documents also permit DUID-LLT. This
  avoids systemd-networkd's default DUID-EN and matches stricter field reports
  that say NGN silently drops non-DUID-LL clients.
- Persist `PDLease` with DUID, IAID, server identifier, server link-local
  address when known, current prefix, last prefix, preferred lifetime, valid
  lifetime, T1, T2, last observed time, last missing time, and the last DHCPv6
  message transition.
- Build Solicit with Client Identifier, Elapsed Time, Option Request Option for
  DNS/SNTP as configured, and IA_PD. When state is valid, include the exact
  previous prefix hint; otherwise include a length-only `::/60` hint for the
  NTT profile.
- Do not include IA_NA by default in the NTT profile. Add a profile switch for
  IA_NA+IA_PD so it can be tested without changing code.
- After Advertise, send Request to the selected server with the same Client
  Identifier and IA_PD. Preserve Server Identifier for later Renew.
- At T1, send Renew with Server Identifier and IA_PD containing the current
  delegated prefix. At T2, send Rebind to the DHCPv6 multicast address if Renew
  did not complete.
- Do not fake Renew if routerd lacks a valid Server Identifier. In that case,
  fall back to Solicit with a prefix hint and log that the exchange is a new
  acquisition attempt.
- Treat Release as an explicit administrative action, not as the default
  shutdown behavior.
- Follow RFC retransmission rules, but allow the NTT profile to use a longer
  acquisition window because the PR-400NE may respond only during or shortly
  after a home-gateway restart in the observed lab condition.
- Log every packet-level transition with message type, transaction ID, DUID,
  IAID, requested hint, delegated prefix, T1/T2, lifetimes, and whether the
  reply source port was non-547.
- Emit events when a prefix is acquired, renewed, rebound, lost, or when the
  server ignores the requested hint and delegates a different /60.

### Backlog Items from This Review

- Add an explicit `ntt-flets-with-hikari-denwa` DHCPv6 client profile that
  defaults to `/60`, stable DUID-LL, stable IAID, `hintFromState: true`,
  `sendRelease: false`, IA_PD-only, Rapid Commit disabled, long initial
  acquisition timeout, and inbound UDP destination 546 matching without source
  port restriction.
- For OS-backed clients, NTT DUID rendering is an owned artifact:
  systemd-networkd renders `DUIDType=link-layer` by default for NTT profiles.
  FreeBSD KAME `dhcp6c` manages `/var/db/dhcp6c_duid` for the same profiles;
  when the file is not DUID-LL, routerd backs it up and writes a DUID-LL
  derived from the uplink MAC before starting `dhcp6c`.
- Add packet-capture based integration tests for Solicit with exact prefix
  hint, Solicit with length-only hint, Renew with current IA_PD, Rebind after
  T2, and Advertise from a non-547 source port.
- Keep transix AFTR DNS resolution independent from PD state, but allow a
  route policy to depend on the state variable that says usable IPv6 upstream
  connectivity exists.
