# BGP policy and path ownership in routerd 1.5

`routerd-bgp` owns policy replacement and locally originated paths. `routerd`
supplies complete typed policy intent, including dynamic peers, through the
private control API. The daemon serializes policy, path and journal updates.
The controller waits for restoration to finish before starting BGP reconciliation.

Import and export policies use neighbor-scoped global assignments with default
reject. Replacement installs a reject guard before changing policies and removing
old assignments. An interrupted replacement retains its pending intent and guard;
it cannot be acknowledged as applied. Policy statement order is significant.

Local path acquisition and withdrawal record pending operations before calling
GoBGP. Recovery matches the exact source and prefix, including a lost returned
UUID. Static paths and Mobility paths have distinct native source identities;
recovery must preserve peer-origin and other locally originated paths. A live RIB
observation is not a replacement for the daemon's desired-path journal.

## Configuration and cutover

`communities.send` accepts `both` (the default), `standard`, and `extended`.
The pinned local GoBGP v4.7.0 correction preserves this selection through the
peer/group API boundary and filters each neighbor's outbound path after export
policy. `standard` omits both extended-community attribute forms; `extended`
omits standard communities. Large communities are independent and remain
advertised in all three modes. Filtering uses a cloned path and leaves the local
RIB and other peers intact. See `third_party/gobgp/ROUTERD_CHANGES.md` for the
source provenance and four changed upstream files. Raw TCP BGP peer regressions
check actual UPDATE attributes and withdrawals for static and dynamic peers.
`communities.accept` and `communities.set.in` are unsupported and rejected.
Standard community values accept decimal integers, `high:low`, `internet`,
`no-export`, and `no-advertise`; initial application and restoration use the same
encoder. Import prefix-length bounds require explicit `allowedPrefixes`.

The daemon's `--state-file` uses private journal format **3**. Older journals,
unknown fields, malformed values and incomplete control acknowledgements are
errors. There is no migration reader. For an intentional cutover, stop the old
routerd and BGP daemon, preserve any required diagnostic copy, and move the old
private journal aside before starting the new pair with rewritten configuration.
Do not remove a running owner's journal as a recovery procedure. Control clients
and the daemon must come from the same release.

## Kernel FIB ownership

The BGP controller serializes watch updates, reconciliation and removal through
one native FIB owner. Its typed SQLite journal records exact applied routes and a
pending operation before each effect. Linux uses an exclusive add and an exact
route tuple; FreeBSD records each gateway operation separately so partial ECMP
withdrawal cannot discard the remaining hops. A BGP protocol number alone never
establishes ownership.

After a durable write, the owner rereads current RIB/Mobility authority and native
topology before acting. A restarted pending add or delete with matching presence
is uncertain and is not replayed. Withdrawal and shutdown preserve their records
until native absence and actual watcher/in-flight-operation exit are acknowledged.

Each actual kernel network scope has a separate SQLite object named
`Router/bgp-fib-<sha256(scope)>`. The scope includes Linux boot and network
namespace identity, or FreeBSD boot, jail and actual FIB identity. Reads request
one scope explicitly and reject a stored payload whose scope differs from its
key. Reconciliation and removal inspect only the current scope. Old-world
receipts, including pending effects, remain untouched; absence in this namespace
does not prove that another namespace or kernel is gone.

After an ordinary reboot, the new scope can acquire routes with new exclusive
adds. A matching native route already present in the current scope is foreign
unless that scope has its own receipt. It is never adopted using an old scope's
receipt or pending operation.

This is a private SQLite layout cutover. There is no reader or migration for the
previous unscoped `Router/bgp-fib` row. Before changing releases while old native
routes remain, complete withdrawal with their old owner, stop that owner, and
preserve any required diagnostic state. Do not delete an active owner's records
to bypass a collision or uncertainty fence.

Native FreeBSD deletion lacks an atomic complete tuple compare-and-delete
against unrelated external writers. A complete immediate read detects known
ambiguity but does not eliminate that native API limitation.

## Validation limits

Local checks cover real GoBGP loopback session advertisement, export edits and
withdrawal, ordered policy repair, static/dynamic restart recovery, and shared
RPC lifetime, typed SQL corruption/recovery and current-intent/native-operation
failure ordering. Independent Linux/FreeBSD peer traffic and native forwarding still
require isolated target qualification; a successful policy RPC does not prove
packet forwarding.

## Daemon process admission

Before state, socket or native effects, one daemon takes a nonblocking native
lock for the actual kernel network world in the platform runtime directory.
Custom public state/socket paths cannot create a second owner. All three public
paths are checked against the locked path and inode before effects; a normalized
path or inode alias cannot unlink or atomically replace the owner's lock.

After GoBGP construction, the raw CLOEXEC descriptor remains held until actual
process exit, including an unacknowledged stop. It has no Go finalizer. The
upstream private RPC listener lifetime is not completely observable through its
current public API; successful core stop alone does not justify early lock
release. Private/public RPC socket ownership remains a separate repair scope.

Actual child-process regressions cover duplicate admission, alternate paths,
lock aliases, retained pending files and release at process exit. These are
local lifecycle checks, not native forwarding qualification.
