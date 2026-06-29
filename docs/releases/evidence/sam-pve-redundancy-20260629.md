# PVE SAM Redundancy Live Evidence

Date: 2026-06-29

Branch/build: `codex/dynamic-rr-leaf-enrollment`, commit `63f0cb53`.

Result: PASS.

This records the PVE-only cloud-SAM redundancy live test that preceded the full
cloud plus on-prem test. Raw logs are intentionally kept outside the repository;
this file records stable paths, checksum, and the key outcome.

## Topology

- RR set: `routerd-samred-rr-1`, `routerd-samred-rr-2`.
- VLAN 999 site: `routerd-samred-leaf-a`, `routerd-samred-leaf-b`, `routerd-samred-client-999`.
- VLAN 998 site: `routerd-samred-leaf-c`, `routerd-samred-leaf-d`, `routerd-samred-client-998`.
- PVE host: `pve07`.
- Transport tested: FOU over private underlay, `encryption: none`.

## Assertions

- RR base configs contained no static `SAMEnrollmentClaim` resources and no
  per-leaf `BGPPeer` resources.
- Leaves submitted enrollment claims at runtime and fetched the authorized RRSet.
- Both RRs discovered four dynamic BGP peers via `BGPDynamicPeer/samred-leaves`.
- Each leaf established BGP to both RRs and installed remote /32 routes.
- `client-999` and `client-998` passed bidirectional ping and SSH across the
  dynamically enrolled SAM fabric.

## Evidence Bundle

- Raw run directory: `/tmp/routerd-samred-20260629T035652Z`.
- Preserved copy: `/tmp/routerd-samred-preserved-20260629T050450Z`.
- Tarball: `/tmp/routerd-samred-20260629T035652Z.tar.gz`.
- Tarball SHA256: `77277d94e9c1b097ff0e9b7158b1cdeed772b27300c3a7c58bc007db9c1c92f4`.
- Key excerpts:
  - `/tmp/routerd-samred-20260629T035652Z/evidence/client-ping-ssh-final.txt`
  - `/tmp/routerd-samred-20260629T035652Z/evidence/final-routerd-status.txt`

## HTTP Control API Note

`routerd serve --http-listen` exposes the mutation/control API over TCP. It is
for controlled management or private underlay networks only, and must be bound
only to protected addresses or shielded by equivalent network policy. It is not
an Internet-safe listener.
