---
title: Resource Ownership
slug: /reference/resource-ownership
---

# Resource Ownership and Reconcile

routerd reconciles declarative resources into host artifacts such as Linux links,
addresses, routing rules, routing tables, nftables tables, systemd units, and
managed config files.

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
- IPv4 fwmark rules use the common orphan detection path, with cleanup limited
  to the explicit routerd fwmark range.

The next step is to move each apply path from command-specific imperative logic
to artifact-specific reconcilers. That keeps cleanup behavior uniform as more
resources are added.

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

The long-term rule is conservative: routerd should delete broad artifact types
only when the local ledger proves ownership. Name and number ranges are useful
guardrails, but they are not enough for destructive cleanup of files, services,
nftables tables, or general route tables.
