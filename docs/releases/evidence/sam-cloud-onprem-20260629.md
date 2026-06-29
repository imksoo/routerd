# Cloud plus On-Prem SAM Live Evidence

Date: 2026-06-29

Branch/build: `codex/dynamic-rr-leaf-enrollment`, commit `63f0cb53`.

Result: PASS.

This records the full cloud plus on-prem SAM redundancy live test. Raw logs are
kept outside the repository; this file records stable paths, checksum, and the
key outcome. Secret material is intentionally excluded from the evidence
tarball.

## Topology

- Cloud provider: Azure.
- Azure resource group: `rg-routerd-cloudonprem-20260629T050742Z`.
- Azure region: `japaneast`.
- Cloud RR set:
  - `routerd-cloudonprem-rr-1`: public `20.222.196.62`, private `10.60.0.101`.
  - `routerd-cloudonprem-rr-2`: public `20.63.159.110`, private `10.60.0.102`.
- PVE/on-prem leaves:
  - `pve-leaf-a`: management `192.168.1.43`, site gateway `10.99.9.1/24`.
  - `pve-leaf-c`: management `192.168.1.41`, site gateway `10.99.8.1/24`.
- PVE/on-prem clients:
  - `client-999`: management `192.168.1.47`, site `10.99.9.10/24`.
  - `client-998`: management `192.168.1.46`, site `10.99.8.10/24`.
- Transport tested: `ipip` with `encryption: wireguard` over public underlay.

The public-underlay cloud/on-prem path used WireGuard because the PVE lab leaves
are behind NAT. The preceding PVE-only test covered private-underlay
`encryption: none` FOU.

## Assertions

- RR base configs contained no static `SAMEnrollmentClaim` resources and no
  per-leaf `BGPPeer` resources.
- `pve-leaf-a` and `pve-leaf-c` submitted enrollment claims at runtime through
  `SAMEnrollmentClient`.
- Both cloud RRs fetched/accepted the dynamic admission state and discovered the
  two leaves through `BGPDynamicPeer/cloud-leaves`.
- Both cloud RRs established BGP sessions with both leaves and accepted the two
  authorized site /32 routes.
- Both leaves fetched the authorized `SAMRRSet/cloud-rrs`, established
  WireGuard and BGP to both cloud RRs, and installed the remote client /32.
- `client-999` and `client-998` passed bidirectional ping and SSH across the
  cloud plus on-prem SAM fabric.
- Azure NSG allowed SSH, enrollment API, and WireGuard only from
  `210.171.174.15/32` during the test.

## Evidence Bundle

- Raw run directory: `/tmp/routerd-cloudonprem-20260629T050742Z`.
- Evidence tarball without secrets:
  `/tmp/routerd-cloudonprem-20260629T050742Z-evidence-no-secrets.tar.gz`.
- Evidence tarball SHA256:
  `4698a210244a0c00754ab49025474db6ce77c3aab8ab6a5d074e86daa97e8a12`.
- Key excerpts:
  - `/tmp/routerd-cloudonprem-20260629T050742Z/evidence/static-config-boundary-check.txt`
  - `/tmp/routerd-cloudonprem-20260629T050742Z/evidence/final-routerd-status.txt`
  - `/tmp/routerd-cloudonprem-20260629T050742Z/evidence/client-ping-ssh-final.txt`
  - `/tmp/routerd-cloudonprem-20260629T050742Z/evidence/azure-nsg-rules-final.txt`
  - `/tmp/routerd-cloudonprem-20260629T050742Z/evidence/azure-resource-list-final.txt`

## Cleanup Status

The Azure resource group and PVE test services were left running for immediate
post-test inspection. Delete the Azure resource group after review if no more
live debugging is needed:

```sh
az group delete -n rg-routerd-cloudonprem-20260629T050742Z --yes --no-wait
```

## HTTP Control API Note

`routerd serve --http-listen` exposes the mutation/control API over TCP. It is
for controlled management or private underlay networks only, and must be bound
only to protected addresses or shielded by equivalent network policy. It is not
an Internet-safe listener.
