# CloudEdge Phase 5.1 OCI Provider Executor Smoke

Result: PASS

Date: 2026-05-31 UTC  
Branch/build: `phase5-oci-azure-executors` / `routerd v20260528.2308 (67d96103)`  
Evidence bundle: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T005414Z-phase5-oci-live-67d96103`

## Scope

- Provider mutation scope: OCI only.
- Tenancy/region: `ocid1.tenancy.oc1..aaaaaaaaby2raoa2kzgywrsz6ofjk4eks6uwtpczgtqxulach3xgksfx52qq` / `ap-tokyo-1`.
- Reused routerd-only SAM lab: `Project=routerd-cloudedge-sam-oci-pve`.
- Target router instance: `routerd-cloud-oci` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2sucs3kor7u77ki2cg7zf3xlgmubj5utwfqeejmm7crq`.
- Target client instance: `oci-cloud-client` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2biuwl7yyjglwn6aompawzlfmkohpbrqceuijiuf7dva`.
- Target VNIC: `ocid1.vnic.oc1.ap-tokyo-1.abxhiljrzn6c2b4hs2jljbs4cmbshywzr7ldugepftjdrvm77nlvcvbdzzkq`.
- Captured address: `10.77.60.9`.

## Rebaseline

Before mutation, the existing SAM lab was reset to a fresh provider baseline:

- `10.77.60.9` secondary private IP removed from the router VNIC.
- `skipSourceDestCheck=false` restored on the VNIC.
- Post-reset evidence: `oci-router-vnic-post-reset.json`, `oci-router-private-ips-post-reset.json`, `retry-reset-summary.tsv`.

## Instance Principal Gate

`routerd-cloud-oci` received an OCI dynamic group and policy for the executor.

- Dynamic group: `routerd_phase5_oci_executor`.
- Initial least-privilege policy was insufficient for `private-ip create` and returned `NotAuthorizedOrNotFound`.
- Progress-first fix: policy broadened to `manage virtual-network-family in tenancy` for this routerd lab dynamic group.

Instance principal preflight passed from the router:

- `oci network vnic get` could read the target VNIC.
- `oci network private-ip list` could read the target VNIC private IPs.

## Executor Run

`oci-provider-executor` was built and installed on `routerd-cloud-oci`.

Two retry2 action journal entries were imported, approved, dry-run, and executed:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.77.60.9 to <target VNIC>`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `set skipSourceDestCheck=true on <target VNIC> (prior=false)`
  - Observed journal fact: `priorSkipSourceDestCheck=false`

OCI validation after mutation:

- VNIC primary: `10.77.60.4`
- VNIC secondary: `10.77.60.9`
- `skipSourceDestCheck=true`

## Dataplane Validation

Cloud side:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`.
- Delivery route: `10.77.60.9 dev wg-hybrid metric 120`.
- Local OS address absence: `10.77.60.9/32 absent from local interfaces`.
- MSS clamp: `routerd_mss covers ens3 -> wg-hybrid`.

On-prem side:

- router06 `routerctl doctor hybrid`: `overall=pass`, `pass=15`, `warn=0`, `fail=0`, `skip=1`.
- Proxy ARP claim for cloud client `10.77.60.7` remained healthy.

Client connectivity:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`.
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`.
- cloud -> onprem SSH preserved source:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH preserved source:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- Default gateways unchanged:
  - cloud-client: `default via 10.77.60.1 dev ens3`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: absent by SSH source preservation.

## Rollback And Restore

Rollback was exercised through `routerctl action rollback`:

- action 4 `ensure-forwarding-enabled`: `rolledBack`, restored `skipSourceDestCheck=false`.
- action 3 `assign-secondary-ip`: `rolledBack`, unassigned `10.77.60.9`.

One fixable lab issue was found during rollback: OCI `private-ip delete` could exceed the Plugin's original `30s` timeout. The lab Plugin timeout was widened to `120s`, after which action 3 rollback completed and the journal recorded `rolledBack`.

Final teardown used option B: restore the previous SAM lab state.

- `10.77.60.9` secondary private IP present again.
- `skipSourceDestCheck=true`.
- `routerd-cloud-oci`: `STOPPED`.
- `oci-cloud-client`: `STOPPED`.

Cost state:

- OCI compute stopped.
- Existing public IP, boot volumes, VNIC, subnet, VCN, and policies remain as the reusable SAM lab state.

## Notes

- OCI Ubuntu image had terminal iptables reject rules. The same lab firewall bootstrap used in the OCI SAM smoke was applied before dataplane validation.
- The first executor attempt found the instance principal policy was too narrow for private IP creation. After broadening the lab dynamic-group policy, the retry2 action pair passed.
- The first normal-user rollback attempt was denied by the action DB file permissions. Rollback was then run with `sudo routerctl`, matching the action DB ownership.
