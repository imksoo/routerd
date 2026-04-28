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
