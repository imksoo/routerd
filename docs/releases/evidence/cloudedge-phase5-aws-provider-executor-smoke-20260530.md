# CloudEdge Phase 5.1 AWS Provider Executor Smoke

Result: PASS

Date: 2026-05-31 UTC  
Branch/build: `main` / `routerd v20260528.2308 (92f4cc94)` with local validator fix for `execute.providerAction`  
Evidence bundle: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260530T235341Z-phase5-aws-rebaseline-92f4cc94`

## Scope

- Provider mutation scope: AWS only.
- Account/region: `350538780953` / `ap-northeast-1`.
- Reused routerd-only SAM lab: `SourceLab=routerd-cloudedge-sam-aws-pve`.
- Target router instance: `routerd-cloud-aws` / `i-05b6cfd2b3e4e0da6`.
- Target client instance: `aws-cloud-client` / `i-0ae791389518353d6`.
- Target ENI: `eni-0904ccbed8d383f65`.
- Captured address: `10.88.60.9`.

## Rebaseline

Before mutation, the existing SAM lab was reset to a fresh provider baseline:

- `10.88.60.9` secondary private IP removed from `eni-0904ccbed8d383f65`.
- `SourceDestCheck=true` restored on the ENI.
- Post-reset evidence: `aws-router-eni-post-reset.json`, `aws-router-eni-post-reset-confirm.json`.

## IAM Gate

`routerd-cloud-aws` received an EC2 instance profile for the executor.

The inline policy allowed only:

- `ec2:DescribeNetworkInterfaces`
- `ec2:AssignPrivateIpAddresses`
- `ec2:UnassignPrivateIpAddresses`
- `ec2:ModifyNetworkInterfaceAttribute`

Mutation permissions were scoped to:

- Region: `ap-northeast-1`
- ENI ARN: `arn:aws:ec2:ap-northeast-1:350538780953:network-interface/eni-0904ccbed8d383f65`
- Resource tag: `Project=routerd-cloudedge-phase5`

Instance role preflight passed from the router:

- `aws sts get-caller-identity` returned `arn:aws:sts::350538780953:assumed-role/routerd-phase5-aws-executor-role/i-05b6cfd2b3e4e0da6`.
- `aws ec2 describe-network-interfaces` could read the target ENI.

## Executor Run

`aws-provider-executor` was built and installed on `routerd-cloud-aws`.

Two action journal entries were imported, approved, dry-run, and executed:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.88.60.9 to eni-0904ccbed8d383f65`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `disabled SourceDestCheck on eni-0904ccbed8d383f65 (prior=true)`
  - Observed journal fact: `priorSourceDestCheck=true`

AWS validation after mutation:

- ENI primary: `10.88.60.4`
- ENI secondary: `10.88.60.9`
- `SourceDestCheck=false`

## Dataplane Validation

Cloud side:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`.
- Delivery route: `10.88.60.9 dev wg-hybrid metric 120`.
- Local OS address absence: `10.88.60.9/32 absent from local interfaces`.
- MSS clamp: `routerd_mss covers ens5 -> wg-hybrid`.

On-prem side:

- router07 `routerctl doctor hybrid`: `overall=pass`, `pass=13`, `warn=0`, `fail=0`, `skip=1`.
- Proxy ARP claim for cloud client `10.88.60.7` remained healthy.

Client connectivity:

- cloud-client `10.88.60.7` -> onprem-client `10.88.60.9` ping: `3/3`, `0% packet loss`.
- onprem-client `10.88.60.9` -> cloud-client `10.88.60.7` ping: `3/3`, `0% packet loss`.
- cloud -> onprem SSH preserved source:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
- onprem -> cloud SSH preserved source:
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- Default gateways unchanged:
  - cloud-client: `default via 10.88.60.1 dev ens5`
  - onprem-client: `default via 10.88.60.1 dev eth0`
- NAT: absent by SSH source preservation.

## Rollback And Restore

Rollback was exercised through `routerctl action rollback`:

- `ensure-forwarding-enabled` rollback dry-run: would re-enable `SourceDestCheck`.
- `assign-secondary-ip` rollback dry-run: would unassign `10.88.60.9`.
- Live rollback result:
  - action 2: `rolledBack`, restored `SourceDestCheck=true`.
  - action 1: `rolledBack`, unassigned `10.88.60.9`.

Final teardown used option B: restore the previous SAM lab state.

- `10.88.60.9` secondary private IP present again.
- `SourceDestCheck=false`.
- `routerd-cloud-aws`: `stopped`.
- `aws-cloud-client`: `stopped`.

Cost state:

- EC2 compute stopped.
- Existing EIP/disk/NIC/VPC lab resources remain as the reusable SAM lab state.

## Notes

- A code bug was found and fixed locally during the run: `PluginSpec` schema and executor resolver supported `execute.providerAction`, but `pkg/config/validate_plugin.go` still rejected it.
- The forwarding action also needed `target.address=10.88.60.9` so `ProviderActionPolicy.allowedCIDRs` could gate the action without weakening policy.
