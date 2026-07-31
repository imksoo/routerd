# Tracked release-QA labs

This directory is the canonical, reviewed implementation of the routerd
release certification lifecycle. Release procedures must not reference a
script from an untracked directory, a local-only nested repository, or an
uncommitted revision.

The code is separate from routerd runtime behavior. Proxmox automation here is
limited to the explicitly reviewed release-QA design in issue #1049.

## Safety model

The only lifecycle entry point is:

```text
drivers/start-supervised-release-qa.sh CONTRACT
```

It must run under the tracked boot-enabled supervisor unit on the remote
execution host named by the contract. Never run the paid lifecycle from a
shared development host, a Codex process, or an interactive transport.

The durable supervisor persists this state machine:

```text
PRECHECK -> MUTATING -> STOPPING -> CLEANING -> VERIFYING_ZERO -> DONE|FAILED
```

The absolute UTC deadline is written before precheck and is not extended by a
supervisor restart. Restart, disconnect, stale heartbeat, timeout, SIGINT,
SIGTERM, or mutation failure all enter STOPPING. The supervisor terminates and
waits for the entire mutation process group before cleanup. A per-run flock
allows only one cleanup owner. Only a successful mutation followed by cleanup
and exhaustive zero inventory reaches DONE; all other cleanly recovered paths
remain FAILED.

## Required run-time files

Each run uses one complete, clean canonical checkout at `<run>/repo`; drivers
execute directly from `<run>/repo/tools/release-qa-labs`. Mutable files live
under `<run>/runtime`, never in a flattened copy of the QA directory. These
runtime inputs must be mode 0600:

- `runtime/contract.json`
- `runtime/run.env.json`
- `runtime/terraform.tfvars`

Credentials, artifacts, provider mirrors, state, plans and evidence are never
committed. `tofu.rc` expects the reviewed provider mirror at
`/var/lib/routerd-release-qa/provider-mirror` on the execution host.

The contract must bind:

- exact untagged RC commit, frozen main parent, canonical origin and artifact
  SHA-256;
- SHA-256 identities for every release and QA script used by the run;
- the canonical tracked QA commit and origin;
- the approved remote execution host and provider mirror versions;
- TTL no greater than 75 minutes and heartbeat-stale less than TTL;
- exact regions, instance types, provider counts and a cost ceiling no greater
  than USD 1.00.

`qa_guard.py` rejects dirty/local-only provenance and validates saved OpenTofu
plans against a closed resource-type/count/type allowlist before apply.

## Precheck and cleanup

Before any mutation, the remote-host precheck verifies DNS, TCP, TLS,
authenticated read-only provider/PVE access and the provider mirror. It then
requires exhaustive zero inventory.

Inventory queries are fail-closed and cover:

- OpenTofu state;
- every AWS resource with the run tag (paginated Resource Groups Tagging API);
- the Azure resource group and every contained resource;
- every OCI resource with the run tag (paginated resource search);
- exact PVE VMIDs and the exact capture bridge.

Missing, duplicate, partial, unknown or failed queries are not zero. The same
inventory gate runs after unconditional destroy.

## Offline validation

No test contacts or mutates a provider, PVE, host network, systemd, or routerd.

```sh
python3 -m unittest discover -s tools/release-qa-labs/tests -v
shellcheck -x tools/release-qa-labs/drivers/*.sh
```

The tests cover success, mutation failure, cleanup/inventory failure, timeout,
stale heartbeat, SIGINT, SIGTERM, supervisor restart, quiesce ordering,
provenance/artifact/blob tamper, plan ceilings, incomplete inventory, remote
egress failure, executable modes and the boot-enabled supervisor configuration.

Offline tests do **not** prove a real execution-host reboot recovery, provider
billing totals, or live provider/PVE API behavior. Before any paid run, staging
must prove that the installed supervisor restarts after a service-manager
restart, survives an SSH/client disconnect, resumes from durable state, and
reaches exhaustive zero inventory. Do not reboot a shared or production host
for this test; use an authorized disposable execution host or an equivalent
service-restart exercise. The USD ceiling is a conservative planned-admission
estimate, not a hard stop on recovery and not evidence of the final bill. Once
mutation starts, cleanup and authoritative zero-inventory verification continue
with bounded individual attempts and service-manager backoff for as long as
necessary; stopping recovery at the admission estimate could leave a much
larger leak. Review the final bill only after exhaustive zero cleanup.
