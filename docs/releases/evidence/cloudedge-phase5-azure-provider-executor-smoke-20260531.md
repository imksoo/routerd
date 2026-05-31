# CloudEdge Phase 5.1 Azure Provider Executor Smoke

Result: PASS

Date: 2026-05-31 UTC  
Branch/build: `phase5-oci-azure-executors` / `routerd v20260528.2308 (c51ba0ca)`  
Evidence bundle: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T013055Z-phase5-azure-live-c51ba0ca`

## Scope

- Provider mutation scope: Azure only.
- Tenant/subscription/region: `53a7de65-6b1f-4878-a424-acad5e25db4b` / `26412fa4-cd3a-4128-9794-72ee01876d84` / `japaneast`.
- Reused routerd-only SAM lab: resource group `cloudedge-lab`.
- Target router VM: `routerd-cloud`, private `10.77.60.4`, public `20.46.113.237`.
- Target client VM: `cloud-client`, private `10.77.60.7`.
- Target NIC: `ce-router-nic`.
- Captured address: `10.77.60.9`.

## Rebaseline

Before mutation, the existing Azure SAM lab was reset to a fresh provider baseline:

- Secondary ipconfig `ipconfig-onprem-capture` / `10.77.60.9` removed from `ce-router-nic`.
- `enableIPForwarding=false` restored on `ce-router-nic`.
- Post-reset evidence: `azure-router-nic-post-reset.json`, `post-reset-nic-summary.tsv`.

Post-reset state:

- `ce-router-nic`: `ipForwarding=false`.
- IP configs: primary `10.77.60.4` only.

## Managed Identity Gate

`routerd-cloud` received a system-assigned managed identity:

- Principal ID: `4b9423bc-01e3-4244-a898-b911f140cb6f`.
- Azure CLI was installed on `routerd-cloud` for the executor.
- Managed identity preflight passed from the router:
  - `az login --identity --allow-no-subscriptions`
  - `az network nic show --ids <ce-router-nic>`

The initial NIC-scope Network Contributor role was insufficient for `ip-config create`
because Azure also required linked NSG `join/action` authorization. Progress-first
fixes added Network Contributor at the lab resource group and NSG scopes. After that,
executor mutations succeeded.

## Executor Run

`azure-provider-executor` was built and installed on `routerd-cloud`.

The router config included:

- `ProviderActionPolicy/azure-live-mutation`
- `Plugin/azure-executor`
- Plugin timeout `120s`
- `AZURE_CONFIG_DIR=/var/lib/routerd/azure`

Action execution:

- `ensure-forwarding-enabled`
  - Action ID: `4`
  - Result: `succeeded`
  - Observed journal fact: `priorIpForwarding=false`
  - Result message: `set ipForwarding=true`
- `assign-secondary-ip`
  - Action ID: `7`
  - Result: `succeeded`
  - Result message: `assigned 10.77.60.9 to ce-router-nic (ip-config ipconfig-onprem-capture)`

Azure validation after mutation:

- `ce-router-nic`: `ipForwarding=true`.
- IP configs: `10.77.60.4`, `10.77.60.9`.
- Evidence: `azure-router-nic-after-mutation.json`, `azure-router-nic-after-mutation-summary.tsv`.

## Dataplane Validation

Cloud side:

- `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`.
- Delivery route: `10.77.60.9 dev wg-hybrid metric 120`.
- Local OS address absence: `10.77.60.9/32 absent from local interfaces`.
- MSS clamp: `routerd_mss covers eth0 -> wg-hybrid`.

On-prem side:

- router06 `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`.
- Proxy ARP claim for cloud client `10.77.60.7` remained healthy.
- MSS clamp: `routerd_mss covers ens21 -> wg-hybrid`.

Client connectivity:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`.
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`.
- cloud -> onprem SSH preserved source:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH preserved source:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- Default gateways unchanged:
  - cloud-client: `default via 10.77.60.1 dev eth0`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: absent by SSH source preservation.

## Rollback And Restore

Rollback was exercised through `routerctl action rollback`:

- action 7 `assign-secondary-ip`: `rolledBack`, unassigned `ipconfig-onprem-capture`.
- action 4 `ensure-forwarding-enabled`: `rolledBack`, restored `ipForwarding=false`.

One fixable lab issue was found during rollback: after a router config reapply, the
Plugin environment no longer exposed `AZURE_CONFIG_DIR`, so Azure CLI reported
`Please run 'az login'`. The config was corrected, managed-identity login was
re-created under `/var/lib/routerd/azure`, and rollback then passed.

Final teardown used option B: restore the previous Azure SAM lab state.

- `10.77.60.9` secondary ipconfig present again.
- `ipForwarding=true`.
- `routerd-cloud`: `VM deallocated`.
- `cloud-client`: `VM deallocated`.

Cost state:

- Azure compute deallocated.
- Existing public IP, NICs, disks, VNet, NSG, and managed-identity/role assignments remain as the reusable SAM lab state.

## Notes

- `capture.interface: eth0` was added to the cloud `RemoteAddressClaim` lab config so the new MSS/PMTU doctor check can prove `eth0 -> wg-hybrid` coverage.
- Initial action attempts failed while the managed identity role scope was too narrow. Final successful actions were IDs 4 and 7.
- The `rtk` wrapper truncates long Azure resource IDs in command substitution; commands that needed exact resource IDs used raw `az` inside `rtk bash -lc`.
