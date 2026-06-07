---
title: State and ownership
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# State and ownership

routerd separates declared intent from observed state.
YAML is the intent you manage.
SQLite, lease files, and `events.jsonl` are state that routerd and its dedicated daemons observe.

![Lifecycle GC diagram showing effective config, ownership ledger, object status, and host inventory feeding a dry-run-capable GC planner and teardown registry](/img/diagrams/lifecycle-gc.png)

## Where state lives

Release installs use `/usr/local/etc/routerd/router.yaml` for the canonical
configuration and `/usr/local/sbin` for routerd binaries.

Linux state defaults:

| Kind | Example |
| --- | --- |
| routerd state database | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD lease | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 lease | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE state | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck state | `/var/lib/routerd/healthcheck/<name>/state.json` |
| Runtime sockets | `/run/routerd/.../*.sock` |

FreeBSD uses the same configuration and binary paths under `/usr/local`.
Its runtime sockets live under `/var/run/routerd`, and persistent state lives
under `/var/db/routerd`.

## Ownership

Every host-side artifact routerd creates has an owning resource.
For example, the dnsmasq configuration is owned by the DHCP and RA resources,
the `routerd-dns-resolver` configuration by `DNSResolver` and `DNSZone`, the
nftables NAT table by `NAT44Rule`, and the aggregate TCP MSS clamp table by the
top-level `Router`.

Knowing the owner answers three questions:

- Is this artifact safe for routerd to modify?
- When the resource is removed from YAML, should routerd remove the host-side artifact too?
- Does routerd adopt an existing object, or create a new one from scratch?

The owner key is `apiVersion/kind/name`; apply generation is not part of that
identity. Resource status includes owner and lifecycle metadata so the stale
status path can distinguish routerd-managed resources from adopted or external
objects.

## Lifecycle GC

routerd keeps an ownership ledger for concrete host artifacts and stores object
status for resources that need resource-specific teardown. During apply, serve
startup, and delete flows, the generic GC planner compares those records with the
same effective config that apply uses: startup YAML after `when` filtering,
active dynamic config, and generated SAM resources.

The resulting plan can remove owned artifacts, run resource teardown, forget
ledger rows, delete stale status rows, record events, and create the state backup
required before destructive cleanup. Unsupported OS integrations are skipped
instead of being forced, and adopted or externally managed statuses are left
alone.

For the concrete resource-to-artifact map and teardown contracts, see
[Resource ownership](../resource-ownership.md).

## Don't act on stale state

Leases and observed values are useful, but acting on stale data is dangerous.
DHCPv6-PD prefixes in particular are propagated downstream only while they are confirmed `Bound`.
When that confirmation is missing, routerd suppresses the matching AAAA records, RA, DHCPv6 server, and LAN IPv6 address applications instead of advertising broken connectivity.

## Events

routerd and its daemons record state changes as events.
Events are persisted in the SQLite `events` table and in per-daemon `events.jsonl` files.
`EventRule` and `DerivedEvent` consume that stream to synthesize virtual state changes.

## Apply generations

The `generation` value in status output is the latest committed apply generation.
It is incremented when `routerd apply` changes the host-side intent store and records a completed apply in SQLite.
It is not a reconcile loop counter.
Dry-run plans, daemon events, health checks, and periodic controller runtime reconciliation do not increment it.
New apply generations store the YAML snapshot that was applied.
The Web Console uses those snapshots to show a read-only generation history and unified diffs between stored generations.
Rows created before YAML snapshot storage was introduced remain valid history, but they cannot be diffed.

## Stateful packet filters

On Linux, routerd updates managed nftables tables with a single `nft -f` transaction.
Generated rulesets create the managed table if needed, flush that table, and then load the replacement chains in the same nftables batch.
For named sets owned by routerd, such as firewall zone interface sets and
client-policy MAC sets, the generated ruleset destroys the managed set before
defining it again. That prevents removed set elements from surviving a reload.
routerd does not delete a live NAT or filter table before adding the replacement table.
Existing conntrack entries therefore remain attached to the kernel state table during routerd restarts and normal configuration changes.

On FreeBSD, routerd loads generated pf rules with `pfctl -f`.
pf keeps the existing state table across rule reloads unless states are explicitly flushed.
routerd does not flush pf states during normal apply.
