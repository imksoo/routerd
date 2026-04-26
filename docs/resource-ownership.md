---
title: Resource Ownership
slug: /reference/resource-ownership
---

# Resource Ownership and Reconcile

routerd reconciles declarative resources into host artifacts such as Linux links,
addresses, routing rules, routing tables, nftables tables, systemd units, and
managed config files.

This page is operationally important. routerd is allowed to change a router's
kernel networking state, so it must have a clear answer to three questions:

1. Which desired resource owns this host artifact?
2. Did routerd create or explicitly adopt it?
3. If the resource disappears from the config, is it safe to remove the host
   artifact?

The local ownership ledger is the durable answer to the second question. It is
not just a cache. It is part of the safety model for destructive cleanup.

The reconcile model is:

1. Collect actual host inventory.
2. Let each routerd resource declare artifact intents.
3. Compare desired artifacts with actual artifacts.
4. Keep matching artifacts.
5. Create or update missing or drifted artifacts.
6. Delete orphaned artifacts that are known to be routerd-managed.

This follows the same broad ownership idea used by Kubernetes controllers:
objects declare ownership of generated objects, finalizers handle cleanup that
must happen before deletion, and field ownership records who manages a field.
routerd does not have an API server, so it keeps the ownership model in the
reconciler and in a local ledger.

## Artifact Intents

Each resource emits one or more artifact intents:

- `kind`: stable artifact type, such as `linux.ipv4.fwmarkRule` or `nft.table`
- `name`: stable artifact identity within that kind
- `owner`: routerd resource ID
- `action`: `ensure`, `delete`, or `observe`
- `applyWith`: module or command family that can correct drift

An intent is the bridge between YAML and the host. Resource-specific code should
not silently create host state without declaring an artifact intent for it. If a
new resource kind or a new host-side object is added, the artifact intent list
and the resource ownership documentation must be updated together.

`action` matters:

- `ensure`: routerd wants this artifact to exist and may manage it.
- `observe`: routerd uses this artifact for discovery or aliasing but does not
  own it for cleanup.
- `delete`: reserved for explicit deletion flows.

Only inventory-backed `ensure` artifacts are remembered in the ledger today.
This avoids recording an object that routerd cannot later match to a stable host
identity.

For example, an `IPv4DefaultRoutePolicy` candidate can own:

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_default_route`

An `IPv4PolicyRouteSet` can own:

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_policy`

## Orphans

An artifact is orphaned when:

- it is in a routerd-managed namespace or range, and
- it exists on the host, and
- no current routerd resource emits an intent for it.

There are two orphan signals:

- **Namespace/range signal**: the artifact is inside a routerd-owned namespace,
  such as fwmark `0x100-0x1ff` or an nftables table named `routerd_*`.
- **Ledger signal**: the artifact was previously recorded in
  `/var/lib/routerd/artifacts.json` as owned by a routerd resource.

The ledger signal is stronger. A namespace or numeric range is a useful
guardrail, but by itself it is not enough for broad destructive cleanup. For
example, a table named `routerd_foo` is probably ours, but a ledger entry proves
that this routerd installation accepted ownership of that concrete artifact.

For Linux policy routing, routerd currently treats fwmarks `0x100-0x1ff` as a
managed range. If a stale rule from an old DS-Lite route set remains there,
reconcile removes the stale `ip rule` and flushes the referenced routing table
when that table is no longer desired by any current resource.

Artifacts outside routerd-managed namespaces are treated as external unless the
local ledger proves that routerd created them. Destructive orphan cleanup should
prefer ledger ownership over heuristics. A namespace or range such as fwmark
`0x100-0x1ff` can be used as an additional safety boundary, but it must not be
the only long-term ownership signal for broad artifact types such as nftables
tables, files, services, or route tables.

## Current Status

The artifact foundation is now in place:

- `pkg/resource` defines artifacts, intents, and orphan detection.
- `pkg/resource` defines a local ownership ledger.
- `pkg/reconcile` declares artifact intents for all current resource kinds.
- `routerd plan` includes per-resource artifact intents.
- `routerd adopt --candidates` reports existing desired artifacts that
  are not yet recorded in the local ownership ledger. Candidate inventory
  currently covers policy routing, nftables tables, selected systemd services,
  managed files, sysctl keys, hostname, links, addresses, and IP-in-IPv6
  tunnels.
- `routerd adopt --apply` records matching adoption candidates in the local
  ledger without changing host state. It refuses candidates whose observed
  attributes differ from desired state.
- Successful `routerd reconcile --once` records owned, inventory-backed
  artifacts in the local ledger.
- `routerd reconcile --once` removes ledger-owned orphaned `linux.ipip6.tunnel`,
  `nft.table`, and `systemd.service` artifacts when they are no longer desired.
- IPv4 fwmark rules use the common orphan detection path, with cleanup limited
  to the explicit routerd fwmark range.

The next step is to move each apply path from command-specific imperative logic
to artifact-specific reconcilers. That keeps cleanup behavior uniform as more
resources are added.

## Desired and Observed State

`desired` is the state derived from the router YAML. `observed` is the state read
from the host. Adoption uses both.

If an artifact exists on the host but is not in the ledger, `adopt --candidates`
reports it. If `desired` and `observed` attributes differ, adoption is not a
plain ownership transfer. The operator must first choose one of these actions:

- apply reconcile so the host matches the YAML,
- change the YAML so desired matches the host, or
- leave it unmanaged.

`adopt --apply` refuses drifted candidates because recording ownership of a
known-different artifact would hide a real configuration decision.

Example:

```json
{
  "kind": "linux.hostname",
  "name": "system",
  "desired": {"hostname": "router03.example.net"},
  "observed": {"hostname": "router03"}
}
```

This means the host object exists, but it is not already in the desired state.
It should be reconciled or the config should be changed before adoption.

## Resource Coverage

Every current resource kind declares the host artifacts it intends to observe or
manage. Unit tests fail when a known resource kind does not emit at least one
artifact intent.

| Resource | Host artifacts |
| --- | --- |
| `LogSink` | routerd log sink |
| `Sysctl` | Linux sysctl key |
| `NTPClient` | systemd-timesyncd config |
| `Interface` | Linux link |
| `PPPoEInterface` | PPP interface, routerd PPPoE systemd unit, PPP secret files |
| `IPv4StaticAddress` | Linux IPv4 address |
| `IPv4DHCPAddress` | DHCPv4 client binding |
| `IPv4DHCPServer` | dnsmasq config and service |
| `IPv4DHCPScope` | dnsmasq DHCPv4 scope |
| `IPv6DHCPAddress` | DHCPv6 client binding |
| `IPv6PrefixDelegation` | DHCPv6 prefix delegation binding |
| `IPv6DelegatedAddress` | Linux IPv6 address |
| `IPv6DHCPServer` | dnsmasq config and service |
| `IPv6DHCPScope` | dnsmasq DHCPv6 scope |
| `SelfAddressPolicy` | routerd address-selection policy |
| `DNSConditionalForwarder` | dnsmasq conditional forwarding config |
| `DSLiteTunnel` | Linux IP-in-IPv6 tunnel |
| `HealthCheck` | routerd scheduler health check |
| `IPv4DefaultRoutePolicy` | nftables mark table, IPv4 route tables, IPv4 fwmark rules |
| `IPv4SourceNAT` | nftables NAT table |
| `IPv4PolicyRoute` | IPv4 route table and fwmark rule |
| `IPv4PolicyRouteSet` | nftables policy table, IPv4 route tables, IPv4 fwmark rules |
| `IPv4ReversePathFilter` | Linux rp_filter sysctl key |
| `PathMTUPolicy` | nftables MSS table, dnsmasq RA MTU option |
| `Zone` | routerd firewall zone |
| `FirewallPolicy` | nftables filter table |
| `ExposeService` | nftables DNAT table |
| `Hostname` | system hostname |

## Adoption Workflow

Use adoption before letting routerd clean up broad host resources that may have
been created by an earlier routerd build or by hand:

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --candidates
```

This command is read-only. It reports desired artifacts that already exist on
the host but are not recorded in `/var/lib/routerd/artifacts.json`.

If the candidates look correct and no candidate reports differing observed
attributes, record them in the local ledger:

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --apply
```

`adopt --apply` does not change kernel, nftables, systemd, or file state. It
only writes the ownership ledger. If a candidate differs from desired state,
run reconcile or change the config first, then re-run adoption.

After a successful non-dry-run reconcile, routerd also remembers the owned
artifacts it knows how to inventory. Derived artifacts that cannot yet be
matched to a stable host identity are intentionally left out of the ledger.

A typical first-time workflow on an already configured router is:

1. Run `routerd plan` and inspect drift.
2. Run `routerd adopt --candidates`.
3. Fix drift by reconciling or editing YAML.
4. Run `routerd adopt --apply` to record existing matching artifacts.
5. Run `routerd reconcile --once --dry-run` and confirm no unexpected orphans.
6. Run `routerd reconcile --once`.

For a fresh router installed only through routerd, step 4 is often unnecessary
because successful reconcile records owned artifacts automatically.

## Cleanup Policy

routerd currently performs destructive cleanup only for artifact kinds where the
delete operation is narrow and ownership can be proven:

- `linux.ipv4.fwmarkRule` in the explicit routerd fwmark range
- `linux.ipip6.tunnel` when recorded in the local ledger
- `nft.table` when recorded in the local ledger and named `routerd_*`
- `systemd.service` when recorded in the local ledger and named `routerd-*.service`

Cleanup details:

- `linux.ipip6.tunnel`: deleted with `ip -6 tunnel del <name>`.
- `nft.table`: deleted with `nft delete table <family> <name>` only for
  ledger-owned `routerd_*` tables.
- `systemd.service`: disabled and stopped with `systemctl disable --now`, then
  the matching `/etc/systemd/system/routerd-*.service` unit file is removed and
  systemd is reloaded.

Explicit non-cleanup cases:

- `linux.link`: routerd does not delete links as orphan cleanup. Physical NICs,
  hypervisor NICs, VLANs, bridges, and other software links may have ownership
  outside routerd.
- `file`: routerd does not delete whole managed files as orphan cleanup. Only
  routerd-owned blocks inside a file may be safe to touch.
- `linux.ipv4.address` / `linux.ipv6.address`: address cleanup is intentionally
  separate. Stale addresses can block moving an address to another interface,
  but deleting the wrong address can break management connectivity.
- `linux.sysctl` and `linux.hostname`: these are global host state, not
  standalone objects that can be safely removed. They can be reconciled to a
  desired value, but orphan cleanup does not delete them.

The long-term rule is conservative: routerd should delete broad artifact types
only when the local ledger proves ownership. Name and number ranges are useful
guardrails, but they are not enough for destructive cleanup of files, services,
nftables tables, or general route tables.

## Implementation Rules

When adding or changing a resource kind:

1. Declare every host artifact the resource creates or relies on.
2. Decide whether each artifact is `ensure`, `observe`, or explicit `delete`.
3. Add actual inventory support before recording it in the ledger.
4. Add cleanup only when deletion is narrow, reversible enough, and ownership is
   proven by the ledger or by a very explicit routerd namespace.
5. Document cleanup behavior and non-cleanup behavior.
6. Add tests that fail if the resource kind emits no artifact intent.

This prevents reconcile from turning into unrelated one-off cleanup heuristics.
The goal is that every host-side object has a declared owner, a known inventory
method, and a deliberately chosen cleanup policy.
