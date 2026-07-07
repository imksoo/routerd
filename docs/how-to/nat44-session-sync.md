---
title: NAT44 session sync for HA routers
slug: /how-to/nat44-session-sync
---

# NAT44 session sync for HA routers

![Diagram showing NAT44SessionSync dumping selected conntrack SNAT entries from the active router, restoring them over SSH, and surfacing insert failures in standby status](/img/diagrams/how-to-nat44-session-sync.png)

Use `NAT44SessionSync` when two routerd nodes share a LAN gateway role and the
active node should keep selected NAT44 conntrack sessions warm on a standby
node. The default `snapshot` mode periodically dumps the local conntrack table
for selected SNAT addresses and restores matching entries on each target.
`event-stream` mode starts with the same snapshot safety net, then keeps a
local conntrack event reader running and sends incremental batches.

Gate the resource with `spec.when` so only the active node exports sessions.
For VRRP-based failover, the usual gate is the local `VirtualAddress` role.

## Sync selected NAT rules

Reference the NAT rules whose SNAT addresses should be mirrored. Dynamic SNAT
addresses are read from `NAT44Rule` status, so run the NAT44 controller before
expecting session sync to become active.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: dslite-abc-sessions
  spec:
    mode: snapshot
    interval: 2s
    natRules:
      - NAT44Rule/lan-to-dslite-a
      - NAT44Rule/lan-to-dslite-b
      - NAT44Rule/lan-to-dslite-c
    excludeNatRules:
      - NAT44Rule/lan-to-dslite-ra
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
        restoreCommand: [sudo, conntrack]
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

You can also provide `snatAddresses` directly when the addresses are static:

```yaml
spec:
  snatAddresses: [192.0.0.2, 192.0.0.3, 192.0.0.4]
```

## How restore works

The controller runs:

```bash
conntrack --dump -o extended -n <snat-address>
```

`extended` output includes the conntrack mark. routerd converts each line into
a delete-then-insert restore script and sends it over SSH. Preserving `ct mark`
matters when policy routing uses conntrack marks to keep an existing flow on
the same egress path.

`restoreCommand` defaults to `[conntrack]`. Use `[sudo, conntrack]` when the
target user needs privilege elevation.

## Event stream mode

The snapshot implementation is intentionally simple and remains the default.
For routers with high session churn, set `mode: event-stream`: routerd keeps a
local `conntrack -E -o extended` reader alive and sends incremental
create/update/destroy batches instead of starting a fresh snapshot cycle each
interval.

`event-stream` keeps snapshot sync as the safety net. On startup and after
local stream loss, the target first receives a full snapshot resync. Only after
that resync completes is the resource reported as `Synced`.

```yaml
spec:
  mode: event-stream
  natRules:
    - NAT44Rule/lan-to-dslite-a
  targets:
    - name: standby
      host: routerd-standby.lan.example
      user: routerd
      restoreCommand: [sudo, conntrack]
```

The operational differences are:

- lower steady-state `ssh` and `conntrack` process churn;
- lower failover warm-up delay for active sessions;
- status that reports stream state, queue depth, last event time, last batch
  time, last resync time, and resync count;
- explicit fallback to snapshot resync when stream integrity is uncertain.

The first event stream implementation still uses the existing SSH restore path
for each event batch. Long-lived target restore sessions can be added later if
the batch restore path becomes the next bottleneck.

See [ADR 0016](../adr/0016-nat44-session-sync-event-stream.md) for the design
and migration plan.

## Check it

```bash
routerctl describe NAT44SessionSync/dslite-abc-sessions
routerd serve --controllers nat44-session-sync --config router.yaml
```

When `spec.when` is false, status stays `Pending` with reason `WhenFalse`. When
a referenced `NAT44Rule` has not resolved `snatAddress` yet, status stays
`Pending` with reason `SNATAddressPending`.
