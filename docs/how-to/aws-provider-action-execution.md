# How-to: execute the AWS SAM provider action via the gated executor

:::warning Experimental — Phase 5.1
This is the **gated live-mutation** path for CloudEdge provider action execution.
It is **experimental** and AWS-only. It builds on
[ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md)
and the [Selective Address Mobility](../reference/selective-address-mobility.md)
dataplane. Do **not** run the live execute step in production or against shared
resources. Live execute happens **only after explicit owner go** following review
of this runbook and the read-only preflight evidence.
:::

The SAM dataplane is already real-cloud validated on AWS×PVE (an ENI secondary
private IP plus source/dest check disabled). Until now that attach/detach was a
**manual operator step**. This guide describes the `aws-provider-executor` plugin
that performs the same mutation through the **gated, journaled** execution path
(ADR 0007) instead of by hand.

## 1. Scope &amp; boundaries

- **AWS only. Exactly one provider.** No Azure, no OCI in this runbook.
- **Topology:** 1 `routerd-cloud` node + 1 cloud-client + 1 on-prem-client, with
  exactly **one captured `/32`** moving from on-prem to the cloud ENI. In lab
  address terms (per the SAM reference) the cloud-client is `.7` and the
  on-prem-client is `.9`.
- **Dedicated lab only.** A throwaway VPC / subnet / instance created for this
  test. **No production or shared resources.** No EIPs, security groups, route
  tables, or instances that anything else depends on.
- **Live execute only after explicit owner go.** Everything up to and including
  the read-only preflight (Section 4) is runnable freely; the mutation in
  Section 7 is gated.

## 2. Executor design

The `aws-provider-executor` is a plugin advertising the capability
`execute.providerAction` (the Phase 5 enum value on `PluginSpec.Capabilities`).
It runs in **its own process** and authenticates with the **EC2 instance IAM
role (instance profile)** via the AWS CLI. **routerd core passes it no
credentials** — the executor uses cloud-native identity only, per the ADR 0007
hard invariant.

It reads one `ExecuteActionRequest` on **stdin** and writes one
`ExecuteActionResult` on stdout. The request spec carries `Action`, `Provider`,
`ProviderRef`, `Target` (the provider keys: for AWS `nicRef` = ENI id, `address`,
`region`), `Parameters`, `Mode` (`dry-run` | `execute`), `IdempotencyKey`, and the
allowlisted `Context`. The result carries `Status` (`succeeded` | `failed` |
`skipped`), `Message`, `Observed` (non-secret facts the journal records),
`UndoAvailable`, and `Error`.

**`dry-run` mode performs NO mutation** — describe / read-only calls only.
`execute` mode mutates.

### `assign-secondary-ip`

Attach the captured `/32` to the cloud ENI.

- **dry-run** (read-only): describe the ENI to report current secondary IPs, then
  report `would assign <address> to <eni>`.

  ```sh
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>"
  ```

- **execute** (mutating):

  ```sh
  aws ec2 assign-private-ip-addresses \
    --network-interface-id "<eni-id>" \
    --private-ip-addresses "<address>" --region "<region>"
  ```

### `ensure-forwarding-enabled`

Disable the ENI source/dest check so the cloud node can forward for the captured
address.

- **dry-run** (read-only): describe the current `SourceDestCheck`, report
  `would set SourceDestCheck=false`.

- **execute** (mutating): **first describe the current `SourceDestCheck` and
  capture the prior value into `Observed`, then** disable it.

  ```sh
  # 1. capture prior state (read-only) BEFORE mutating
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>" \
    --query 'NetworkInterfaces[0].SourceDestCheck'

  # 2. mutate
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --no-source-dest-check --region "<region>"
  ```

  The result's `Observed` **MUST** include `priorSourceDestCheck=<true|false>` so
  the journal records the state that existed *before* this action ran. The undo
  step depends on it.

### `unassign-secondary-ip` (undo of `assign-secondary-ip`)

```sh
aws ec2 unassign-private-ip-addresses \
  --network-interface-id "<eni-id>" \
  --private-ip-addresses "<address>" --region "<region>"
```

### `ensure-forwarding-disabled` (undo of `ensure-forwarding-enabled`)

**Restore the PRIOR state recorded in the journal's `Observed.priorSourceDestCheck`.**
This is the load-bearing safety rule:

- If `priorSourceDestCheck == true` → the check was on before we touched it →
  restore it:

  ```sh
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --source-dest-check --region "<region>"
  ```

- If `priorSourceDestCheck == false` → the check was **already disabled** before
  we ran (the ENI was already a forwarder) → **NO-OP**. Return
  `Status=skipped`. Do **not** force the check back on.

**NEVER hardcode undo = enable the check.** A blind "undo re-enables
source/dest-check" would break an appliance/ENI that was already a forwarder for
its own reasons. The undo must read back what we observed and only revert what we
actually changed.

## 3. IAM least-privilege

The instance profile attached to the executor's EC2 instance gets **exactly these
four EC2 actions and nothing more**:

| Action | Used by |
|--------|---------|
| `ec2:DescribeNetworkInterfaces` | dry-run + preflight + prior-state capture |
| `ec2:AssignPrivateIpAddresses` | `assign-secondary-ip` execute |
| `ec2:UnassignPrivateIpAddresses` | `unassign-secondary-ip` undo |
| `ec2:ModifyNetworkInterfaceAttribute` | forwarding enable/disable execute |

Scope to the lab ENI / VPC via resource ARNs and conditions wherever the API
supports it (the mutating ENI actions are resource-scopable to the lab ENI ARN;
`Describe*` is not resource-scopable and is restricted by condition keys such as
`ec2:Region` / `ec2:Vpc` where applicable):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DescribeEnis",
      "Effect": "Allow",
      "Action": "ec2:DescribeNetworkInterfaces",
      "Resource": "*",
      "Condition": { "StringEquals": { "ec2:Region": "<region>" } }
    },
    {
      "Sid": "MutateLabEni",
      "Effect": "Allow",
      "Action": [
        "ec2:AssignPrivateIpAddresses",
        "ec2:UnassignPrivateIpAddresses",
        "ec2:ModifyNetworkInterfaceAttribute"
      ],
      "Resource": "arn:aws:ec2:<region>:<account-id>:network-interface/<eni-id>"
    }
  ]
}
```

**No broader EC2 permissions. No IAM/STS write. No other AWS services.** If a
needed call is not on this list, the runbook stops rather than widening the role.

## 4. Read-only preflight

Run **before any mutation**, against the dedicated lab, to confirm the target.
**None of these mutate.** lab-codex runs these and captures the output as the
evidence the owner reviews before granting go.

```sh
# Target ENI + its current secondary private IPs + current SourceDestCheck
aws ec2 describe-network-interfaces \
  --network-interface-ids "<eni-id>" --region "<region>" \
  --query 'NetworkInterfaces[0].{Eni:NetworkInterfaceId,SrcDstCheck:SourceDestCheck,PrivateIps:PrivateIpAddresses[*].PrivateIpAddress}'

# The instance the ENI is attached to
aws ec2 describe-instances \
  --filters "Name=network-interface.network-interface-id,Values=<eni-id>" \
  --region "<region>"

# Subnet of the ENI
aws ec2 describe-subnets \
  --subnet-ids "<subnet-id>" --region "<region>"

# Route table(s) for that subnet (confirm default gateway / no surprises)
aws ec2 describe-route-tables \
  --filters "Name=association.subnet-id,Values=<subnet-id>" \
  --region "<region>"
```

Then confirm:

1. **The IAM role has only the 4 permissions** in Section 3 — inspect the
   instance profile's attached policy and verify no broader EC2, no IAM/STS write,
   no other services. (Read-only inspection of the policy document; do not modify
   it here.)
2. **The address is not already assigned** — the `<address>` must **not** already
   appear in the ENI's `PrivateIpAddresses` from the first describe above. If it
   is already there, assign is a no-op / the lab is dirty — stop and investigate.
3. **`SourceDestCheck` current value is recorded** — this is the value the
   executor will capture as `priorSourceDestCheck` during execute.

## 5. Action journal fields the smoke relies on

The `action_executions` journal records, per action:

- `idempotencyKey` — the dedupe key; a key that already succeeded is not run again.
- `provider` — `aws`.
- `action` — e.g. `assign-secondary-ip`, `ensure-forwarding-enabled`.
- `target` — `eni`, `address`, `region`.
- `status` — `pending` / `approved` / `succeeded` / `failed` / `skipped` /
  `rolledBack`.
- `Observed.priorSourceDestCheck` — `true` | `false`, captured before mutating;
  the undo of `ensure-forwarding-enabled` reads this.
- `executedAt` — timestamp.
- `result` / `error` — the `ExecuteActionResult` message / `Error`.

The journal is the single source of truth for what ran and for the idempotency
guard. Credentials are **never** journaled.

## 6. Undo / teardown plan

Reverse, in order, anything that was applied. Every step must be describable
**before** the live run begins.

1. **Undo forwarding** — `ensure-forwarding-disabled`, applying the
   **restore-prior rule** from Section 2: if `Observed.priorSourceDestCheck`
   was `true`, run `--source-dest-check` to re-enable it; if it was `false`,
   **NO-OP** (skipped). Never blindly force the check on.
2. **Unassign the secondary IP** — `unassign-secondary-ip`:

   ```sh
   aws ec2 unassign-private-ip-addresses \
     --network-interface-id "<eni-id>" \
     --private-ip-addresses "<address>" --region "<region>"
   ```
3. **Stop / terminate lab instances and release cost-bearing resources** — stop
   or terminate the `routerd-cloud`, cloud-client, and on-prem-client lab
   instances; release any allocated **EIP**; delete any orphaned **EBS** volumes;
   tear down the dedicated VPC/subnet/SG if created only for this test.

**Stop or delete every cost-bearing resource after evidence is captured.** Do not
leave lab instances running idle.

## 7. Live mutation smoke plan + acceptance

The smoke exercises the full gated path. Run only after the Section 9 gate is
granted.

Sequence:

1. `actionPlan` generated (planner, dry-run, as Phase 4.1).
2. Action **imported** into the journal as `pending` (keyed by `idempotencyKey`).
3. Action **approved** (`routerctl action approve`).
4. Action **executed by the `aws-provider-executor`**
   (`routerctl action execute --approved`).
5. Journal shows `succeeded`.

Acceptance (all must hold):

- [ ] actionPlan generated → imported → approved → executed → journal `succeeded`.
- [ ] The **secondary IP exists on the ENI** (`describe-network-interfaces`
      shows `<address>` in `PrivateIpAddresses`).
- [ ] **Source/dest check disabled** on the ENI (`SourceDestCheck=false`), with
      `Observed.priorSourceDestCheck` recorded in the journal.
- [ ] `routerd-cloud` does **NOT** retain the address as a local OS address when
      `configureOSAddress=false` (capture is route/forward-only, no OS address).
- [ ] `RemoteAddressClaim` reaches **Ready**.
- [ ] `routerctl doctor` hybrid checks **pass**.
- [ ] cloud-client **`.7`** ↔ on-prem-client **`.9`** — **ping and ssh both
      ways** succeed.
- [ ] **NAT absent** on the captured path (routed/forwarded, not translated).
- [ ] **Default gateway unchanged** on every node.
- [ ] **Teardown / undo succeeds** (Section 6), including the source/dest-check
      restore-prior rule.
- [ ] **Cost-bearing resources stopped / deleted** after evidence capture.

## 8. Hard stops

Abort immediately (do not "work around") if any of the following is true:

1. Credentials would pass **through routerd core** (they must not — executor uses
   its own instance profile only).
2. The action would affect a **non-lab resource**.
3. **More than one provider** is in play.
4. **Rollback / cleanup is not describable beforehand.**
5. The provider API returns an **ambiguous / partial success**.
6. A **cost-bearing resource would be left running** without an active test.
7. **Waiting more than 10 minutes** for a human decision while cloud resources are
   running → **stop and deallocate** (stop instances) to cut cost; resume after
   the decision.
8. Any command **implies a production or shared mutation**.

## 9. Gate to run live

Live mutation runs **only after explicit owner go**, granted **after** the owner
reviews **this runbook** and the **read-only preflight evidence** (Section 4).
Until that go is given, only the read-only steps may run.
