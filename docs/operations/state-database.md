---
title: State database
slug: /operations/state-database
---

# State database

![Diagram showing routerd state database paths, daemon lease and event files, routerctl get events access, and backup philosophy where YAML remains authoritative and event databases provide forensic history](/img/diagrams/operations-state-database.png)

routerd persists state and events in SQLite. Each managed daemon additionally keeps its own lease or state file and an event log.

## Key paths

| Kind | Path |
| --- | --- |
| routerd state DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD lease | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 lease | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE state | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck state | `/var/lib/routerd/healthcheck/<name>/state.json` |
| Per-daemon events | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## routerd 1.5 proposal format

This proposal branch uses one new schema, identified by SQLite `user_version`
1500, for state and managed-artifact ownership. Persistent paths always use
SQLite; the old JSON backend and prototype migrations are removed. Opening an
older database fails with an explicit format error. No automatic reset occurs.

For an authorized cutover, stop the old daemons, retain a complete backup if
needed, select a fresh DB path, and start all routerd/worker binaries from the
same revision with the rewritten configuration. Do not mix old and new workers:
Health IPC now includes a typed `routing` observation required by the consumer.
A Health worker restart restores display history and re-probes before declaring
reachability. DHCP worker stop commands cancel the owning daemon; configuration
changes are applied by the supervisor restarting workers with new arguments. Keep independent console access during network qualification.

The fresh database does not contain the old ownership ledger, pending provider
actions, DHCP callback queue, or historical events. Resolve or explicitly retire
those operations before switching; config alone cannot reconstruct unfinished
external effects. Existing ownership must be inventoried so removal or adoption
is deliberate. This is an operator cutover procedure, not a daemon migration.

State-variable writes now report persistence errors to their operation owner.
PD observation saves and ingress drain/undrain requests fail when their change
cannot be stored. Expired drain records are inactive when read; observing expiry
does not delete operator intent. An explicit undrain removes the record.

Conditional state reads also return storage failures. A closed database or
invalid persisted object JSON stops conditional reconciliation; it is not a new
`unknown` observation and must not authorize application or teardown.

DHCPv6 worker snapshots now retain a `prefixes` collection with individual
lifetimes and a lease-update timestamp. Current/previous selected prefixes are
derived views for LAN renumbering. The old single-prefix snapshot is not a
supported restart format on this branch. The supported PD client is
`routerd-dhcpv6-client`, also selected when `client` is omitted. External-client
selectors such as `networkd`, `dhcp6c` and `dhcpcd` are rejected: they did not
select an external lifecycle in the existing implementation. Rewrite those
selectors at cutover; this revision does not infer lease authority from an OS
address or a client-name label.

The controller stores one typed PD
observation in the object's `status.observation`; generic status updates cannot
replace it. Inspection exposes `pdLease` as a typed control API field. Conditional
status views are derived on read, so an expired lease is not revived by a stale
display projection. Source identity, observation timestamps and explicit release
govern whether a daemon snapshot may update that observation. Lease transitions
and their required EventRule input history commit together.
The complete association is authoritative for live prefixes; local address
receipts retain cleanup identity and cannot revive a prefix the client revoked.

LAN addresses inherit the remaining upstream preferred and valid lifetimes in
the kernel. A renewal refreshes the same address's deadline; stopping the daemon
cannot turn a finite lease into an unlimited address. Protocol infinite
lifetimes remain supported. DS-Lite uses the exact LAN-owned /128 when shared;
its separately derived source has a durable pending/applied receipt and its own
bounded lifetime. Preserve those receipts through failed withdrawal or restart.

DHCPv4 lease-effect ownership includes
pending address, gateway and DNS operations in current state, so a restart can
finish interrupted effects and exact withdrawal. Preserve these records while
cleanup is pending.

BGP removal keeps a `<applied-state-path>.remove-pending` operation until the
owning supervisor has stopped the helper and the owned FIB cleanup succeeds.
Worker restart refuses to restore advertisements while that operation exists.
Do not discard the operation to force an apparently successful removal; correct
the reported supervisor/kernel failure and let the same owner retry.

The BGP helper's applied-state journal is version 3 on this branch. Static and dynamic path
additions persist their source and native GoBGP identifier before the live add;
the returned UUID is acknowledged only after the final save. A restart can
complete or withdraw an interrupted add without treating the path as absent.
Earlier versions are rejected. At an authorized cutover, stop the old helper and
retire its old journal before starting the matching new binaries; do not replace
a journal while its helper is running or discard pending effects during repair.
Complete static/dynamic policy intent and pending policy replacement are owned
by that same daemon; see [BGP ownership](../reference/bgp-ownership.md).

The separate traffic-log database requires `traffic_log_format` version 1.
Earlier databases that encoded missing counters as zero are rejected without
modifying their rows; select a fresh traffic database at cutover. A missing
per-flow or directional counter is `null`/unobserved, while a measured zero is
`0`. Client and aggregate incomplete flags mark a known partial sum as a lower
bound; the console shows `≥` for that sum. Neither a missing flow nor a failed
collection establishes zero traffic. This traffic format does not change the
main state's SQLite `user_version` 1500.

Mobility SAM, routes and addresses also retain pending acquisitions and applied
ownership. A static handover needs a new `staticHandovers[].id` shared by its
participants. Its finite release proof is published only after BGP withdrawal,
the current desired plan and all three native owners agree on that transfer.
Receipt/read/cleanup failures withhold release and remain retryable. Retain the
source during handover; use a fresh ID after rollback. See the
[Phase F procedure](https://github.com/imksoo/routerd/blob/proposal/routerd15-kiss-runtime/examples/cloudedge-mobility-demo/phase-f/README.md).

## Events table

The bus persists events into SQLite. `EventRule` and `DerivedEvent` consume this stream as input. For day-to-day operations, prefer `routerctl get events` over running `sqlite3` against the database directly:

```sh
routerctl get events --limit 20
routerctl get events --topic routerd.resource.status.changed
routerctl get events --resource DNSResolver/lan-resolver -o json
```

Even without a `LogRetention` resource, the local event journal is bounded to
24 hours, 100,000 rows, and 64 MiB of logical payload. Use a `LogSink` before
increasing local forensic retention; router operation must not depend on an
unbounded event table.

### Storage-full recovery

Check `routerctl doctor disk`, `systemctl status routerd`, and
`/run/routerd/storage-critical.json`. For a volatile Live ISO COW filesystem,
reboot the router OS and then verify its persistent configuration contains the
intended `LogRetention`. For persistent storage, stop routerd, run
`routerctl ledger prune-events --older-than 24h`, and only run
`routerctl ledger vacuum` after enough working space is available. A reboot by
itself does not reclaim persistent storage.

### Mobility holder transitions

CloudEdge SAM failover emits `routerd.mobility.holder.transition` events with
machine-readable attributes such as `transitionKind`, `address`, `timestamp`,
`issuedAt`, `fromNode`, `toNode`, `mobilityPathSig`, and
`assignmentGeneration`.

For provider-secondary-IP capture, `seize-complete` means the provider capture
assign action succeeded in the action journal for an active `/32`
`bgpCaptureAssignment`. Its `issuedAt` is the journal `ExecutedAt`, so
`timestamp - issuedAt` measures the delay from provider acceptance to event
recording. `T_seize` is the provider acceptance time.

`capture-confirmed` is still discovery-observation based. `T_confirm` is the
time the local process observed the provider capture taking effect. Together,
`seize-complete` and `capture-confirmed` measure the accepted-to-effective
interval.

After a node restart or rejoin, reconfirmation events can reuse the original
journal acceptance time as `issuedAt`. In that case, `timestamp - issuedAt`
includes the time while the node was stopped or absent. Treat those deltas as
reconfirmation age, not convergence latency.

For non-capture flows such as static-owned, static-handover, and local-home,
`seize-complete` still comes from the active-holder plus self-identity BGP
observation. Lab evidence currently proves the capture flow; static and
handover completion events are not yet proven in a real environment.

## Backup philosophy

The YAML configuration holds operator intent. The state database also holds
required ownership and unfinished operations, alongside observations and history.
Preserve that state while repairing the same running router: restoring YAML alone
cannot identify every external effect that needs cleanup. Rebuilding on a fresh
host uses rewritten configuration and an explicit inventory and retirement of
old effects, as described in the cutover procedure above.

If you want history of operational events for forensic purposes, export or take periodic snapshots of `routerd.db`, `dns-queries.db`, `traffic-flows.db`, and `firewall-logs.db`. The controller event journal currently lives in `routerd.db`; preserve the complete database when event history is required.

## See also

- [Log storage](../concepts/log-storage.md)
- [Reconcile and removal](./reconcile)

## Provider operation recovery in the 1.5 proposal

Provider actions use one process guard next to the database, `<db>.actions.lock`,
shared by the daemon and `routerctl`. Preserve this file; unlinking a live flock
file creates a second lock identity. The forward operation records `running`
before invoking the executor; undo records `rollingBack` first. Explicit executor
failures are returned to the caller. Invocation/reply failures retain
`outcomeUnknown` or `rollbackUnknown`, including restart recovery. Elapsed time
never reapproves an operation. The guard proves an invocation owner has released
or exited; it does not prove a remote provider request completed.

An unresolved operation blocks further live provider operations, including a new
idempotency key. Inspect the exact provider resource independently and establish
that no old operation remains in flight. Then record that evidence explicitly:

```sh
routerctl action resolve <id> --outcome succeeded --by <operator> \
  --evidence 'exact provider resource inspected; assignment confirmed; no request in flight'
```

Forward uncertainty accepts `succeeded` or `failed`; undo uncertainty accepts
`rolledBack` or `rollbackFailed`. A failed outcome means an inspection established
that the operation may safely be retried; ordinary desired-action import can
then requeue it. This command performs no provider mutation, cannot override a
live operation guard, and preserves prior non-secret undo facts. It does not
invent lost pre-mutation facts needed for undo. Operator identity, evidence and
the prior error remain in the journal's result message.

Journal inspection exposes typed `target`, `parameters` and `undo` values.
Encoding/decoding belongs to SQLite; malformed stored values fail the read rather
than turning into empty parameters. Unknown capture operations remain protective
Mobility evidence and cannot establish confirmed ownership. A completed undo
invalidates the preceding assignment fence.

Manual plugin publication compares the source generation after the plugin exits.
A slow invocation cannot overwrite a result published by a concurrent command;
an observation already expired at completion is rejected. Required subscription
output and successful deduplication of its complete event batch commit in one
SQLite transaction. Display history alone does not authorize publication.
