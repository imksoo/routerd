# ADR 0007: Provider Action Execution (gated, executor-isolated)

![Diagram showing ADR 0007 provider action execution from inert planner actionPlan through ProviderActionPolicy gating and approval to isolated executor plugin journaling](/img/diagrams/adr-0007-provider-action-execution.png)

## Status

Proposed; Accepted for experimental implementation — 2026-05-30.

This ADR builds directly on [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md)
and the [Selective Address Mobility](../reference/selective-address-mobility)
dataplane. It is **experimental**.

Phase 5.0 (this chunk) lands the **design, the `ProviderActionPolicy` Kind, and
the `action_executions` journal only**. Phase 5.0 contains **no execution state
machine, no `routerctl action` commands, no executor invocation, and no real
provider CLI/SDK calls** — a fake executor and the execution path arrive in
later chunks.

## Context

- **Phase 4.1 landed dry-run `actionPlans`.** Planner plugins (capability
  `propose.providerAction`) emit display-only provider operations recorded on a
  `DynamicConfigPart`. routerd **never** executes an `actionPlan` and never
  invokes a provider CLI/SDK from them; `pkg/plugin.ValidateActionPlan` rejects
  `mode=execute`. They exist purely so EventSubscription-driven runs stay
  reviewable.
- **The SAM dataplane is real-cloud validated.** Selective Address Mobility has
  passed clean smokes across AWS, Azure, and OCI (3-cloud parity). The on-prem
  side delivers a claimed address over the overlay; the cloud side still needs
  the provider to actually attach/detach the secondary IP on its NIC. Today that
  attach/detach is a manual operator step.
- **The missing piece is gated execution.** We want routerd to be able to drive
  the approved provider mutation, but provider credentials must never enter
  routerd core, and execution must be off by default, explicitly approved, and
  fully journaled.

## Decision

### Two plugin roles

- **Planner** (Phase 4.1, capability `propose.providerAction`): emits dry-run
  `actionPlans`; holds **no** credentials.
- **Executor** (Phase 5, capability `execute.providerAction` — a new enum value
  on `PluginSpec.Capabilities`): performs the action **in its own process with
  its own credentials**, using cloud-native identity (AWS instance profile,
  Azure managed identity, OCI instance principal) or its own environment.

### Credential model (hard invariant)

**routerd core NEVER holds, reads, or passes provider credentials.** routerd
passes the executor only the approved `actionPlan` (no secrets) plus the
Phase-4.0 allowlisted/redacted context. The executor authenticates itself to the
cloud. Credentials never traverse routerd core or the `action_executions`
journal.

### Flow

1. A planner emits an `actionPlan` on a `DynamicConfigPart` (dry-run, as today).
2. The plan is **imported** into the `action_executions` journal as
   `status=pending`, keyed by `idempotencyKey`.
3. **Approval**: an operator approves it, OR policy auto-approves it (only when
   `requireApproval=false` AND `enabled=true` AND not `dryRunOnly` AND the
   allowlists match).
4. **Execute**: routerd invokes the matching executor plugin, handing it the
   approved plan (no secrets).
5. The **result is journaled**: `succeeded` / `failed` / `skipped` /
   `rolledBack`.

### `ProviderActionPolicy` Kind

A new Kind (`apiVersion: hybrid.routerd.net/v1alpha1`) gates execution. It is
defined in the `hybrid` group to sit alongside `RemoteAddressClaim` and
`CloudProviderProfile`, which it governs. Its zero value is the safe locked-down
state:

- `enabled` (bool, default false) — execution is disabled unless true.
- `dryRunOnly` (`*bool`, default true when nil) — only dry-run permitted.
- `requireApproval` (`*bool`, default true when nil).
- `allowedProviders` / `allowedProviderRefs` / `allowedActions` — empty means
  none (default-deny).
- `allowedCIDRs` — the action target address must fall within one.
- `maxActionsPerRun` (int, default 0 = no actions; the operator must set a
  positive bound).
- `allowUndo` (bool, default false).
- `executionWindow` (string, validated leniently).

### `routerctl action` UX surface (later chunks, documented here)

`routerctl action list`, `show`, `approve`, `execute --dry-run|--approved`,
`journal`, and `rollback --dry-run`. These are the operator surface; Phase 5.0
ships **none** of them.

### Phasing

- **Phase 5.0** — framework + data model: `ProviderActionPolicy` Kind, the
  `action_executions` journal, schema/validation. A **fake executor** (no real
  cloud) arrives in Phase 5.0's later chunk to exercise the path end-to-end.
  **Phase 5.0 calls no real provider CLI/SDK.**
- **Live mutation smoke** — gated, one provider at a time, against the
  SAM-validated cloud.
- **Phase 5.x** — hardening (windows, rate limits, richer rollback, audit).

## Hard safety stops

1. **Execution disabled by default.** `ProviderActionPolicy.enabled` defaults
   false; `dryRunOnly` defaults true.
2. **Explicit approval required.** An action executes only if approved (operator
   approval, OR policy `requireApproval=false` with `enabled` + not `dryRunOnly`
   + allowlist match).
3. **`mode=execute` is rejected** unless there is an approved
   `action_execution` that policy permits.
4. **`idempotencyKey` required**; a key that already succeeded is not executed
   again (skipped / duplicate). Import is `ON CONFLICT DO NOTHING`, so a repeated
   key never creates a second execution row.
5. **All execution results are journaled** — `succeeded` / `failed` /
   `skipped` / `rolledBack`, plus the `pending` / `approved` lifecycle states.
6. **Undo/rollback is best-effort** — an executor may not support it; rollback
   is gated by `allowUndo`.
7. **Provider credentials never traverse routerd core** — the executor holds and
   uses its own cloud-native identity.
8. **Phase 5.0 calls no real provider CLI/SDK** — fake executor only.

## Consequences

- routerd gains a reviewable, default-off path to drive the cloud-side SAM
  attach/detach without ever holding cloud credentials.
- The journal is the audit trail and the idempotency guard; it is the single
  source of truth for what was executed.
- The asymmetry between provision and de-provision (TTL teardown with hysteresis,
  per ADR 0006) is honoured by keeping execution gated and journaled rather than
  reactive to every event.
