---
title: Design philosophy
slug: /concepts/design-philosophy
sidebar_position: 2
---

# Design philosophy

routerd is opinionated. It works the way it does because of a small set of
principles that turned out to matter on real router hosts. Reading these
once makes most of the day-to-day decisions in the project obvious.

## 1. The YAML is the source of truth

A router's behavior should live in a file you can review, diff, and roll
back. routerd treats the YAML as the source of truth and the host as a
projection of it.

Practical consequences:

- Operators change YAML, not generated files. Editing
  `/etc/dnsmasq.d/routerd.conf` by hand means routerd will overwrite it on
  the next apply, and the change will not survive a host rebuild.
- The history of changes lives in git. `routerd apply` does not need to
  log "what changed" because the diff already exists.
- Rollback is `git revert` plus `routerd apply`.

## 2. Prefer OS-standard daemons; add ports tools only when the base lacks the feature

routerd is not a network stack. It configures and supervises the OS's own
daemons. On each platform we use the most standard daemon that does the
job, and we accept a non-base tool only when the base option is missing or
known not to work.

Examples:

- Ubuntu: systemd-networkd is the default for interfaces and DHCPv4/DHCPv6.
  dnsmasq handles LAN DNS/DHCP. nftables for filtering and NAT.
- FreeBSD: base `dhclient` for IPv4 DHCP, base `rtsold` for IPv6 SLAAC
  router solicitation. KAME `dhcp6c` (from ports) is used **only** for
  IPv6 prefix delegation, because the FreeBSD base does not ship a working
  PD client.
- NixOS: same Linux daemons, but installed and configured through the Nix
  module system rather than raw `/etc` files.

This keeps the host inspectable with the operator's existing knowledge of
the OS. It also means we trust each OS to know how to renew leases,
restart daemons, and survive crashes; routerd does not reimplement those.

## 3. kubectl-style verbs

routerd resources look like Kubernetes resources, and the CLI uses the same
verbs:

- `apply` brings the host into the shape of the YAML.
- `validate` checks the YAML against the schema.
- `render` shows what files would be written for the current YAML.
- `get`, `describe`, `show` inspect resources and observed state at three
  granularities (one-line summary, structured summary, full data).

This is not aesthetics. The verbs match a control loop that already works
at scale, and the [resource ownership](../reference/resource-ownership)
model is built on the same idea (managed objects, owner references,
finalizers conceptually).

## 4. Observation-first

routerd records what it observed on the host alongside what was declared.
DHCPv6 prefix delegations, host inventory (kernel, virtualization, service
manager, available commands), and the artifacts routerd installed are
stored in a local SQLite database in addition to the YAML.

This lets you answer "what is the router actually doing right now?"
without grepping through several daemons' logs. It also lets future
versions of routerd branch on observed facts (physical vs virtual host,
systemd vs rc.d) instead of guessing.

## 5. Small, inspectable, no remote control plane

routerd is not a platform. It is a small control loop with a typed YAML
schema and a SQLite state file. Adding a new resource kind is a deliberate
decision, not a feature gap to fill. There is no centralized API server,
no agent fleet, no per-tenant abstraction.

You can read the daemon's behavior end-to-end with `routerctl` and a few
SQL queries. Operators of small networks should be able to fully
understand the tool that is running their router.

## 6. Idempotent by default

Re-applying the same YAML against an already-converged host should change
nothing. The renderer is deterministic. The reconcile loop diffs observed
state against desired state and only acts when there is a real
difference. This is the property that lets routerd run as a long-lived
daemon on the apply loop without thrashing the host.

## How these connect

These principles compound:

- "YAML is the source of truth" + "kubectl-style verbs" gives a workflow
  operators already know.
- "OS-standard daemons" + "observation-first" means the host can be
  debugged with normal tools, and routerd's state explains what it
  expected.
- "Small + inspectable" + "idempotent" means you can leave the daemon
  running and trust it.

When choosing between two implementation paths in routerd, the principle
that wins is whichever makes the next operator faster at understanding
the running router from the YAML alone.
