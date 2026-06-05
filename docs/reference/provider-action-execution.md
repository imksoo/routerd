# Provider Action Execution (experimental)

> **Experimental.** This feature is gated, default-off, and under active
> development. See [ADR 0007](../adr/0007-provider-action-execution.md) for the
> design and safety rationale.

routerd can drive an approved cloud provider mutation (for example, attaching a
secondary IP for [Selective Address Mobility](./selective-address-mobility))
through an **executor plugin** — without ever holding cloud credentials.

## Credential model

- **The executor plugin holds the credentials, not routerd.** An executor
  (capability `execute.providerAction`) runs in its own process and
  authenticates with cloud-native identity (AWS instance profile, Azure managed
  identity, OCI instance principal) or its own environment.
- **routerd core never holds, reads, or passes provider credentials.** routerd
  hands the executor only the approved action plan (no secrets) plus the
  allowlisted, redacted plugin context.
- The `action_executions` journal records the plan and its outcome only — never
  any secret.

## `ProviderActionPolicy`

`apiVersion: hybrid.routerd.net/v1alpha1`, `kind: ProviderActionPolicy`. The zero
value is the safe locked-down state: execution disabled, dry-run only, approval
required, nothing allowlisted.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Execution is disabled unless `true`. |
| `dryRunOnly` | bool (pointer) | `true` when omitted | Only dry-run permitted; live mutation rejected. |
| `requireApproval` | bool (pointer) | `true` when omitted | Operator approval required before execution. |
| `allowedProviders` | list | empty = none | Providers permitted: `aws`, `azure`, `oci`, `gcp`. |
| `allowedProviderRefs` | list | empty = no restriction | Restrict to named `CloudProviderProfile` refs. |
| `allowedActions` | list | empty = none | Canonical verbs: `assign-secondary-ip`, `unassign-secondary-ip`, `ensure-forwarding-enabled`, `ensure-forwarding-disabled`. |
| `allowedCIDRs` | list | empty = no restriction | Action target address must fall within one CIDR. |
| `maxActionsPerRun` | int | `0` = no actions | Cap on actions per run; set a positive bound to permit any. |
| `allowUndo` | bool | `false` | Permit best-effort rollback. |
| `executionWindow` | string | empty = no window | Optional time window; validated leniently. |

Example (still locked down except for a single allowlisted action):

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: ProviderActionPolicy
metadata:
  name: sam-execution
spec:
  enabled: true
  dryRunOnly: false
  requireApproval: true
  allowedProviders: [aws]
  allowedActions: [assign-secondary-ip, unassign-secondary-ip]
  allowedCIDRs: [10.88.60.0/24]
  maxActionsPerRun: 4
  allowUndo: true
```

## Action lifecycle

An action plan proposed by a planner plugin is imported into the journal and
moves through these states:

```text
pending  ->  approved  ->  succeeded
                       ->  failed
                       ->  skipped
                            (succeeded) -> rolledBack
```

- **pending** — imported from an `actionPlan`, keyed by `idempotencyKey`,
  awaiting approval.
- **approved** — approved by an operator, or auto-approved by policy
  (`requireApproval: false` with `enabled` and not `dryRunOnly`).
- **succeeded / failed / skipped** — the executor's reported outcome. `skipped`
  covers a duplicate `idempotencyKey` that already succeeded, or a
  policy-declined action.
- **rolledBack** — a best-effort undo was applied to a previously succeeded
  action (only when `allowUndo` is true).

Import is idempotent: re-importing the same `idempotencyKey` never creates a
second row, so an action that already succeeded is never executed twice.

## `routerctl action` commands

> Shipped in a later Phase 5 chunk; documented here so the surface is stable.

| Command | Purpose |
| --- | --- |
| `routerctl action list` | List journal entries (filter by status/provider). |
| `routerctl action show ID` | Show one journal entry. |
| `routerctl action approve ID` | Operator approval: `pending` to `approved`. |
| `routerctl action execute --dry-run` | Validate and preview; no mutation. |
| `routerctl action execute --approved` | Execute approved actions permitted by policy. |
| `routerctl action journal` | Print the execution journal / audit trail. |
| `routerctl action rollback ID --dry-run` | Preview a best-effort undo (no mutation). |

## Dry-run versus execute

- **Dry-run** is the default and the only path permitted while `dryRunOnly` is
  true (or `enabled` is false). It validates the plan, checks the policy, and
  previews the effect, but makes **no** provider mutation.
- **Execute** performs the real mutation through the executor, and only when all
  hard safety stops are satisfied: `enabled`, not `dryRunOnly`, approved (or
  policy auto-approve), allowlist match, and within `maxActionsPerRun`.

See [ADR 0007](../adr/0007-provider-action-execution.md) for the full list of
hard safety stops.
