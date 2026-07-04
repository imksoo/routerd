---
title: State database
slug: /operations/state-database
---

# State database

![Diagram showing routerd state database paths, daemon lease and event files, routerctl event access, and backup philosophy where YAML remains authoritative and event databases provide forensic history](/img/diagrams/operations-state-database.png)

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

## Events table

The bus persists events into SQLite. `EventRule` and `DerivedEvent` consume this stream as input. For day-to-day operations, prefer `routerctl events` over running `sqlite3` against the database directly:

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

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

The state database holds **observed** state — it is not a substitute for the configuration. The authoritative description of intent lives in the YAML configuration, version-controlled in git. If a host is rebuilt, restoring the configuration and letting routerd reconcile is preferred over restoring the SQLite database.

If you want history of operational events for forensic purposes, take periodic snapshots of `events.db`, `dns-queries.db`, `traffic-flows.db`, and `firewall-logs.db` instead. Those are append-only by nature and do not need point-in-time backups of `routerd.db`.

## See also

- [Log storage](../concepts/log-storage.md)
- [Reconcile and removal](./reconcile)
