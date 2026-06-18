# Federation Auto-Remediation Readiness Matrix

This matrix classifies each remediation action from `routerctl doctor federation --remediation-plan` by execution readiness. P4 generates plans only; P5+ would wire execution.

## Actions

| # | Action constant | Check code | Safe | Operator approval | Auto-execute ready | Rationale |
|---|-----------------|-----------|------|--------------------|--------------------|-----------|
| 1 | `retry-failed-deliveries` | `failed-deliveries` | yes | no | **ready** | Re-enqueues outbox entries; idempotent delivery with HMAC; receiver deduplicates. |
| 2 | `investigate-pending-deliveries` | `pending-deliveries` | yes | no | **inspect-only** | Pending may be in-flight. Action is diagnostic (list pending), not mutating. |
| 3 | `force-repush-stale-ttl` | `stale-ttl` | yes | no | **ready** | Re-pushes events whose TTL was refreshed locally but not yet delivered. Idempotent; receiver applies latest TTL. |
| 4 | `check-peer-connectivity` | `delivery-lag` | yes | no | **inspect-only** | Probes overlay/TCP reachability to peer endpoint. Diagnostic; cannot fix network issues. |
| 5 | `configure-peer-endpoint` | `expected-delivery-no-endpoint` | **no** | **yes** | **not ready** | Requires config mutation (add EventPeer endpoint). Must be operator-reviewed; wrong endpoint = data leak risk. |
| 6 | `investigate-missing-delivery-rows` | `expected-delivery` | yes | no | **inspect-only** | Expected peer has endpoint but no delivery rows. Diagnostic query only. |
| 7 | `inspect-failed-subscription-runs` | `subscription-runs` | yes | no | **inspect-only** | Lists recent failed/pending subscription runs. Diagnostic; does not retry. |

## Readiness categories

- **ready**: Action is safe, idempotent, and has no side effects beyond the intended fix. Can be wired to auto-execute with `FederationSLO` thresholds gating frequency.
- **inspect-only**: Action is diagnostic. It collects information but does not mutate state. Useful for triage dashboards and alerting, not auto-remediation.
- **not ready**: Action requires operator judgment or config changes. Must remain behind approval gating even in future auto-execute phases.

## P4 contract

In P4, `--remediation-plan` emits all 7 actions as a **plan-only** JSON document. The plan:

1. Never mutates state (read-only doctor run + plan generation).
2. Uses stable typed action constants (not free-text strings).
3. Deduplicates by `(action, group, peer, resource)`.
4. Sorts deterministically for diff-friendly output.
5. Includes `safe` and `requiresOperatorApproval` flags per action.

## Pre-conditions for P5+ auto-execute

Before any action graduates from plan-only to auto-execute:

1. The action must be classified as **ready** in this matrix.
2. A `FederationSLO` resource must exist for the target EventGroup.
3. The `ProviderActionPolicy` (or a new `FederationRemediationPolicy`) must gate execution.
4. Rate limiting must prevent remediation storms (e.g., max 3 retries per peer per hour).
5. Every execution must be journaled in the `action_executions` table.
6. Qualification evidence must demonstrate the action resolving the fault without side effects.
