# CloudEdge Autonomous Lab (`cloudedge-labctl`)

> Experimental (CloudEdge). A single-command harness that lets an agent run
> cloud-edge **Selective Address Mobility (SAM)** failover labs **without a human
> reading runbooks**. It fixes the interface and implements all non-cloud logic
> (run-id/tag convention, TTL + teardown cost guard, fault primitives, connectivity
> matrix, evidence assembly). Real per-provider provisioning either wraps the
> existing [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo)
> package or is marked `TODO(lab-operator)` for Terraform/CLI wiring.

The harness is `scripts/cloudedge-labctl.sh`, with two helpers:

- `scripts/cloudedge-connectivity-matrix.sh` — directed ping+ssh matrix + assertions.
- `scripts/cloudedge-evidence-schema.json` — JSON schema for the run evidence.

`--help`, dry paths, and `down --expired` need **no cloud credentials**.

## Lifecycle

```sh
scripts/cloudedge-labctl.sh up        --profile full --provider aws,oci,azure,onprem --ttl 4h
scripts/cloudedge-labctl.sh deploy    --commit HEAD          # or --build <dist path>
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix.json
scripts/cloudedge-labctl.sh failover  --provider aws --fault stop-active
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix-after.json
scripts/cloudedge-labctl.sh evidence  collect --out evidence/<run-id> --matrix-json /tmp/matrix-after.json
scripts/cloudedge-labctl.sh down      --run-id <run-id> --force
```

`up` prints the **run-id** on stdout; capture it and pass it to later commands.
Cloud mutations are **DRY by default** (`CE_DRY_RUN=1`); set `CE_DRY_RUN=0` to act
for real once credentials and budget are approved.

## Profiles

| Profile | Sites | Use |
| --- | --- | --- |
| `minimal` | on-prem + 1 cloud | cheapest smoke; interface/CI shake-out |
| `provider` | one provider A/B routers + client | provider parity (AWS/OCI/Azure seize) |
| `full` | on-prem + AWS + OCI + Azure | the 4-site `/24` 12-flow demo |
| `soak` | a `full` run held open for its whole TTL | long-running heartbeat/accumulation checks |

`soak` is operationally a `full` run with a long `--ttl` left up (do not run
`down` until TTL); use it to exercise heartbeat-event accumulation and reconverge.

## TTL and cost policy

Every cloud resource MUST carry these tags (helpers `cloudedge_tags()` emit them,
`up` stamps them):

```text
routerd.cloudedge.run_id          <UTCdate>T<time>-cloudedge-<scenario>
routerd.cloudedge.owner
routerd.cloudedge.ttl_expires_at  absolute UTC RFC3339
routerd.cloudedge.provider
routerd.cloudedge.purpose
```

Cost guard rules:

- `up --ttl <dur>` stamps `ttl_expires_at`. Pick the shortest TTL that fits the run.
- `down --run-id <id>` tears down one run; `down --expired` tears down **any** run
  past its TTL (safe no-op when there is no lab — exit 0).
- An **EXIT trap** in the harness tears down the active run if an orchestrated
  chain aborts unexpectedly (armed for chained/sourced flows; a normal `up`
  leaves the lab alive until explicit `down` or TTL).
- Always run `down` (or `down --expired` from a janitor) after every run, even on
  failure. Past-TTL labs are cleanable without knowing the run-id.

## Fault primitives (`failover --fault`)

| Fault | Meaning | First-pass wiring |
| --- | --- | --- |
| `stop-active` | stop the active router VM/instance | provider stop CLI (see `reset-lab.sh`) |
| `drain` | MobilityPool `maintenance.drain=true` on active | reuse `run-demo.sh` `*-drain.yaml` |
| `heartbeat-stop` | stop `routerd-eventd` (federation heartbeats cease) | ssh `systemctl stop` |
| `executor-fail` | provider action executor denied (identity scope-down) | identity policy |
| `stale-replay` | replay a stale-epoch action; must be **fenced** | `probe_stale_gate_on_aws_b` |

Inject a fault, then re-run `smoke` and `evidence collect` to prove recovery.

## Evidence schema

`evidence collect --out <dir>` writes `<dir>/result.json` validating against
`scripts/cloudedge-evidence-schema.json`, plus `summary.md` and (if provided) the
connectivity matrix JSON. Shape:

```json
{
  "runId": "20260601T031500Z-cloudedge-aws-failover",
  "commit": "<sha>",
  "scenario": "aws-active-stop-seize",
  "result": "pass",
  "providers": {
    "aws":    {"dataplane": "pass", "providerState": "pass"},
    "oci":    {"dataplane": "pass", "providerState": "pass"},
    "azure":  {"dataplane": "pass", "providerState": "pass"},
    "onprem": {"dataplane": "pass", "providerState": "pass"}
  },
  "assertions": [
    {"name": "ownership_epoch_bumped", "result": "pass"},
    {"name": "allow_reassignment_maintained_until_success", "result": "pass"},
    {"name": "source_ip_preserved", "result": "pass"},
    {"name": "default_gateway_unchanged", "result": "pass"},
    {"name": "old_holder_residue_absent", "result": "pass"},
    {"name": "stale_action_fenced", "result": "pass"}
  ],
  "costGuard": {"ttlHours": 4, "teardown": "completed"}
}
```

Data-plane checks and `source_ip_preserved` / `default_gateway_unchanged` are
derived automatically from the connectivity matrix. The seize/fencing assertions
(`ownership_epoch_bumped`, `allow_reassignment_maintained_until_success`,
`old_holder_residue_absent`, `stale_action_fenced`) and `providerState` start as
`na` and are folded in by the lab operator from provider inventory / action
journal / `mobility_capture_epochs` (see `collect-evidence.sh`). A run is only
**PASS** when `result == pass` and every required assertion passes.

## The connectivity matrix

`cloudedge-connectivity-matrix.sh` runs every directed `src -> dst` flow over the
shared `/24` and asserts, per flow:

- **source-IP-preserved** — the destination sees the real source client IP (no NAT).
- **default-gw-unchanged** — the source client's default gateway is unchanged.
- **no-NAT** — ping reaches the destination and the SSH peer IP equals the source IP.

Execution goes through a `MATRIX_RUNNER` indirection so the matrix is
**unit-runnable offline** (set `MATRIX_RUNNER` to a stub); with a real lab the
default runner uses `ssh`/`ping` against the demo env. Output is per-flow JSON
consumable by `evidence collect --matrix-json`.

## Autonomy charter (summary)

The agent owns the full loop — **lab up -> deploy -> fault inject -> data-plane
verify -> evidence -> teardown -> issue/PR update** — and runs it without a human
reading a runbook. Cloud actions are dry by default and gated behind explicit
credential/budget grants. The agent must always leave the lab torn down or within
its TTL cost guard, and must attach a schema-valid evidence bundle to any PASS.

## Human gates

Only these require a human; everything else is automated:

1. **Budget** — approving spend / raising the TTL or budget ceiling.
2. **Credentials/permissions** — granting cloud credentials and the least-privilege
   identity/roles the executor uses (no secrets are committed or passed to plugins).
3. **Merge** — final approval to merge a PR.
4. **Production** — any production rollout (never performed by the lab harness).

## Caveats

- This is a **lab harness**, not a production turnkey.
- First pass: real per-provider allocation/teardown/node-push are `TODO(lab-operator)`
  stubs or thin wrappers over the demo package — wire Terraform/OpenTofu or provider
  CLIs filtered by the run-id tag.
- Never commit real account IDs / subscription IDs / OCIDs / ENI/VNIC IDs / secrets
  / private keys. Use placeholder logical addresses as in `env.example`.
