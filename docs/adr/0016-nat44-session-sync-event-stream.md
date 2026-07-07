# ADR 0016: NAT44 session sync event stream mode

## Status

Accepted

## Context

`NAT44SessionSync` currently uses a snapshot loop. On every reconcile it dumps
selected local conntrack entries with `conntrack --dump -o extended -n
<snat-address>`, builds a delete-then-insert restore script, and sends that
script to every target over SSH.

This keeps failover behavior simple and idempotent, but it also creates work on
every interval even when only a small number of flows changed. homert02
observations showed the controller taking several seconds per 30 second cycle,
with repeated `conntrack`, `ssh`, and remote restore command process creation.

## Decision

Add an event stream mode for NAT44 session sync while keeping `snapshot` as the
compatible default.

The event stream mode uses a long-lived local conntrack event reader. The local
side consumes conntrack create, update, and destroy events, filters them to the
configured SNAT addresses or NAT rules, converts them to restore operations,
and sends ordered batches to each target. The first implementation still uses
the existing SSH restore path for each batch; a long-lived target transport is
left as a follow-up optimization once local snapshot churn has been removed.

The first implementation must preserve these properties:

- `snapshot` remains supported and keeps the current behavior.
- `event-stream` starts with a full snapshot resync before accepting live
  events.
- stream loss forces the worker to restart and perform a snapshot resync before
  returning to healthy status.
- target status exposes stream state, last event time, last batch time, last
  resync time, resync count, queued event count, and last error.
- all restore operations remain idempotent: duplicate inserts and missing
  deletes are not fatal.

## Transport

The target transport should reuse SSH because operators already have SSH
credentials and privilege boundaries for snapshot sync. To avoid per-cycle
handshake cost, the implementation should use either:

- SSH multiplexing with `ControlMaster=auto` and `ControlPersist`, or
- one long-lived SSH process running a small remote restore loop.

The first implementation reuses the current SSH script path for each event
batch to keep the runtime change small and preserve the existing idempotent
restore behavior. A long-lived restore loop remains the preferred follow-up
because it also avoids starting `sh` and `conntrack` for every batch. A later
optimization can replace per-entry `conntrack` calls with a small installed
helper if the shell loop becomes the next bottleneck.

## Reconciliation Model

Event stream mode still needs a periodic controller entry point, but that
periodic reconcile should supervise long-lived workers rather than perform the
full sync itself.

Responsibilities:

- build desired sync jobs from `NAT44SessionSync` resources;
- start, stop, or restart workers when spec, target, or dependency status
  changes;
- report worker health into resource status;
- trigger a full resync when the worker reports an unsafe gap.

The worker owns the hot path:

- read local conntrack events;
- filter by SNAT address;
- coalesce bursts into small batches;
- send batches to every target;
- apply backpressure with bounded queues;
- mark a target `Degraded` and request resync if it falls behind.

## Failure Handling

Event stream mode is eventually consistent. A target must not be marked
`Synced` until it has completed an initial snapshot and is consuming live
events.

Failure cases:

- local event reader exits: restart reader and resync all targets;
- target SSH batch fails: mark the batch result as `Error` or `Degraded` and
  continue reporting the failing target status;
- event stream exits: restart the worker and perform a snapshot resync;
- restore batch has partial failures: keep idempotent duplicate/missing cases
  healthy, mark real restore failures as `Degraded` or `Error`;
- resource dependencies become pending: stop workers and report `Pending`.

## Migration Plan

1. Keep the existing snapshot implementation as the default.
2. Add API and validation for `mode: event-stream`.
3. Add the worker manager, local event reader, batch restore path, and status
   reporting behind `event-stream`.
4. Test on homert02 with both modes available:
   - compare `nat44-session-sync` duration;
   - compare 75 second `execve` counts for `ssh` and `conntrack`;
   - verify failover behavior;
   - force SSH disconnect and confirm snapshot resync recovery.
5. After event stream mode is stable, consider making it the recommended mode
   for HA routers with high session churn.

## Consequences

Event stream mode reduces steady-state process churn and sync latency, but it
adds long-lived worker lifecycle, backpressure, and resync logic. Snapshot mode
remains useful as a simple fallback and as the recovery mechanism when stream
integrity is uncertain.
