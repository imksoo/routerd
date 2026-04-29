# Design Notes

This document records design decisions and lab findings that are not yet stable
resource definitions. Because this file is public, lab-specific prefixes, MAC
addresses, DUIDs, and private addresses are replaced with documentation values.

## Evidence Language

The document uses the following terms to keep facts separate from assumptions.

| Term | Meaning |
| --- | --- |
| assert | A routerd design decision or implementation direction. |
| believe | An inference based on indirect evidence. It may be revised. |
| observe | Behavior seen at a point in time. Reproducibility is separate. |
| measure | A value confirmed from tcpdump, logs, status tables, or counters. |
| cite | A summary or reference from an RFC, official specification, or public document. |

If a statement cannot fit one of these labels, move it to the open-issues
section or remove it.

## 1. Verified Facts

### 1.0 Source of Truth and Operation Path

assert: routerd's source of truth is the YAML configuration plus the state and
ownership records written by `routerd apply`. Operators should change router
behavior by editing files and running apply, not by hand-editing generated
files or nudging daemons outside the apply path. This keeps operational intent
reviewable through git history, file diffs, apply output, and the local
database.

assert: Host services remain owned by the operating system's service manager.
routerd should use `systemctl` on systemd hosts and `service` / rc.d on
FreeBSD, instead of sending ad-hoc signals to long-running daemons as the
normal control path. Direct signals are acceptable only as short-lived
debugging probes, and the final verification must be repeated through
`routerd apply --once`.

assert: Renderer changes are not considered tested until the rendered files
have passed through the same apply path that production uses. Checking
`routerd render` output is useful, but it is only a preflight step; it does not
exercise ownership records, service-manager behavior, dependency ordering, or
the host's own diagnostics.

### 1.1 RFCs and Public Specifications

- cite: RFC 8415 uses Solicit, Advertise, Request, and Reply for the normal
  DHCPv6 exchange. Rapid Commit can shorten the exchange, but routerd does not
  require Rapid Commit for the NTT profile.
- cite: RFC 8415 defines UDP 546 for clients and UDP 547 for servers and
  relays. This defines listener ports; it does not mean every Advertise/Reply
  must have source port 547.
- cite: RFC 8415 allows IA_PD to contain an IA Prefix as a prefix or length
  hint. The server treats this as a hint.
- cite: RFC 8415 defines Renew as a request to the original server, Rebind as a
  request to any server after Renew does not complete, and Solicit as a fresh
  acquisition path. Confirm is for address validation and is not a PD recovery
  primitive for routerd.
- cite: NTT East and NTT West public FLET'S interface documents describe
  client DUIDs as DUID-LL or DUID-LLT derived from MAC addresses. DUID-EN and
  UUID-derived DUIDs are outside that documented terminal model.
- cite: The same documents describe cases where DHCPv6 does not provide a
  128-bit address. routerd therefore does not request IA_NA by default in the
  NTT profile.
- cite: Rapid Commit appears in the NTT option tables as unused for the relevant
  specification. The NTT profile keeps Rapid Commit disabled by default.
- cite: Sorah's Diary reported in 2017 that non-DUID-LL Solicit packets were
  silently ignored in one FLET'S observation. That is a field report, not an
  official specification. routerd treats DUID-LL as the strict default for NTT
  profiles, not as a universal DHCPv6 rule.
- cite: NEC IX public examples show DHCPv6-PD use in Hikari Denwa environments
  and downstream advertisement of delegated prefixes.
- cite: Commercial and open router platforms treat DUID, IAID, lease state,
  delegated prefixes, and downstream advertisement as operational state.
  routerd should expose the same class of information.

### 1.2 NTT Home-Gateway Profile Shape

measure: In a NTT home-gateway LAN-side delegation environment, successful
clients used DUID-LL, IA_PD, Rapid Commit disabled, and `/60` delegated
prefixes. DHCPv6 Advertise/Reply packets arrived at UDP destination 546, and
captures must not assume source port 547.

measure: A commercial router's initial Solicit did not include a prefix hint.
routerd therefore omits exact and length-only prefix hints for
`ntt-ngn-direct-hikari-denwa` and `ntt-hgw-lan-pd`. Exact hints are not modeled
as generally harmful, but they are no longer the default shape.

assert: DUID-LL is a strong default for NTT profiles. Option-request contents,
Reconfigure Accept, and Client FQDN can differ between working clients, so
routerd does not treat those fields as the deciding profile knobs.

assert: The `ntt-ngn-direct-hikari-denwa` and `ntt-hgw-lan-pd` profiles should
not send exact or length-only prefix hints by default. `prefixLength` remains
part of routerd's expected-shape model, but the systemd-networkd renderer omits
`PrefixDelegationHint=` for these profiles.

### 1.3 OS Client Behavior

| Client | Cited or measured behavior | routerd treatment |
| --- | --- | --- |
| systemd-networkd | cite/measure: Supports `DUIDType=link-layer`, `IAID`, `PrefixDelegationHint`, and `WithoutRA`. Renew/Rebind IA Prefix lifetimes are zero. | Keep as the default Linux path. routerd compensates for weak notification and state visibility with observation. |
| KAME/WIDE `dhcp6c` | cite/measure: Stores DUID in a file and IAID/IA_PD in config. Hint-bearing Solicit can carry IA Prefix lifetimes. | Keep as the FreeBSD DHCPv6-PD path. routerd manages DUID-LL files for NTT profiles. |
| dnsmasq | cite/assert: Useful for LAN DNS, DHCPv4, DHCPv6, and RA. It is not the source of truth for WAN PD acquisition. | Keep it for LAN services only. |

assert: DHCPv6-PD acquisition is intentionally narrow: Linux uses
systemd-networkd and FreeBSD uses KAME/WIDE `dhcp6c`.

assert: NTT profiles default to real MAC-derived DUID-LL. `duidRawData` is an
explicit override for HA failover, router replacement, or migration. It is not
the default lab recovery path.

## 2. Lab-Specific Issues

### 2.1 Multicast Transparency in Virtual Labs

observe: The lab routers are virtual machines on Proxmox. With Linux bridge
`multicast_snooping` enabled, IPv6 RA and DHCPv6 multicast traffic may be
missing or partially visible.

cite: Public Proxmox reports and lab articles describe IPv6 RA/DHCPv6 issues
that are resolved by disabling bridge multicast snooping.

assert: Before judging DHCPv6-PD behavior in routerd labs, verify:

- Proxmox bridges pass IPv6 multicast.
- L2 switches in the path do not block MLD/IGMP-related multicast traffic.
- Captures include `udp port 546 or udp port 547`, plus separate RA capture.
- A working router's Solicit/Request can be seen on the same segment before
  concluding that the HGW is not replying.

### 2.2 L2 Switch Multicast Snooping

observe: When L2 switches in the path had IGMP snooping enabled, parts of the
IPv6 RA and DHCPv6 multicast exchange were not delivered. Many implementations
tie IGMP snooping and MLD snooping together, so a setting that was meant to
optimize IPv4 multicast can block IPv6 ND/DHCPv6 paths during validation.

assert: For routerd lab validation, disable snooping so that multicast flows
flat. If snooping must remain enabled in production, alternatives are:

- Place an MLD Querier on the segment so that hosts emit Listener Reports and
  the snooping tables stay populated.
- Split the topology so that snooping is disabled only on the routerd-facing
  VLAN, while other VLANs keep it.

believe: At lab scale, disabled snooping is the practical choice. The added
flooding is acceptable and root-cause separation is faster.

observe: Concrete switch configuration depends on each vendor's UI/CLI. Switch
model names and exact commands are intentionally not included in this
document.

## 3. Public References

Primary references:

- [RFC 8415: Dynamic Host Configuration Protocol for IPv6](https://www.rfc-editor.org/rfc/rfc8415.html)
- [RFC 9915: Dynamic Host Configuration Protocol for IPv6](https://datatracker.ietf.org/doc/html/rfc9915)
- [NTT East technical reference](https://www.ntt-east.co.jp/gisanshi/)
- [NTT East FLET'S IP interface, volume 3](https://flets.com/pdf/ip-int-3.pdf)
- [NTT West IP interface document](https://www.ntt-west.co.jp/info/katsuyo/pdf/23/tenpu16-1.pdf)
- [Yamaha RT DHCPv6 documentation](https://www.rtpro.yamaha.co.jp/RT/docs/dhcpv6/index.html)
- [Yamaha IPv6 IPoE documentation](https://www.rtpro.yamaha.co.jp/RT/docs/ipoe/index.html)
- [NEC UNIVERGE IX FLET'S IPv6 IPoE example](https://jpn.nec.com/univerge/ix/Support/ipv6/native/ipv6-internet_dh.html)
- [NEC IX-R/IX-V DHCPv6 documentation](https://support.necplatforms.co.jp/ix-nrv/manual/fd/02_router/14-1_dhcpv6.html)
- [Sorah's Diary DHCPv6-PD observation](https://diary.sorah.jp/2017/02/19/flets-ngn-hikaridenwa-kill-dhcpv6pd)
- [rixwwd DHCPv6 packet observation behind an NTT home gateway](https://rixwwd.hatenablog.jp/entry/2023/04/09/211529)
- [SEIL NGN IPv6 native IPoE example](https://www.seil.jp/blog/10.html)
- [systemd.network manual](https://www.freedesktop.org/software/systemd/man/254/systemd.network.html)
- [FreeBSD dhcp6c(8)](https://man.freebsd.org/cgi/man.cgi?manpath=freebsd-release-ports&query=dhcp6c&sektion=8)
- [FreeBSD dhcp6c.conf(5)](https://man.freebsd.org/cgi/man.cgi?query=dhcp6c.conf)
- [pfSense advanced networking documentation](https://docs.netgate.com/pfsense/en/latest/config/advanced-networking.html)
- [OPNsense DHCP documentation](https://docs.opnsense.org/manual/isc.html)
- [MikroTik RouterOS DHCP documentation](https://help.mikrotik.com/docs/display/ROS/DHCP)
- [Cisco IOS XE DHCPv6 Prefix Delegation](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/ipaddr_dhcp/configuration/xe-16-9/dhcp-xe-16-9-book/ip6-dhcp-prefix-xe.html)
- [Juniper Junos IA_NA and Prefix Delegation](https://www.juniper.net/documentation/us/en/software/junos/subscriber-mgmt-sessions/topics/topic-map/dhcpv6-iana-prefix-delegation-addressing.html)

## 4. Known Limits and Open Questions

### 4.1 DHCPv6-PD

- believe: Some NTT paths may silently ignore DUID-LLT. NTT public documents
  still allow DUID-LLT, so this remains implementation-specific, not official.
- assert: An in-process routerd DHCPv6 client remains a later option. First,
  stabilize DUID, IAID, lease storage, and events around OS clients.

### 4.2 State and Ownership Storage

routerd stores local state and ownership in SQLite. The default path is
`/var/lib/routerd/routerd.db` on Linux and `/var/db/routerd/routerd.db` on
FreeBSD.

| Table | Role |
| --- | --- |
| `generations` | One row per apply attempt, including result, warnings, and config hash. |
| `objects` | Per-resource status JSON. Example: `IPv6PrefixDelegation/wan-pd` lease, DUID, IAID, and timestamps. |
| `artifacts` | Ownership ledger for host artifacts managed by routerd. |
| `events` | Apply warnings and prefix-observation events. |
| `access_logs` | Reserved for future local HTTP API auditing. |

JSON is stored as text and can be inspected with SQLite JSON1:

```sh
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.lastPrefix') from objects where kind = 'IPv6PrefixDelegation' and name = 'wan-pd';"
```

The `sqlite3` command is not required to run routerd, but it is useful for
human debugging. `jq` remains because trusted local plugins use JSON through
standard input and output.

### 4.3 Host Inventory

routerd records one observed host object at apply time:
`routerd.net/v1alpha1/Inventory/host`. The status JSON contains the Go OS
name, kernel information from `uname`, virtualization detection, best-effort
DMI values, the detected service manager, and availability of selected host
commands such as `nft`, `pf`, `dnsmasq`, `dhcp6c`, and `sysctl`.

assert: Inventory is an observed object, not a desired resource. It does not
appear in normal authored `spec.resources`, and renderers do not use it in the
first implementation. The reason to record it now is to make later platform
decisions explicit: physical versus virtual hosts, systemd versus rc.d, and
host-level prerequisites such as bridge multicast behavior should be based on
observed facts rather than guessed in each renderer.

Use:

```sh
routerctl describe inventory/host
```

### 4.4 Future Design Work

- Observe natural DHCPv6-PD Renew/Rebind behavior on Linux systemd-networkd
  and FreeBSD `dhcp6c` clients after the current profile cleanup. Record what
  routerd can reliably expose as status without managing the client timers.
- Improve `IPv6PrefixDelegation` status when OS clients expose T1/T2 or
  lifetime data. Current and last prefixes, observed and expected DUID/IAID,
  last observed time, and warnings should remain distinct.
- Design how LAN RA/DHCPv6 withdraws stale prefixes when PD disappears.
- Use `Inventory/host` as an input to future platform advice or rendering for
  host prerequisites such as virtual bridge multicast behavior, RA acceptance,
  and service-manager differences.
- Decide whether the remaining example plugin stubs should stay under
  `plugins/` or move fully into test fixtures once the trusted local plugin
  protocol has enough real examples.
- Keep "DS-Lite without delegated LAN IPv6" as a design candidate. It changes
  ownership and firewall boundaries, so it needs separate validation before
  implementation.
