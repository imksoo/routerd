# AWS Azure OCI PVE SAM Full-Mesh Live Merge Gate

Date: 2026-06-29

Branch/build: `codex/dynamic-rr-leaf-enrollment`, commit `4bfdeeb1`.

Result: PASS.

This records the live merge gate for a four-site SAM topology spanning AWS,
Azure, OCI, and PVE. "Full-mesh" here means all AWS/Azure/OCI/PVE
client-bearing leaf sites can reach each other pairwise through the SAM
fabric; the tested topology uses an Azure RR/hub and does not require direct
router-to-router adjacency for every site pair. Raw logs are kept outside the
repository; this file records stable paths, checksum, and the key outcome.
Secret material is excluded from the evidence tarball.

## Topology

- RR: Azure `routerd-fullmesh-rr`, public `20.89.58.147`, private `10.82.10.5`.
- AWS site:
  - `aws-leaf`: public `13.231.195.59`, private `10.81.10.10`.
  - `aws-client`: public `18.182.63.83`, private `10.81.10.20`.
- Azure site:
  - `azure-leaf`: public `13.78.11.131`, private `10.82.10.10`.
  - `azure-client`: public `20.222.23.85`, private `10.82.10.20`.
- OCI site:
  - `oci-leaf`: public `161.33.143.93`, private `10.83.10.10`.
  - `oci-client`: public `168.110.63.34`, private `10.83.10.20`.
- PVE site:
  - `pve-leaf`: management `192.168.1.43`, site gateway `10.99.9.1`.
  - `pve-client`: management `192.168.1.47`, site `10.99.9.10`.

Transport tested: `ipip` with `encryption: wireguard` over public/NAT-reachable
underlay. PVE is behind NAT, so WireGuard was used as the shared practical
transport for this merge gate.

## Assertions

- RR base config contained zero static `SAMEnrollmentClaim` resources and zero
  per-leaf `BGPPeer` resources.
- All leaf configs were saved before deployment under the run directory
  `configs/`, with a node-by-node explanation in evidence.
- `SAMEnrollmentClient` was `Ready` on all four leaves.
- `BGPDynamicPeer/fullmesh-leaves` discovered four dynamic peers, all
  `ESTABLISHED`.
- RR accepted four routes and rejected zero:
  - `10.81.10.20/32`
  - `10.82.10.20/32`
  - `10.83.10.20/32`
  - `10.99.9.10/32`
- Each leaf established BGP to the RR and installed all four client /32 routes.
- All 12 directed client-to-client checks passed with ping and SSH:
  - AWS -> Azure, Azure -> AWS
  - AWS -> OCI, OCI -> AWS
  - AWS -> PVE, PVE -> AWS
  - Azure -> OCI, OCI -> Azure
  - Azure -> PVE, PVE -> Azure
  - OCI -> PVE, PVE -> OCI

## Provider Routing Evidence

- AWS `ap-northeast-1`:
  - source/destination check disabled on `aws-leaf`.
  - route table sends remote client /32s to the `aws-leaf` ENI.
- Azure `japaneast`:
  - NIC IP forwarding enabled on RR and `azure-leaf`.
  - UDR sends remote client /32s to `10.82.10.10`.
- OCI `ap-tokyo-1`:
  - skip source/destination check enabled on the `oci-leaf` VNIC.
  - route table sends remote client /32s to the `oci-leaf` primary private IP
    target.
  - Oracle-provided Ubuntu image had a default FORWARD reject; the live test
    added an explicit FORWARD accept on `oci-leaf`.
- PVE:
  - `pve-client` has explicit in-guest static routes for AWS/Azure/OCI client
    /32s via `10.99.9.1 dev ens19`.

## Evidence Bundle

- Raw run directory: `/tmp/routerd-fullmesh-20260629T092330Z`.
- Evidence tarball without secrets:
  `/tmp/routerd-fullmesh-20260629T092330Z-evidence-no-secrets.tar.gz`.
- Evidence tarball SHA256:
  `7eda955889f642951466fcebe4322404fde006595a6f4370444db505348a1027`.
- Key excerpts:
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/node-by-node-config-explanation.md`
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/pre-deploy-config-boundary-check.txt`
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/convergence-status-2.txt`
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/fullmesh-client-matrix-final.txt`
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/final-routerd-status.txt`
  - `/tmp/routerd-fullmesh-20260629T092330Z/evidence/provider-routing-summary.txt`

## Cleanup Status

Live AWS, Azure, OCI, and PVE resources were left running for immediate
post-merge review. They should be destroyed after review if no further
debugging is needed. A short direct-adjacency experiment was started after this
run while interpreting a stricter review note; after the corrected acceptance
criteria were clarified, that experiment was stopped and the hub-oriented
`routerd-fullmesh.service` was restored on RR and leaves. The PASS evidence
above is from `/tmp/routerd-fullmesh-20260629T092330Z`.

## HTTP Control API Note

`routerd serve --http-listen` exposes the mutation/control API over TCP. It is
for controlled management or private underlay networks only, and must be bound
only to protected addresses or shielded by equivalent network policy. In this
test, Azure NSG rules restricted SSH and the enrollment API to lab and leaf
source addresses; WireGuard UDP was opened for public/NAT reachability.
