# 3-role firewall audit - 2026-05-09

## Scope

Re-check the 3-role firewall model after the router04 FreeBSD NAT parity work:

- `untrust`
- `trust`
- `mgmt`

The check covered homert02 on Linux/nftables and router04 on FreeBSD/pf.

## homert02

Host:

- OS path: Linux + nftables
- management SSH: `192.168.123.129`
- routerd status after latest binary swap:
  - phase: `Healthy`
  - generation: `44`
  - resource count: `88`
- egress policy:
  - selected candidate: `ds-lite-a`
  - selected device: `ds-lite-a`
  - production traffic stayed on the DS-Lite primary path.

Observed nftables table:

- `table inet routerd_filter`
- `input` chain:
  - hook input, policy `drop`
  - drops `ct state invalid`
  - accepts `ct state established,related`
  - accepts loopback and ICMPv6
  - jumps to `lan_to_self`, `management_to_self`, and `wan_to_self`
  - final log/drop rule: `routerd firewall input deny`
- `forward` chain:
  - hook forward, policy `drop`
  - drops `ct state invalid`
  - accepts `ct state established,related`
  - accepts ICMPv6
  - jumps across the role matrix:
    - `lan_to_lan`
    - `lan_to_management`
    - `lan_to_wan`
    - `management_to_lan`
    - `management_to_management`
    - `management_to_wan`
    - `wan_to_lan`
    - `wan_to_management`
    - `wan_to_wan`
  - final log/drop rule: `routerd firewall forward deny`

Observed role chains:

| Chain | Result |
| --- | --- |
| `lan_to_self` | DHCPv4, DHCPv6, DNS holes, then accept |
| `lan_to_lan` | accept |
| `lan_to_management` | explicit `192.168.123.126:8080/tcp` accept, then log/drop |
| `lan_to_wan` | accept |
| `management_to_self` | DNS holes, then accept |
| `management_to_lan` | accept |
| `management_to_management` | accept |
| `management_to_wan` | accept |
| `wan_to_self` | Tailscale UDP, DHCPv6 client/info holes, then log/drop |
| `wan_to_lan` | log/drop |
| `wan_to_management` | log/drop |
| `wan_to_wan` | accept |

Counters after the routerd restart showed live traffic:

- `input ct established,related`: non-zero
- `forward ct established,related`: non-zero
- `lan_to_self` DNS: non-zero
- `lan_to_wan`: non-zero
- `wan_to_self` deny: non-zero

NAT table:

- `table ip routerd_nat`
- per-interface NAT exists for:
  - `ds-lite-a`
  - `ds-lite-b`
  - `ds-lite-c`
  - `ds-lite-ra`
  - `ppp-flets`
  - `ens18`

The latest binary was deployed to homert02 during this audit because the
previous running binary rendered the PPPoE NAT rule with the resource name
`pppoe-flets`. The current table now renders the OS interface name
`ppp-flets`.

## router04

Host:

- OS path: FreeBSD + pf
- management SSH: `192.168.123.126`
- routerd status:
  - phase: `Healthy`
  - generation: `1081`
  - resource count: `73`
- egress policy:
  - selected candidate: `ds-lite-a`
  - selected device: `gif41`

Observed pf rules:

- `block drop all` is present.
- TCP MSS scrub is present for LAN ingress and DS-Lite/PPPoE egress:
  - `vtnet1`
  - `gif41`
  - `gif42`
  - `gif43`
  - `gif44`
  - `ppp-flets`
- Default stateful accepts:
  - loopback
  - outbound from self
  - ICMPv6
- Service holes are labelled with `routerd:*`.
- `lan-to-mgmt-deny` blocks `vtnet1`/`wg0` to the management network.
- `lan-to-lan` accepts trust-to-trust traffic.
- `lan-to-wan` accepts trust-to-untrust traffic.
- `mgmt-to-lan`, `mgmt-to-mgmt`, and `mgmt-to-wan` accept management traffic.
- There is no broad `wan-to-lan` or `wan-to-management` pass rule. Those paths
  fall through to `block drop all`.

pf counters:

- `block drop all`: non-zero packet counter.
- `pass out quick all`: non-zero packet counter.
- `pass quick inet6 proto ipv6-icmp`: non-zero packet counter.
- state table current entries: observed around `60-73` during the audit.

NAT:

- `pfctl -sn` shows per-interface NAT for:
  - `gif41`
  - `gif42`
  - `gif43`
  - `gif44`
  - `ppp-flets`
  - `vtnet0`
- RFC 1918 destinations are excluded from NAT.

## Semantic comparison

| Direction | homert02/Linux nft | router04/FreeBSD pf | Result |
| --- | --- | --- | --- |
| invalid state | drop | falls under pf state/default drop | Equivalent intent |
| established/related | explicit accept | pf keep-state on pass rules | Equivalent stateful model |
| loopback | accept | accept | Equivalent |
| ICMPv6 | accept | accept | Equivalent |
| trust -> self | DHCP/DNS/NTP holes plus accept | service holes plus trust-to-self pass | Equivalent intent |
| mgmt -> self | accept after service holes | management path allowed | Equivalent intent |
| untrust -> self | only required holes, then drop | only required holes, otherwise default drop | Equivalent intent |
| trust -> trust | accept | accept | Equivalent |
| trust -> mgmt | default drop with homert02-specific router04 Web UI exception | default drop | Intentional environment difference |
| trust -> untrust | accept | accept | Equivalent |
| mgmt -> trust | accept | accept | Equivalent |
| mgmt -> mgmt | accept | accept | Equivalent |
| mgmt -> untrust | accept | accept | Equivalent |
| untrust -> trust | drop | default drop | Equivalent |
| untrust -> mgmt | drop | default drop | Equivalent |
| untrust -> untrust | accept | not broadly used except self outbound/state | Acceptable difference for current FreeBSD lab |

## Findings

- No emergency hole was found that allows untrust to reach LAN or management.
- homert02 has an intentional extra hole: LAN clients may reach router04 Web UI
  at `192.168.123.126:8080`. This is specific to the current lab workflow.
- FreeBSD pf does not expose the model as named chains. The same model is
  represented by ordered rules and labels.
- homert02 production traffic remained on `ds-lite-a`.
- SSH management paths stayed available on both hosts.

## Follow-up candidates

- Render fewer duplicate pf service-hole rules on FreeBSD by constraining holes
  to the actual service listen addresses instead of every self address.
- Add a small `routerctl firewall matrix` view so the semantic matrix can be
  audited without reading nft/pf syntax directly.
