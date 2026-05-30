# CloudEdge Event Federation — checkpoint (Phase 1 + 1.5 complete)

Status: **experimental** (in development; NOT recommended-stable)
Branch: `event-federation` · Checkpoint commit: `2bfd8b4d` · Date: 2026-05-30

## Summary

CloudEdge Event Federation (ADR 0006) Phase 1 and the Phase 1.5 cleanup are
complete on `event-federation`. This is the local-only foundation of the
routerd-to-routerd typed event bus: an observed-fact envelope, an `EventGroup`
Kind, a SQLite local store, and a CLI to emit/list events. **No cross-node
delivery yet** — that is Phase 2.

## What is in at this checkpoint

- `EventGroup` Kind (`federation.routerd.net/v1alpha1`) + validation.
- `federation.Event` envelope (observed fact; not config, not a command) with
  `Normalize`/`Validate`/`IsExpired`.
- SQLite `federation_events` table, idempotent `RecordFederationEvent`
  (`ON CONFLICT(id) DO NOTHING`), and filtered `ListFederationEvents`
  (group filter + read-time expiry filter).
- `routerctl federation event emit/list` (alias `fed`).
- Unit + CLI tests; ADR 0006 reconciled to reflect implemented state.

Semantics fixed here (do not regress in later phases): store idempotency is keyed
on the event **`id`**; **`dedupeKey`** is a subscription-side grouping key, not a
DB unique constraint in Phase 1.

## Next: Phase 2 — transport only

`routerd-eventd` + `EventPeer` push delivery over the overlay + HMAC +
`event_deliveries` + retention prune. Explicitly **out of Phase 2**:
`EventSubscription`, plugin triggering, `DynamicConfigPart` generation,
ARP/Clients observer, and any provider mutation (those are Phase 3+).

This is a branch checkpoint note, **not** a release tag.
