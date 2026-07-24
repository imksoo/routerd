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
was not retried. Exact CI and retained-topology redeploy/revalidation remain
required.

Revised evidence-dependent stages: fix and exact CI 13:30–15:30Z; retained
topology redeploy and all full-run scenarios 15:30–19:00Z; destroy and clean
inventory audit 19:00–20:00Z if no second production failure. A separate
mixed Linux/FreeBSD interoperability qualification follows the Linux-only
cleanup.
