---
title: BFD with FRR
---

# BFD with FRR

On Linux and FreeBSD, `BFD` resources configure an existing FRR `bfdd`
service through `vtysh`. The BFD controller does not install, start or supervise
that service. The selected platform must provide `bfdd`, its VTY socket and
permission to run `vtysh` (`/usr/local/bin/vtysh` on FreeBSD).

`spec.peer` accepts an address or a `BGPPeer/name` reference. With
`spec.interface`, routerd configures a single-hop peer on the resolved native
interface. Without it, routerd configures a multihop peer using the referenced
`BGPRouter.spec.routerID` as its local address. `profile`, `minRx`, `minTx` and
`detectMultiplier` select the local timers. Authentication, echo and demand-mode
configuration are not exposed by this API.

For example, a `BGPPeer` can set `spec.bfd: BFD/upstream`, with the following
resource alongside it:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: BFD
metadata:
  name: upstream
spec:
  peer: BGPPeer/upstream
  minRx: 100ms
  minTx: 100ms
  detectMultiplier: 3
```

The configured local address must exist and reach the independent peer. A BFD
`Down` observation hard-resets the referenced BGP session after it has been up
or while BGP is established. It retains the configured BGP peer, allowing BGP
to reconnect. It does not permanently block BGP establishment while BFD is down.
Unknown BFD observations do not invent a new `Down` event.

The controller records pending and held native tuples before applying FRR
configuration. It refuses adoption of an existing unowned tuple. Observations
combine `show bfd peers json` with `show bfd peers`: FRR 8.4.4 emits peer type
only in the text display. The complete header and local timers must agree;
missing, changed or unreadable observations preserve ownership and return an
error. Removal withdraws only the acknowledged tuples and leaves unrelated
configured peers in place. Other configuration writers must be coordinated;
the FRR CLI does not provide an atomic compare-and-swap operation.

A pending tuple can authorize restart recovery only after routerd durably
records a successful `vtysh` acknowledgement and matching native readback.
An attempt aborted before dispatch retracts its own unchanged pending receipt.
If that save fails, or command completion remains uncertain, a matching live
tuple is retained as an unacknowledged attempt. Tuple/timer equality alone
cannot promote it to held ownership or authorize its withdrawal.

Run the explicit Linux acceptance from the repository root:

```sh
GO=/path/to/go sh scripts/linux-bfd-frr-smoke.sh
```

The script requires FRR `bfdd` and `bgpd`, `vtysh`, user/network/mount namespaces
and the listed native tools. It uses private network namespaces, sysfs,
`/run` and `/var/tmp`, plus a private bind-mounted `/etc/group` view for FRR's
VTY group check.
It does not install packages, register named namespaces or change host network
configuration. Both daemons and each namespace process are directly owned,
stopped and reaped by the tests. Evidence is written to the printed temporary
directory, including source hashes, revision, versions and execution results.

The acceptance exercises IPv4 multihop BFD reception, peer stop/restart,
administrative shutdown, local `bfdd` outage/restart, SQLite reopen, exact
withdrawal and unrelated-peer retention. Its BGP stage uses the production
`routerd-bgp` binary: BFD failure removes the referenced IPv4 BGP session,
RIB/FIB route and reachability while an unrelated IPv6 BGP session continues
forwarding. Recovery and removal of the BFD configuration preserve both BGP
families, followed by the existing independent-FRR BGP lifecycle checks.
This does not qualify IPv6 BFD, single-hop BFD, FreeBSD interoperability,
authentication, performance or hardware failure detection.
