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
