# CloudEdge SAM Azure x PVE Smoke Evidence

Date: 2026-05-29

Branch/build: `cloudedge-mvp`, `routerd v20260528.2308 (439ec316)`

Result: PASS

Evidence bundle:
`/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260529T161157Z-439ec316-clean`

## Topology

- Cloud client: `10.77.60.7/24`
- On-prem client: `10.77.60.9/24`
- Cloud router primary: `10.77.60.4/24`
- Cloud router Azure NIC secondary capture address: `10.77.60.9`
- On-prem router: router06, `10.77.60.1/24` on `ens21`
- Overlay: `wg-hybrid`, `169.254.110.1/32` to `169.254.110.2/32`

## Azure Capture

- `ce-router-nic` had IP forwarding enabled.
- Primary private IP was `10.77.60.4`.
- Secondary private IP was `10.77.60.9`.
- `routerd-cloud` guest OS did not retain `10.77.60.9` as a local interface address after routerd reconciliation.
- `10.77.60.9/32` was delivered over `wg-hybrid`.

## Cloud Side

- `RemoteAddressClaim/onprem-client-10-77-60-9` was `Ready`.
- Capture type was `provider-secondary-ip`.
- `captureDeassignedOSAddress.enforced=true`.
- Delivery route was installed: `10.77.60.9 dev wg-hybrid scope link metric 120`.
- `ip route get 10.77.60.9` selected `wg-hybrid`.
- `10.77.60.9/32` was absent from local interfaces.
- `routerctl doctor hybrid` was `overall=pass`, `fail=0`.

## On-Prem Side

- `RemoteAddressClaim/cloud-client-10-77-60-7` was `Ready`.
- Capture type was `proxy-arp` on `ens21`.
- Proxy neighbor existed: `10.77.60.7 proxy`.
- Delivery route was installed: `10.77.60.7 dev wg-hybrid scope link metric 120`.
- `ens21.proxy_arp=1`.
- `routerctl doctor hybrid` was `overall=pass`, `fail=0`.

## Connectivity

- Cloud client to on-prem client ping: 3/3 received, 0% loss.
- On-prem client to cloud client ping: 3/3 received, 0% loss.
- Cloud client to on-prem client SSH succeeded with source preserved:
  `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`.
- On-prem client to cloud client SSH succeeded with source preserved:
  `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`.
- NAT was not observed.
- Client default gateways were unchanged.

## Clean-Run Hardening Checks

- Azure Ubuntu reintroduced `10.77.60.9/24` on `eth0` before routerd startup.
- routerd `439ec316` de-assigned that address without a manual `ip addr del` workaround.
- routerd applied WireGuard without the previous manual `/dev/stdin` workaround.
- Evidence was captured before Azure VM deallocation.
- Azure VMs were deallocated after evidence capture; the resource group was not torn down.

## Known Notes

- The FORWARD policy doctor check was skipped where the `routerd_filter` table was unavailable; dataplane smoke still passed.
- router06 global status remained `Pending`, but `doctor hybrid` passed and the SAM dataplane path was healthy.
- `captureDeassignedOSAddress.deassigned=false` in steady state means there was no address to remove during that reconcile; `enforced=true` plus the local-address doctor pass is the relevant assertion.
