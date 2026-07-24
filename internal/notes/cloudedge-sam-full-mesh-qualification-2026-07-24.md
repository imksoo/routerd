# CloudEdge SAM full-mesh qualification — 2026-07-24

This note records the final, user-authorized qualification gate for the
`v20260724.1159` release. It is a runbook checkpoint, not evidence of a
completed cloud test.

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

