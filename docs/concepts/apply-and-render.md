---
title: Apply and render
slug: /concepts/apply-and-render
sidebar_position: 4
---

# Apply and render

routerd has six core verbs. They cover the everyday workflow: edit YAML,
preview the change, push it to the host, inspect what happened. This page
explains each one and how they connect.

## At a glance

| Verb | Tool | Purpose |
|---|---|---|
| `validate` | `routerd validate` | Schema-check the YAML without touching the host. |
| `render` | `routerd render` | Show the files routerd would write for this YAML. |
| `apply` | `routerd apply` | Bring the host into the shape of the YAML. |
| `get` | `routerctl get` | One-line per resource summary (kubectl-style). |
| `describe` | `routerctl describe` | Multi-section structured summary of one resource. |
| `show` | `routerctl show` | Full data of a resource in YAML/JSON. |

`validate`, `render`, and `apply` operate on a YAML file. `get`,
`describe`, and `show` operate on the running router (via the SQLite
state and the local control socket).

## `validate`

`routerd validate --config router.yaml` parses the YAML, checks each
resource against the schema, and reports problems. It does not look at
the host at all. Run this in CI or before every apply.

```bash
routerd validate --config router.yaml
```

If `validate` is happy, the YAML is structurally correct. It does not
guarantee the configuration matches reality (e.g. that the interface name
exists on this host) — `apply --dry-run` does that.

## `render`

`routerd render` emits the files routerd would write for a given YAML, on
a given platform target.

```bash
routerd render linux --config router.yaml
routerd render freebsd --config router.yaml
```

The output includes `systemd-networkd` drop-ins, `dnsmasq.conf`,
`nftables.conf`, the FreeBSD `rc.conf` fragment, and so on. Use `render`
to read what would change, especially during development of a new
resource.

`render` is read-only. It does not need to talk to the host.

## `apply`

`routerd apply` is the verb that changes the host. It:

1. Reads the YAML.
2. Reads the current host state and routerd's local state database.
3. Plans the changes (or dry-runs if `--dry-run` is set).
4. Writes generated files, restarts services as needed, and updates the
   state database.
5. Records events for any warnings.

```bash
sudo routerd apply --once --config /usr/local/etc/routerd/router.yaml
```

`--once` runs a single apply pass. Without `--once`, `routerd serve` runs
as a daemon and re-applies on a periodic schedule. Both paths share the
same code; `serve` is just `apply` in a loop.

For early validation, run `apply --dry-run`:

```bash
sudo routerd apply --once --dry-run --config /usr/local/etc/routerd/router.yaml
```

This produces the change plan without writing files or restarting
services.

## `get`, `describe`, `show`

The three inspection verbs follow kubectl conventions:

- `routerctl get` — one line per resource, suitable for piping or
  scanning at a glance.
- `routerctl describe` — multi-section human-readable summary of one
  resource, including observed status and recent events.
- `routerctl show` — the full structured data of one resource as YAML or
  JSON, suitable for scripting.

Examples:

```bash
routerctl get
routerctl describe interface/wan
routerctl describe ipv6pd/wan-pd
routerctl show ipv6pd/wan-pd -o yaml
```

The same three verbs work for `Inventory` (`routerctl describe inventory/host`),
`Router`, and every other kind.

## Where the work happens

```
              YAML
                │
        ┌───────┴───────┐
        │   validate    │  schema check (no host)
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │    render     │  preview generated files (no host)
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │     apply     │  write files, restart services
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │ host + state  │  the live router + SQLite state
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │ get / describe│
        │ / show        │  inspect what's there
        └───────────────┘
```

`apply` is the only step that changes the host. Everything else is
read-only.

## Where to go next

- [State and ownership](./state-and-ownership) — what `apply` records.
- [Install](../tutorials/install) — first apply.
- [Resource ownership](../reference/resource-ownership) — what `apply`
  promises and the rules for cleanup.
