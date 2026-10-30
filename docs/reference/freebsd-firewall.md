# FreeBSD PF firewall contracts

FreeBSD firewall rules support zone-to-self input, zone-to-zone forwarding and
self-to-zone output. Explicit rules are ordered by resource name, as on Linux;
forward rules and holes run before the implicit role decision. Forward holes
retain their owning ingress interface when a zone contains several interfaces.

The PF renderer tags a forwarded packet at its actual ingress interface with a
stateless pass. An outbound anchor then matches that tag and the actual egress
zone interface. Only an authorized outbound pass creates forwarding state.
An ingress-tagged packet that reaches no destination zone is blocked before
local output rules or the local output default. Destination subnets are not a
substitute for the selected egress interface. Input anchors match `(self)` so
native address changes remain visible without rendering a stale address list.
Generated anchor components, packet tags and rule labels fit FreeBSD 14.3's
63-byte content limit. Overlong internal names use a stable digest of their full
name; operator resource names keep their existing configuration limits.

DS-Lite references in zones, NAT, MSS and internal service exceptions resolve to
the gif interface name chosen by the native tunnel owner. Explicit gif unit
names remain unchanged. Rendering uses the target OS for this resolution.

The root firewall ruleset remains owned by the firewall controller. Runtime
translations remain in `routerd_nat`; SAM keeps its existing forward anchor.
Replacing the root ruleset withdraws a removed explicit rule while retaining
those anchor references. Existing PF states are retained on ruleset replacement;
removing a rule does not flush established connections.

## Supported selectors and limits

Source/destination CIDRs, TCP/UDP source and destination ports, protocol, ICMP
types, logging and accept/drop/reject retain their native meanings. PF outbound
filtering sees the source after SNAT. A rule with an original source CIDR or
source port therefore cannot be expressed by this path when IPv4 SNAT may apply
on its destination zone. Such a combination fails rendering explicitly.
Unrelated egress-zone NAT, provably disjoint or fully NAT-excluded destination
CIDRs, and IPv6-only rules with NAT44 remain supported. Partly excluded
destinations still fail because some matching packets would be translated.
Hairpin checks cover every possible failover backend, including unresolved
backups that may later translate on the configured hairpin interfaces. These
checks preserve the runtime controller's current backend selection.
Destination predicates see the destination after ingress DNAT, as in the Linux
forward path. Managed ingress translations retain their existing narrow
`rdr pass` behavior and precedence.

Address-set references have no PF table owner, packet/byte rate limits have no
equivalent implemented PF limiter, and connection limits are not implemented.
These fields now fail FreeBSD admission instead of being ignored. Loopback is
skipped by the existing root ruleset, so self-to-self filtering also fails
admission. CIDR selectors remain available without address-set references.

The existing restricted hash `EgressRoutePolicy` route-to path is preserved. It
still rejects coexistence with explicit rules, holes, event logging or
ClientPolicy and does not use the new actual-egress forwarding anchors. Its
connected-network approximations are not qualified as general zone parity.
ClientPolicy's existing ingress exceptions remain ahead of ordinary zone
forwarding, matching its documented policy precedence.

## Evidence and remaining qualification

Renderer regressions cover actual ingress/egress anchoring, explicit deny before
implicit accept, local-output separation, IPv6 reject selectors, source/SNAT
rejection, same-role forwarding and interface-scoped holes. Backend regressions
read the actual root file consumed by its command boundary, replace it after a
rule removal and verify NAT/SAM anchor preservation. These checks do not execute
PF or establish packet forwarding on FreeBSD. Native `pfctl -nf`, routed peer
traffic, translation, state teardown and reboot remain required qualification.

The implementation follows FreeBSD 14.3 primary sources:

- [pf.conf(5)](https://github.com/freebsd/freebsd-src/blob/releng/14.3/share/man/man5/pf.conf.5)
  documents default state creation, `no state`, sticky tags, dynamic interface
  addresses and the effect of a quick rule inside an inline anchor.
- [pfctl grammar](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sbin/pfctl/parse.y)
  accepts `tag`/`tagged` and dynamic interface expressions; it has no
  `received-on` selector.
- [PF native field sizes](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netpfil/pf/pf.h)
  allocate 64 bytes for anchor components, tags and labels, including their
  terminator. [PF ruleset creation](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netpfil/pf/pf_ruleset.c)
  rejects an anchor component of 64 bytes or more.
- [PF kernel filtering](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netpfil/pf/pf.c#L4850)
  applies translation before outbound rule matching and carries packet tags
  between hooks. An authorized outbound pass is therefore necessary before
  forwarding state may bypass later rules.
- [PF dynamic interface addresses](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netpfil/pf/pf_if.c#L506)
  maps dynamic `self` to all native interfaces.
