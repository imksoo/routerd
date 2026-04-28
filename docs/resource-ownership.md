---
title: Resource Ownership
slug: /reference/resource-ownership
---

# Resource Ownership and Apply Model

routerd is allowed to change a router's kernel networking state, dnsmasq
config, nftables tables, systemd units, and other host-side configuration.
Before doing that, it has to answer three questions for every host artifact
it touches:

1. Which routerd resource owns this artifact?
2. Did routerd create or explicitly adopt it?
3. If the resource disappears from the YAML, is it safe to delete the
   artifact from the host?

This page describes how routerd answers those questions. The local ownership
ledger is central to question 2, and is part of the safety model — it is
not just a cache.

## How apply works

Each apply pass:

1. Reads the current host inventory.
2. Lets each routerd resource emit one or more *artifact intents* — short
   declarations of "I expect this artifact to exist" or "I observe this
   artifact".
3. Compares desired artifacts against the actual host artifacts.
4. Keeps anything that already matches.
5. Creates or updates anything that is missing or drifted.
6. Optionally removes orphan artifacts that are known to be routerd-managed.

This mirrors the broad ownership idea Kubernetes controllers use: generated
objects carry an owner reference, finalizers handle pre-deletion cleanup,
and field ownership records who manages a field. routerd has no API server,
so the ownership model lives in the applier and in a JSON file on disk.

## Apply Failure Model

routerd deliberately avoids promising a single all-or-nothing transaction for
every host operation. Linux routing tables, DHCP leases, nftables state,
systemd units, and FreeBSD rc.conf files do not share one common transaction
manager. Pretending otherwise would make failure handling harder to reason
about.

Instead, routerd uses three plain concepts:

- **Current apply**: the work attempted by one apply pass.
- **Ownership ledger**: the local record of artifacts routerd has created or
  adopted.
- **Protected management path**: interfaces or firewall zones that must remain
  usable for SSH or the local control API.

The top-level `spec.reconcile` policy chooses how strict an apply should be:

```yaml
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
    protectedZones:
      - mgmt
```

`mode: strict` stops on the first apply error. `mode: progressive` keeps
independent stages moving when it can, records failed stages as warnings, and
marks the result as `Degraded`. If any stage fails, routerd skips destructive
orphan cleanup and skips recording new ownership in the ledger. This prevents
a partial apply from being mistaken for a clean committed generation.

Protected interfaces and zones are safety anchors. They do not mean "never
touch this interface". They mean routerd must not casually remove or block
the path the operator uses to repair the router. Firewall rendering always
keeps SSH open from protected zones. Future cleanup and rollback code must
treat protected artifacts as preserve-first.

For operators, the rule is simple: keep management access, apply safe
independent changes, and leave failed data-plane work visible for the next
plan or apply.

## Artifact intents

An intent is the bridge between a YAML resource and the host:

- `kind`: a stable artifact type, for example `linux.ipv4.fwmarkRule` or
  `nft.table`.
- `name`: a stable identity within that kind.
- `owner`: the routerd resource ID that emitted the intent.
- `action`: `ensure`, `delete`, or `observe`.
- `applyWith`: the renderer or command family that can correct drift.

Resource code is not allowed to silently create host state without
emitting a matching intent. When a new resource kind starts managing a new
host-side object, the intent list and this document must be updated
together.

`action` values:

- `ensure`: routerd wants this artifact to exist and may manage it.
- `observe`: routerd reads the artifact for discovery or alias resolution
  but does not own it for cleanup.
- `delete`: reserved for explicit deletion flows.

Only inventory-backed `ensure` artifacts are recorded in the ledger. This
avoids registering an artifact that routerd cannot later match to a stable
host identity.

For example, an `IPv4DefaultRoutePolicy` candidate may own:

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_default_route`

An `IPv4PolicyRouteSet` may own:

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_policy`

## Orphans

An artifact is considered orphaned when:

- it lives inside a routerd-managed namespace or numeric range, and
- it is present on the host, and
- no current routerd resource emits an intent for it.

There are two orphan signals:

- **Namespace / range signal**: the artifact is inside a routerd-owned
  namespace, such as fwmark `0x100-0x1ff` or an nftables table named
  `routerd_*`.
- **Ledger signal**: the artifact was previously recorded in
  the `artifacts` table in `/var/lib/routerd/routerd.db` as owned by a routerd resource.

The ledger signal is the stronger one. A namespace or numeric range is a
useful guardrail, but on its own it is not enough for broad destructive
cleanup. A table named `routerd_foo` is probably ours, but a ledger entry
proves that *this* routerd installation accepted ownership of that
artifact.

For Linux policy routing, routerd currently treats fwmarks `0x100-0x1ff` as
a managed range. A stale rule from a removed DS-Lite route set in that
range is removed by apply; if the routing table it pointed at is no
longer desired, that table is flushed too.

Artifacts outside routerd-managed namespaces are treated as external until
the ledger proves routerd owns them. The long-term rule is that destructive
cleanup of broad artifact types — files, services, nftables tables, route
tables — must be backed by a ledger entry, not by a name match alone.

## Desired and observed state

`desired` is the state derived from the YAML config. `observed` is the
state read from the host. Adoption uses both.

If an artifact exists on the host but is not in the ledger,
`routerd adopt --candidates` reports it. When desired and observed
attributes differ, adoption is no longer a plain ownership transfer — the
operator has to make a decision first:

- run apply so the host matches the YAML,
- change the YAML so desired matches the host, or
- leave the artifact unmanaged.

`adopt --apply` refuses drifted candidates because recording ownership of a
known-different artifact would silently hide a real configuration choice.

Example of a drifted candidate:

```json
{
  "kind": "host.hostname",
  "name": "system",
  "desired": {"hostname": "router03.example.net"},
  "observed": {"hostname": "router03"}
}
```

The host artifact exists, but it is not yet in the desired state. Resolve
the drift before recording ownership.

## Resource coverage

Every current resource kind declares the host artifacts it intends to
observe or manage. Unit tests fail when a known kind emits no artifact
intent.

| Resource | Host artifacts |
| --- | --- |
| `LogSink` | routerd log sink |
| `Sysctl` | host sysctl key |
| `NTPClient` | systemd-timesyncd config |
| `Interface` | network link |
| `PPPoEInterface` | PPP interface, Linux PPPoE systemd unit and PPP secret files, or FreeBSD mpd5 config and `mpd5` service |
| `IPv4StaticAddress` | IPv4 address |
| `IPv4DHCPAddress` | DHCPv4 client binding and renderer-specific route/DNS adoption settings |
| `IPv4DHCPServer` | dnsmasq config and service |
| `IPv4DHCPScope` | dnsmasq DHCPv4 scope |
| `IPv6DHCPAddress` | DHCPv6 client binding |
| `IPv6PrefixDelegation` | DHCPv6 prefix delegation binding; FreeBSD KAME `dhcp6c` DUID file for NTT link-layer DUID profiles |
| `IPv6DelegatedAddress` | IPv6 address |
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

## Adoption workflow

Adoption is the controlled handover that turns "this artifact already
exists on the host" into "routerd owns it". Use it before letting routerd
clean up broad host resources that may have been created by an earlier
routerd build or by hand.

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --candidates
```

The candidate command is read-only. It reports desired artifacts that
already exist on the host but are not yet recorded in
the `artifacts` table in `/var/lib/routerd/routerd.db`.

If the candidates look correct and none reports differing observed
attributes, record them in the ledger:

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --apply
```

`adopt --apply` does not change kernel, nftables, systemd, or file state.
It only writes the ownership ledger. If a candidate differs from desired
state, run apply or change the config first, then re-run adoption.

After a successful non-dry-run apply, routerd also remembers the owned
artifacts it can inventory. Derived artifacts that cannot yet be matched to
a stable host identity are intentionally left out of the ledger.

A typical first-time workflow on an already configured router:

1. Run `routerd plan` and inspect drift.
2. Run `routerd adopt --candidates`.
3. Resolve drift by applying changes or editing the YAML.
4. Run `routerd adopt --apply` to record matching artifacts.
5. Run `routerd apply --once --dry-run` and confirm there are no
   unexpected orphans.
6. Run `routerd apply --once`.

For a fresh router built only through routerd, step 4 is often
unnecessary, because successful apply records owned artifacts
automatically.

## Cleanup policy

routerd performs destructive cleanup only for artifact kinds where the
delete operation is narrow and ownership can be proven:

- `linux.ipv4.fwmarkRule` inside the explicit routerd fwmark range.
- `linux.ipip6.tunnel` recorded in the local ledger.
- `nft.table` recorded in the local ledger and named `routerd_*`.
- `systemd.service` recorded in the local ledger and named
  `routerd-*.service`.

Cleanup details:

- `linux.ipip6.tunnel`: deleted with `ip -6 tunnel del <name>`.
- `nft.table`: deleted with `nft delete table <family> <name>`, only for
  ledger-owned `routerd_*` tables.
- `systemd.service`: stopped and disabled with `systemctl disable --now`,
  the corresponding `/etc/systemd/system/routerd-*.service` unit file is
  removed, and systemd is reloaded.

Explicitly *not* cleaned up as orphans:

- `net.link`: physical NICs, hypervisor NICs, VLANs, bridges, and other
  software links may have ownership outside routerd. Routerd does not
  delete links.
- `file`: routerd does not delete whole managed files as orphan cleanup.
  Only routerd-owned blocks inside a file may be safe to touch.
- `net.ipv4.address` / `net.ipv6.address`: address cleanup is left
  separate. Stale addresses can block moving an address to another
  interface, but deleting the wrong one breaks management connectivity.
- `host.sysctl` and `host.hostname`: these are global host state, not
  standalone objects that can be safely removed. Apply can drive them
  to a desired value, but orphan cleanup will not delete them.

The long-term rule is conservative: routerd deletes broad artifact types
only when the local ledger proves ownership. Name and number ranges are
useful as additional guardrails, never as the sole long-term ownership
signal.

## Implementation rules

When adding or changing a resource kind:

1. Declare every host artifact the resource creates or relies on.
2. Decide whether each artifact is `ensure`, `observe`, or explicit
   `delete`.
3. Add real inventory support before recording the artifact in the ledger.
4. Add cleanup only when deletion is narrow, recoverable enough, and
   ownership is proven by the ledger or by a very explicit routerd
   namespace.
5. Document both the cleanup behavior and the deliberate non-cleanup
   behavior.
6. Add tests that fail if the resource kind emits no artifact intent.

These rules keep apply from turning into unrelated one-off cleanup
heuristics. The goal is that every host-side object has a declared owner,
a known inventory method, and a deliberate cleanup policy.
