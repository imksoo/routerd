# CloudEdge Mobility D5 AWS Maintenance Smoke

Result: PASS

Date: 2026-05-31
Build: main 99eb1d45
Evidence bundle: `/home/imksoo/routerd-labs/cloudedge-mobility/evidence/20260531T215831Z-d5-aws-rerun-99eb1d45`

## Scenario

- AWS-only D5 live maintenance / capture migration.
- Existing active router A was reused: `i-001f62ac01d66e782`, ENI-A `eni-0d17f203a6717e4d9`, primary `10.77.60.4`.
- Standby router B was recreated for this run: `i-045382a4f5bbf6fc0`, ENI-B `eni-017dd140722f5d819`, primary `10.77.60.14`, `t3.small`.
- AWS cloud client was reused: `i-0c5d4e3578e7669a9`, `10.77.60.11`.
- Captured address: on-prem client `10.77.60.10/32`.

## Initial Capture

- A imported and executed:
  - `assign-secondary-ip` epoch 1 for `10.77.60.10/32` on ENI-A.
  - `ensure-forwarding-enabled` epoch 1 on ENI-A.
- AWS provider state after initial execute:
  - ENI-A: `10.77.60.4,10.77.60.10`, `SourceDestCheck=false`.
  - ENI-B: `10.77.60.14`, `SourceDestCheck=true`.
- Before migration dataplane:
  - cloud-client `10.77.60.11 -> 10.77.60.10` ping: `3/3`, `0% loss`.
  - SSH reached on-prem client with source preserved: `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`.

## Drain And Migration

- `maintenance.drain=true` was applied declaratively to router A.
- A imported epoch 2 de-provision actions:
  - `unassign-secondary-ip` for `10.77.60.10/32` from ENI-A.
  - `ensure-forwarding-disabled` for ENI-A.
- B imported epoch 2 active-capture actions:
  - `assign-secondary-ip` for `10.77.60.10/32` to ENI-B.
  - `ensure-forwarding-enabled` for ENI-B.
- A unassign executed successfully and removed `.10` from ENI-A.
- B assign executed successfully and added `.10` to ENI-B.
- AWS provider state after migration:
  - ENI-A: `10.77.60.4`, `SourceDestCheck=true`.
  - ENI-B: `10.77.60.14,10.77.60.10`, `SourceDestCheck=false`.
- Capture epoch converged to holder `aws-router-b`, epoch `2`.

## Epoch Fence

- A epoch 1 actions succeeded before drain.
- A epoch 2 unassign and forwarding-disable remained journaled until executed.
- B epoch 2 assign and forwarding-enable executed successfully.
- Stale gate was verified with a non-provider journal probe:
  - an epoch 1 pending action for the same capture key was inserted as `d5-rerun-stale-probe-epoch1`;
  - `routerctl action import` changed it to `status=skipped`;
  - result message: `stale mobility capture epoch`.

## After Migration Dataplane

- B side `doctor hybrid`: PASS.
- B side `routerd_mss`: present for `ens5 -> wg-hybrid`.
- On-prem `routerd_mss`: present for `ens21 -> wg-hybrid`.
- After neighbor refresh, cloud-client `10.77.60.11 -> 10.77.60.10` ping passed `3/3` for three consecutive rounds.
- SSH reached on-prem client through B with source preserved:
  - `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`.
- Client default gateway remained unchanged: `default via 10.77.60.1`.

## Teardown

- Removed `10.77.60.10` from ENI-B.
- Restored `SourceDestCheck=true` on ENI-A and ENI-B.
- Restored IAM inline policy to the pre-B-scope document.
- Terminated B.
- Stopped A and cloud-client.
- Final cost state:
  - A: `stopped`.
  - cloud-client: `stopped`.
  - B: `terminated`.
  - ENI-A baseline: only `10.77.60.4`, `SourceDestCheck=true`.
