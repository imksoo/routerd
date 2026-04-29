---
title: State and ownership
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# State and ownership

routerd separates two kinds of memory: what you declared (in YAML, in
git) and what was observed and produced on the host (in a local SQLite
database). This separation is what makes the daemon safe to leave
running.

## The state database

routerd keeps its state in a single SQLite file. The default path is:

- Linux: `/var/lib/routerd/routerd.db`
- FreeBSD: `/var/db/routerd/routerd.db`

The schema is k8s-style: a small set of tables, each holding typed JSON.

| Table | Purpose |
|---|---|
| `generations` | One row per `apply` pass: timestamp, config hash, warnings, and outcome. |
| `objects` | One row per resource: latest observed status as JSON. |
| `artifacts` | The ownership ledger: which host-side files and units routerd installed and on whose behalf. |
| `events` | Notable observations during apply (warnings, prefix changes, lease lost, ...). |
| `access_logs` | Reserved for future local control-API auditing. |

A typical query:

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.currentPrefix')
   from objects
   where kind = 'IPv6PrefixDelegation' and name = 'wan-pd';"
```

`sqlite3` is not required at runtime; the daemon embeds its driver. The
CLI is a convenience for operators.

## What goes in `objects`

For each resource named in the YAML (and a few observation-only kinds
like `Inventory`), routerd stores a `status` blob. The shape of `status`
depends on the kind. For example, an `IPv6PrefixDelegation`'s status
includes:

- `currentPrefix` — the delegated prefix routerd last observed.
- `lastPrefix` — the previous value (kept across apply cycles).
- `lastObservedAt` — when the prefix was last seen.
- `duid`, `duidText`, `iaid`, `expectedDUID` — DHCPv6 client identity.
- `validLifetime` — the lease lifetime if known.

`spec` is never copied into `status`. The YAML is always the source of
truth for desired configuration.

## What goes in `artifacts`

`artifacts` is the ownership ledger. Every host-side object routerd
creates (a `systemd-networkd` drop-in, a `dnsmasq.conf`, a kernel route)
gets a row that records:

- which routerd resource owns it,
- what kind of host artifact it is (file, sysctl, route, service unit),
- enough identity to find it again (path, key, etc.).

When you remove a resource from the YAML, the next apply walks the
ledger, finds artifacts whose owner is no longer in the desired set, and
removes them. This is how routerd avoids leaving orphan dnsmasq drop-ins
or stale routes when the YAML is edited.

The ledger is **persistent**, not a cache. Rebuilding it from the host
alone is unsafe: routerd would not be able to tell its own files from
files an operator created by hand.

## What goes in `events`

`events` captures notable transitions during apply. Examples:

- `Normal InventoryObserved` — host inventory changed.
- `Warning ApplyWarning` — something didn't reach the desired state.
- `Normal PrefixObserved` — DHCPv6-PD prefix observed for the first time.

You can read events through `routerctl describe` (recent events appear
inline at the bottom of each describe output).

## Why this matters

Splitting "what you wanted" (YAML) from "what is" (objects) and "what
routerd installed" (artifacts) is what makes apply both safe and
reversible:

- Removing a resource → apply finds the artifact in the ledger and
  removes the host-side object.
- Resource still wanted → apply diffs the observed status against the
  spec and patches what differs.
- Resource not in the ledger → routerd does not own it and leaves it
  alone. Pre-existing files are not silently destroyed.

This is also why routerd is comfortable being a long-running daemon: it
re-reads observed state on each pass and converges only when there is a
real diff.

## Where to go next

- [Resource ownership](../reference/resource-ownership) — the formal
  rules of the ledger.
- [Operations: state database](../operations/state-database) — practical
  queries and inspection.
- [Apply and render](./apply-and-render) — the verbs that read and write
  this state.
