# CloudEdge SAM full-mesh qualification — 2026-07-24

This note records the final, user-authorized qualification gate for the
`v20260724.1159` release. It is both a runbook checkpoint and an index to the
retained live-run evidence; the complete qualification is still in progress.

## Scope

Run `tests/e2e/cloudedge/scripts/sam-full-validation.sh` against a fresh
OpenTofu environment with `fabric.topology_scale=full`:

- AWS route reflectors and leaf A/B;
- Azure, OCI, and Proxmox VE leaf A/B;
- the provider/on-prem pseudo-clients required by the full topology.

The required artifact is the published
`routerd-v20260724.1159-linux-amd64.tar.gz`. The runner records real packet
reachability, convergence, `routerctl` SAM status/doctor, provider state,
captures, and cleanup evidence. Historical OpenTofu state or old outputs are
not qualification evidence.

## Preconditions recorded on 2026-07-24

- The runner and `sam-preflight.sh` are checked into this repository.
- The audited infrastructure source is staged into a fresh, state-free work
  directory. `tofu fmt`, `tofu init -backend=false`, and `tofu validate` pass.
- The full source is currently lab infrastructure rather than a checked-in
  repository environment. This is tracked as lab-tooling debt; it does not
  authorize reuse of archived state, credentials, resources, or outputs.
- AWS must use the authenticated `routerd-labcodex` profile. Azure is enabled.
  OCI must use a current authenticated profile and verify the target
  compartment before any plan or apply. PVE inputs and credentials must be
  freshly verified and supplied outside version control.

## Measured staged ETA

After fresh inputs are verified: clean plan 30–60 minutes; provision/deploy
1–2 hours; baseline plus all failover/rejoin scenarios and final load-balance
1–2 hours; cleanup/evidence 30–60 minutes. Expected total is 3–5 hours. A
newly proven production or provider defect may extend this to one day.

## Live run checkpoint

Fresh run `samrel-202607241231` created 18 Linux/amd64 nodes: ten routers
(AWS RR A/B and AWS/Azure/OCI/PVE leaf A/B) and eight pseudo-clients. The
published release deployed successfully after provider inventory, SSH,
dataplane-address, and config-validation preflight passed.

Official validation attempt 2 is retained at
`/tmp/routerd-sam-full-20260724.fDbT39/full-validation-attempt2`. Baseline
stopped at `initial-dataplane TIMEOUT` after 303 seconds; no destroy ran.
PVE leaf and client `eth1` interfaces and neighbor entries were healthy, but
both `routerd-arp-observer` modes were terminated because their legacy launch
path supplied an empty supervised-daemon owner token. Consequently on-prem
client `/32`s never entered the owner table or FIB. Shared production defect
[#972](https://github.com/imksoo/routerd/issues/972) tracks the fix.

The fix integrates ARP observers into the existing supervised-daemon token,
marker-recovery, foreign-process refusal, restart, and cleanup lifecycle.
Focused regression, unfiltered `go test ./...`, FreeBSD amd64 cross, schema,
website-schema, and diff checks pass locally. A bounded Claude review returned
`Execution error` at its 180-second limit and supplied no usable finding; it
was not retried. The initial implementation was completed by
`aa7f1ea44f944c8246fbc3fda7a4ede2eb916421`, exact CI
[30098194329](https://github.com/imksoo/routerd/actions/runs/30098194329)
passed, and attempt 3 exposed one remaining CLI contract: the observer binary
did not accept the internal `--supervisor-owner` argument. Commit
`1eec779a785bc45bf09c510fe2e3da653737081b` adds that parser contract and
regression; exact CI
[30099558747](https://github.com/imksoo/routerd/actions/runs/30099558747)
is terminal success.

Attempt 4 uses exact artifact
`/tmp/routerd-sam-full-20260724.fDbT39/artifacts/routerd-1eec779a-linux-amd64.tar.gz`
(`sha256=e3c851b5b14e57ea05b26509bbb9f882566c21e0e9bd042c97a77c3ea223cd90`).
Both PVE leaves run two token-bearing observer children and emit real
`ARPObserved`/`ARPProbeHit` events. The initial dataplane phase is PASS:
directed client matrix 56/56, cloud-ingress 42/42, provider gate PASS, and
legacy RPC/FTP/NFS/CIFS matrix 56/56. Baseline performance measurement is in
progress.

The original full wrapper repeated legacy and throughput probes before,
during, and after each of ten failovers, which measures the same property
roughly thirty times and extends the run toward fifteen hours without adding
owner-transfer validity. The ordered default suite now keeps full
legacy/performance at baseline and final load-balance while retaining all
directed client/cloud-ingress checks, provider convergence, in-flight transfer
observation, owner tables, and rejoin checks at every failover. Resume accepts
only a contiguous ordered PASS prefix with retained evidence. A standalone
`--scenario` remains exhaustive. Unfiltered `go test ./...`, FreeBSD amd64
cross, schemas, shell syntax/ShellCheck, default Linux generator behavior, and
FreeBSD CARP generator/render checks pass.

Separate mixed Linux/FreeBSD qualification is tracked by
[#973](https://github.com/imksoo/routerd/issues/973). It uses real FreeBSD
14.3 amd64 full clones, console/read-only-ISO bootstrap because VM115 QGA is
not running, and CARP master/backup ownership gating. It follows Linux-only
terminal cleanup.

Exact CI
[30102383983](https://github.com/imksoo/routerd/actions/runs/30102383983)
is terminal success at
`a9f1a60503dcd7be83be0493bcdca0051e87f0e4`. While baseline performance
continued, mixed qualification preparation created three stopped full clones
from the stopped, unchanged VM115 source on `pve06`: VM970/971 are FreeBSD
leaf A/B and VM972 is the FreeBSD pseudo-client. They are tagged
`routerd-owned;issue-973`, have isolated `vmbr404` capture and `vmbr0`
management NICs, and are not started or counted as acceptance evidence before
the Linux-only qualification reaches its terminal cleanup.
