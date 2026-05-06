---
title: State database
slug: /operations/state-database
---

# State database

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

## Backup philosophy

The state database holds **observed** state — it is not a substitute for the configuration. The authoritative description of intent lives in the YAML configuration, version-controlled in git. If a host is rebuilt, restoring the configuration and letting routerd reconcile is preferred over restoring the SQLite database.

If you want history of operational events for forensic purposes, take periodic snapshots of `events.db`, `dns-queries.db`, `traffic-flows.db`, and `firewall-logs.db` instead. Those are append-only by nature and do not need point-in-time backups of `routerd.db`.

## See also

- [Log storage](../concepts/log-storage.md)
- [Reconcile and removal](./reconcile.md)
