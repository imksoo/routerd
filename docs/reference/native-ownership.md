# Native ownership and interrupted operations

The routerd 1.5 native address and route owners distinguish desired configuration,
native observations and durable operation receipts. An `Applied` display field or
a matching address/route is not an acquisition receipt. Existing foreign objects
must be removed or managed explicitly outside routerd before acquisition; ordinary
reconciliation does not intentionally overwrite an observed foreign tuple.

Linux IPv4 acquisition uses the kernel's exclusive address-add operation.
FreeBSD 14.3 has an important API limit: both the ifconfig ioctl and default
Netlink paths implement IPv4 address creation as create-or-replace. The kernel
explicitly does not honor `NLM_F_EXCL` for this operation. A pre-read can reject
an observed foreign address, but another writer can insert one between that read
and our add, and the FreeBSD call can then replace it. The routerd scope lock
coordinates routerd processes, not unrelated native writers. Do not interpret a
FreeBSD IPv4 address receipt as proof of atomic exclusive acquisition against
such writers. This shared limitation applies to ordinary and Mobility addresses;
it is not fixed by another pre-check or a speculative CLI flag. See the
[14.3 kernel implementation](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netlink/route/iface.c#L1324)
and [ifconfig add path](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sbin/ifconfig/af_inet.c#L401).
The inspected source blob hashes were respectively
`d98385a4c23e26ce9c52161af635f72ef1616312` and
`e21956cfc4fd29e24d8678c1d01f16378daf03ff`.
FreeBSD route deletion likewise lacks an atomic full-tuple compare-and-delete;
current native checks reduce, but cannot remove, that unrelated-writer race.

`serve`, one-shot apply and rollback share an exclusive native-operation lock for
the actual boot and network namespace or FreeBSD jail. Different config, database
or socket paths cannot create a second owner for the same native scope. Use the
running owner's control API, or stop it and wait for exit before starting another
mutating command. Read-only validation and history inspection do not take this
lock. An unacknowledged shutdown retains ownership until actual process exit.

## Recovery without a native ownership cookie

Ordinary static IPv4 addresses, ordinary IPv4 routes and delegated LAN addresses
record pending intent before an add or delete. If an interrupted operation leaves
the same native tuple present, presence alone cannot establish its creator: our
add may never have run, or another writer may have recreated it after our delete.
The owner preserves the pending receipt and reports uncertainty instead of
adopting, refreshing or automatically deleting that tuple.

Inspect the recorded interface, address or route, kernel scope and operation with
the native tools and routerd status. Resolve the ambiguous native object under
operator control. A subsequent complete observation of absence permits the owner
to retire the pending receipt, or acquire the current desired object anew. Do not
clear the database as a substitute for resolving a possibly live native effect.
Read errors retain uncertainty; they do not confirm absence.

For delegated addresses, the acknowledged valid/preferred deadlines remain the
authority until a refresh succeeds. An expired address that is still present may
have been replaced by another writer; it is not automatically reclaimed. Recovery
does not extend prefix validity. Interface replacement and reboot invalidate the
old native identity rather than authorizing mutation of its replacement.

Child address and route receipts on either VXLAN kind also retain the parent's
native token. Removing the parent declaration does not remove the proof needed
for cleanup: the recorded parent must still match its cookie, scope and interface
index. An unrecorded or foreign replacement retains an uncertain child receipt
and cannot authorize a delete.

This contract describes these concrete owners. Mobility, BGP kernel FIB, tunnel
cookies and policy-routing effects have separate recovery boundaries; their
implementation and qualification status is tracked in the
[open findings](../design/routerd15-open-findings.md).

## Policy routing

The policy controller owns exact IPv4/IPv6 default-route and rule receipts in
`Router/ipv4-policy-native` and `Router/ipv6-policy-native`. Their format 2 shares
one typed effect/receipt implementation and records address family, kernel scope,
complete selector or route tuple, preferred source, interface index for routes,
and pending add/delete. Old native journal or IPv6 post-effect file state is not
read or migrated; cutover must resolve possibly live old effects explicitly.

Routes acquire with `ip route add`; rules never delete a priority wholesale.
Withdrawal removes selectors before routes, matches full selectors including
protocol, and never flushes a shared route table. Source changes likewise stop
selectors, withdraw the previous exact route, acquire its replacement and then
restore selectors. Numbered table/mark ranges do not establish ownership and are
not offered for generic artifact adoption or cleanup. IPv6 physical-WAN host
traffic uses the local-input selector while explicit delegated DS-Lite sources
use their source-specific main-table rule. IPv6 host-policy state is no longer
written after native effects to a separate JSON file.

A pending effect that is still present is uncertain, including after a lost
response or failed acknowledgement write. Reopening the database does not adopt
or replay it. An exact absence observation releases the pending fence; required
read failures preserve it. Native scope and current intent/source are checked
again after pending persistence and before mutation. Known absence of an eligible
IPv6 source withdraws its owned selector/route and reports Pending; an address
inventory failure cannot authorize that withdrawal. Priority selection is derived
after route persistence/effects so a previous candidate snapshot cannot survive
that wait.

A foreign same-priority rule with a disjoint mark remains untouched. An overlapping
selector, even with a different lookup table or wider mark mask, blocks acquisition
before any route or rule changes. Same-slot foreign routes also block acquisition.
These checks do not establish atomic compare-and-delete against an unrelated
kernel writer. Linux rules permit concurrent insertion; ambiguous duplicate
readback retains pending uncertainty.

Native default-route observation uses numeric `table all exact default`, which
observes an empty requested table without converting an arbitrary command failure
into absence. It treats omitted table identity as main (254), filters other tables
before interpreting route attributes, and accepts numeric static protocol (4) and
link/global scopes (253/0). These output rules were checked against upstream
[iproute2 v6.1.0 iproute.c](https://github.com/iproute2/iproute2/blob/v6.1.0/ip/iproute.c#L876)
and [numeric names](https://github.com/iproute2/iproute2/blob/v6.1.0/lib/rt_names.c#L416),
matching the locally installed iproute2 version. Other-table RA expiry, preference
or unreachable routes do not become collision evidence for the requested table.

Executed checks use actual SQLite files and native-command boundary fixtures,
including a rejected pending write, reopen after lost acknowledgement, changed
source/Health authority during persistence, exact teardown and family separation.
Native Netlink/forwarding qualification remains unexecuted. Delegated DS-Lite
source lifetime/native-authority qualification remains part of the broader source
owner finding; this change establishes the policy effects' ownership boundary.

## Mobility IPv4 routes

The typed Mobility route owner now records the current kernel scope, interface
index and virtual-parent cookie with its exact route tuple. Acquisition uses the
ordinary owner's exclusive native add and exact readback, rather than replace
or adoption by route presence. A changed owned route is withdrawn under its
receipt before the new tuple is acquired. Required source or native observation
failures preserve the previous confirmed effect and any pending uncertainty.

Pending add/delete records are retired only by an explicit acknowledged result
or exact absence in their recorded native world. An acknowledgement and its
applied ledger update share one SQLite write; retaining an older equal applied
value cannot resolve a failed new acknowledgement. A different scope or recycled
interface cannot authorize cleanup or retirement of the old pending operation.
Legacy Mobility route rows without a native-world receipt require explicit
operator resolution or the authorized new-format cutover; they cannot acquire
ownership of an existing route.

The production Runner supplies the current typed DynamicConfigPart source.
Acquisition and withdrawal read that source again after persistence, checking
actual current expiry/revocation as well as native identity and preferred-source
availability. If intent recovers while deletion persistence waits, the known
receipt is retained without deleting its route. If intent changes during the
acquisition acknowledgement, the owner attempts to withdraw that acknowledged
effect. Required read failures remain errors and cannot fabricate withdrawal ACKs.

SQLite/native-command boundary regressions exercise the source changes, native ignored
add, lost acknowledgement, new kernel world and restored deletion intent. Native
Linux/FreeBSD forwarding remains unqualified. FreeBSD does not report Linux route
metrics or preferred source through its exact withdrawal observation, so logical
Mobility metrics are not native ordering/ownership proof there; the receipt uses
the observable prefix/interface/gateway tuple, and preferred source is not
ownership proof.
Mobility IPv4 addresses retain a separate implementation/qualification boundary.
## Shared native VXLAN owner

`VXLANTunnel` and `VXLANSegment` use the same kernel-scope receipt owner for link
creation, flood destinations, bridge attachment, activation and withdrawal. The
private journal format is 2 and records the complete API version/kind/name; older
receipts are rejected. This revision does not adopt links or migrate networkd
ownership markers. Resolve old native links and generated boot creators before
cutover rather than clearing receipts for possibly live effects.

Linux tunnels require `nolearning` and the selected `outerDF` mode (default
`inherit`); both are read back from native attributes. FreeBSD supports its
native single unicast remote or multicast mode and verifies the driver's learning
flag. An explicitly requested `outerDF` mode on FreeBSD is rejected because its
current VXLAN boundary does not expose that control; omission retains the native
FreeBSD behavior. FreeBSD does not silently discard additional unicast peers.

Networkd renders no VXLAN `.netdev`, flood entries, bridge attachment or admin-up
operation. Its `.network` file has manual activation and retains native
configuration so separately declared NTP/static-route inputs can be consumed.
FreeBSD has no separate VXLAN rc boot creator. The native actor is selected as
`vxlan-segment` for either resource kind.

Tunnel `when` input is checked again after persistence, current-selection reads
and native parent observation. Missing/expired/unknown input cannot authorize
activation or child acquisition, and the daemon also requires observations from
its current lifetime. Gate changes retain exact native ownership for withdrawal;
read failures preserve uncertainty. Local SQLite/fault and native-message tests
do not qualify Linux/FreeBSD kernel forwarding or independent peers.
