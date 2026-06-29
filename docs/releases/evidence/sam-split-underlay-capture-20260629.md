# Split Underlay and Capture Subnet SAM Live Evidence

Date: 2026-06-29

Branch/build: `codex/dynamic-rr-leaf-enrollment`, commit `3c2cc657`.

Result: PASS.

This records a live Azure plus PVE test for a split topology where SAM underlay
traffic exits a public/underlay side while workload capture happens on a
separate private subnet. Raw logs are kept outside the repository; this file
records stable paths, checksum, and key outcome. Secret material is excluded
from the evidence tarball.

## Topology

- Cloud provider: Azure, chosen because authentication was already verified and
  Azure supports the required two-subnet model with NIC IP forwarding plus UDR.
- Azure resource group: `rg-routerd-split-underlay-20260629T072416Z`.
- Azure region: `japaneast`.
- Subnets:
  - SAM underlay subnet: `10.70.0.0/24`.
  - Capture/workload subnet: `10.70.10.0/24`.
- Cloud routerd VM `routerd-split-router`:
  - `eth0` SAM underlay: `10.70.0.10/24`, public `52.253.113.16`.
  - `eth1` capture side: `10.70.10.4/24`.
  - `wg-split`: `10.31.130.101/32`.
- Cloud private workload VM `routerd-split-client`:
  - `eth0`: `10.70.10.10/24`.
  - No public IP.
- PVE/on-prem leaf `pve-leaf-a`:
  - management/private WAN: `192.168.1.43/24`.
  - site LAN gateway: `10.99.9.1/24`.
  - `wg-split`: `10.31.130.11/32`.
- PVE/on-prem client `client-999`:
  - site address: `10.99.9.10/24`.

The test used `ipip` with `encryption: wireguard` because the PVE leaf is behind
NAT and the public IP path is standing in for future dedicated-line
reachability. The capture subnet remained separate from the SAM underlay subnet.

## Assertions

- Cloud routerd used separate NICs for underlay and capture:
  - `eth0` carried the public SAM/WireGuard endpoint.
  - `eth1` connected to the private workload/capture subnet.
- Azure NIC IP forwarding was enabled on both router NICs.
- Azure capture-subnet UDR sent `10.99.9.10/32` to next hop `10.70.10.4`.
- Cloud routerd accepted `pve-leaf-a` through runtime SAM enrollment, not a
  static per-leaf `BGPPeer`.
- `BGPDynamicPeer/split-leaves` discovered one dynamic peer,
  `pve-leaf-a`, in `ESTABLISHED` state.
- Cloud routerd installed `10.99.9.10/32` via the SAM tunnel.
- PVE leaf installed `10.70.10.10/32` via the SAM tunnel.
- Path proof showed:
  - `ip route get 210.171.174.15` on cloud router used `eth0`.
  - `ip route get 10.99.9.10 from 10.70.10.10 iif eth1` used the SAM tunnel.
  - `ip route get 52.253.113.16` on PVE leaf used `ens18`.
- `routerd-split-client` and `client-999` passed bidirectional ping and SSH.

## Provider Notes

- Azure: enable NIC IP forwarding on router NICs and use a UDR from the capture
  subnet to the router capture-side private IP.
- AWS equivalent: disable source/destination check on the router EC2 instance or
  ENI, then route the capture subnet's remote prefixes to the instance or ENI.
- OCI equivalent: enable skip source/destination check on the router VNIC and
  route the capture subnet's remote prefixes to the router private IP target
  model used by the compartment/network setup.

## Evidence Bundle

- Raw run directory: `/tmp/routerd-split-underlay-20260629T072416Z`.
- Evidence tarball without secrets:
  `/tmp/routerd-split-underlay-20260629T072416Z-evidence-no-secrets.tar.gz`.
- Evidence tarball SHA256:
  `b13e9e0efe644059708c8986e3ae265d95abe18d16fca4c1ccffe59e816299e4`.
- Key excerpts:
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/ip-plan.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/post-db-clean-routerd-status.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/split-underlay-path-proof.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/client-ping-ssh-final.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/final-routerd-status.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/azure-capture-udr-routes-final.txt`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/azure-router-underlay-nic-forwarding-final.json`
  - `/tmp/routerd-split-underlay-20260629T072416Z/evidence/azure-router-capture-nic-forwarding-final.json`

## Cleanup Status

Cleanup was verified after review in
`/tmp/routerd-cleanup-20260629T121511Z`. The Azure resource group was already
absent when cleanup ran, and PVE VM/ISO absence was verified from `pve07`.

## HTTP Control API Note

`routerd serve --http-listen` exposes the mutation/control API over TCP. It is
for controlled management or private underlay networks only, and must be bound
only to protected addresses or shielded by equivalent network policy. It is not
an Internet-safe listener. In this test, Azure NSG rules restricted the public
listener to the lab source address.
