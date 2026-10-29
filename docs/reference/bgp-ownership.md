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

`communities.send` supports `both` (the default), `standard`, and `extended`.
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

## Validation limits

Local checks cover real GoBGP loopback session advertisement, export edits and
withdrawal, ordered policy repair, static/dynamic restart recovery, and shared
RPC lifetime. Kernel FIB ownership and teardown are a separate implementation
boundary. Independent Linux/FreeBSD peer traffic and native forwarding still
require isolated target qualification; a successful policy RPC does not prove
packet forwarding.
