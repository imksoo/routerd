---
title: State database
slug: /operations/state-database
---

# State database

routerd keeps its observed state and ownership ledger in a single SQLite
file. This page covers practical queries and where the file lives.

## Path

| OS | Default |
|---|---|
| Linux | `/var/lib/routerd/routerd.db` |
| FreeBSD | `/var/db/routerd/routerd.db` |

If you set `--state-dir` on `routerd apply` or `routerd serve`, the
database lives under that directory.

## Tables

| Table | Purpose |
|---|---|
| `generations` | One row per apply pass. Columns: `id`, `created_at`, `config_hash`, `outcome`, JSON of warnings. |
| `objects` | One row per resource. Columns: `api_version`, `kind`, `name`, `status` JSON. |
| `artifacts` | The ownership ledger. Each host-side artifact owned by routerd. |
| `events` | Notable observations during apply. |
| `access_logs` | Reserved for future control-API audit. |

The conceptual mapping is k8s-like: `objects` ≈ Kubernetes objects with
`status`, `artifacts` ≈ owner references, `events` ≈ Kubernetes events.

## Common queries

### What prefix does PD think it has?

```bash
sqlite3 /var/lib/routerd/routerd.db <<'SQL'
select json_extract(status, '$.currentPrefix') as current,
       json_extract(status, '$.lastPrefix') as last,
       json_extract(status, '$.lastObservedAt') as observed_at
from objects
where kind = 'IPv6PrefixDelegation';
SQL
```

### Recent events

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select created_at, type, reason, message
   from events
   order by id desc limit 30;"
```

### What does routerd own?

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select kind, name, owner_kind, owner_name
   from artifacts
   order by kind, name;"
```

### Last apply pass

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select id, created_at, config_hash, outcome
   from generations
   order by id desc limit 5;"
```

## Inspecting from `routerctl`

For most day-to-day questions, prefer `routerctl`:

```bash
routerctl get
routerctl describe ipv6pd/wan-pd
routerctl show inventory/host -o yaml
```

These talk to the daemon's local socket and read the same database, but
through a typed API. Use the SQLite shell when you need ad-hoc cross-row
queries that the CLI does not expose.

## Backup and migration

The database is a normal SQLite file. To back it up while routerd is
running, use:

```bash
sqlite3 /var/lib/routerd/routerd.db ".backup /tmp/routerd-backup.db"
```

To migrate a router host, copy the database alongside the YAML. routerd
detects schema drift on next start and migrates in place when the new
binary is shipped with a forward migration.

## When to NOT touch the database

- During an active `apply` (you'll fight the writer).
- To "fix" what looks like a stale prefix or stale ownership row. The
  next apply will re-read the host and reconcile. Manual editing of the
  ledger can leave routerd unable to find what it owns.

If you really need to reset state, stop the daemon, move the file
aside, and let the next `apply` rebuild from the YAML. Routerd will not
clean up host-side artifacts whose ownership rows were lost — be ready
to remove them by hand if you go this route.
