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

Provider HTTPS egress uses the tracked unit
`routerd-release-qa-egress-proxy@RUN_ID.service` introduced by issue #1068.
It listens only on the run's explicit `http://127.0.0.1:<high-port>` endpoint
and resolves and connects upstream with IPv4 sockets only. It changes no host
DNS, route, interface, DHCP, or routerd setting. The proxy is started before
baseline inventory, remains available through post/failure inventory, and is
stopped only after final inventory. Its independent `RuntimeMaxSec` bounds it
when the operator disconnects; a port collision fails readiness instead of
falling back to an untracked or externally reachable proxy.

It must run under the tracked boot-enabled supervisor unit on the remote
execution host named by the contract. Never run the paid lifecycle from a
shared development host, a Codex process, or an interactive transport.

`execution.mode` is mandatory and is either `production` or
`staging-no-mutation`. Missing and unknown modes fail closed. Staging additionally
requires a `relqa-staging-...` run ID and the exact environment
`routerd-release-qa-staging`; production rejects that identity. The mode is read
only from the contract, copied into the pinned contract, and digest-bound in
durable state. It is not a launcher option.

The durable supervisor persists this state machine:

```text
PRECHECK -> MUTATING -> STOPPING -> CLEANING -> VERIFYING_ZERO -> DONE|FAILED
PRECHECK -> STAGING_ARMED -(unit restart)-> STOPPING -> CLEANING -> VERIFYING_ZERO -> STAGING_DONE|FAILED
```

The absolute UTC deadline is written before precheck and is not extended by a
supervisor restart. Restart, disconnect, stale heartbeat, timeout, SIGINT,
SIGTERM, or mutation failure all enter STOPPING. The supervisor terminates and
waits for the entire mutation process group before cleanup. A per-run flock
allows only one cleanup owner. Only a successful mutation followed by cleanup
and exhaustive zero inventory reaches DONE; all other cleanly recovered paths
remain FAILED.

Staging never constructs or executes the mutation subprocess. It deliberately
exits after persisting `STAGING_ARMED`, so the boot-enabled unit must restart from
pinned state before cleanup. Only exhaustive zero inventory reaches
`STAGING_DONE`, whose result records `paidQualification: not-run` and
`mutationExecuted: false`; it is not a release qualification PASS.
Deleted or modified mutable source inputs still exercise pinned cleanup recovery,
but are recorded as tamper and must terminate `FAILED` after zero inventory;
only an untampered restart can produce `STAGING_DONE`.

## Required run-time files

Each run uses one complete, clean canonical checkout at `<run>/repo`; drivers
execute directly from `<run>/repo/tools/release-qa-labs`. Mutable files live
under `<run>/runtime`, never in a flattened copy of the QA directory. These
runtime inputs must be mode 0600:

- `runtime/contract.json`
- `runtime/run.env.json`
- `runtime/terraform.tfvars`

`runtime/run.env.json` must point `azureAuthSource` at the exact
`runtime/secrets/azure-auth-source` directory.  Before the unit is started,
copy only the Azure CLI authentication/configuration files required by the
selected account into that directory, with mode 0700 on directories and 0600
on files.  Symlinks are rejected.  The unit mounts this source read-only; the
launcher digest-pins it and creates the writable CLI working copy at
`runtime/provider-state/azure`.  Every driver receives that working copy via
`AZURE_CONFIG_DIR`, so Azure command logs and token-cache updates cannot target
the service user's global home.

The same file must set `httpsProxy` to a run-unique unprivileged endpoint of
the exact form `http://127.0.0.1:<port>`. External proxy hosts, IPv6 loopback,
wildcard listeners, omitted endpoints, and privileged ports are rejected.

The service home and its `.aws`, `.azure`, and `.oci` trees remain read-only
under `ProtectSystem=strict`; do not add them to `ReadWritePaths`.  AWS and OCI
use their read-only credential/config sources, while PVE credentials remain
run-confined inputs.  Provider writes are permitted only below the exact run's
`runtime` directory.

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
- separate PVE identities: `pve.sshHost` is a DNS FQDN used by every PVE
  DNS/TCP/SSH consumer, while `pve.node` is the short Proxmox cluster node ID
  used by `pvesh /nodes/...` and Terraform `pve_node_name`. The FQDN's first
  label must exactly equal the cluster node ID; `pve_endpoint` must be the
  corresponding `https://<pve.sshHost>:8006/` API endpoint.

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

## Exact pre-paid staging procedure on chatty

Do this only with a fresh canonical run root whose exhaustive baseline inventory
is already zero. It performs authenticated read-only provider/PVE prechecks and
the normal cleanup/inventory commands, but it cannot invoke the mutation driver.
Do not alter host DNS, routes, network configuration, routerd services, or DHCP.

1. Copy the exact clean release checkout to
   `/var/lib/routerd-release-qa/$RUN_ID/repo`, and create the canonical mode-0600
   runtime inputs and mode-0700 secrets directory described above. Set all of
   `execution.mode: staging-no-mutation`, `runId: relqa-staging-...`, and
   `environment: routerd-release-qa-staging`. Keep every other contract field,
   provenance digest, credential, TTL and cleanup scope identical to the reviewed
   production contract.
2. Install the tracked `supervisor/routerd-release-qa-egress-proxy@.service`,
   `supervisor/routerd-release-qa-prepare@.service`, and
   `supervisor/routerd-release-qa@.service` unchanged and run
   `systemctl daemon-reload`. Before baseline inventory, run tracked
   `drivers/manage-egress-proxy.sh start "$RUN_ID"` as root. It selects or
   validates a collision-free run port, creates the status directory, starts
   the proxy, and waits for systemd readiness. Keep the existing reviewed
   sequence unchanged after that: authoritative baseline zero, first prepare,
   pinned metadata/digest capture, second prepare/restart and byte comparison,
   then main-unit start without blocking on the controller connection.
   Disconnect the SSH client immediately;
   the service does not depend on that session.
3. Reconnect and inspect
   `runtime/evidence/lifecycle/supervisor-state.json`. Its history must contain
   `PRECHECK -> STAGING_ARMED -> STOPPING -> CLEANING -> VERIFYING_ZERO ->
   STAGING_DONE`. A missing unit restart, failed cleanup, or nonzero/partial
   inventory cannot produce `STAGING_DONE`; the restart policy keeps recovery
   active for retryable cleanup/inventory failures.
4. Require the terminal result to equal
   `{"status":"pass","kind":"staging-no-mutation","paidQualification":"not-run","mutationExecuted":false}`,
   require `mutationCommandExecuted` to remain false, and archive the precheck,
   final-cleanup, final-inventory and lifecycle evidence. Disable the staging run
   unit after recording its terminal result. Never reuse this staging run root
   for production; create a fresh `production` contract and run ID after review.
5. Keep the proxy active through every success/failure final-inventory path.
   Every operator trap must run bounded final inventory and its zero guard first,
   then call `drivers/manage-egress-proxy.sh stop "$RUN_ID"`, and only after the
   unit is inactive and its exact port is not listening invoke the tracked
   finalizer. The finalizer removes only
   `/var/lib/routerd-release-qa-sealed/$RUN_ID`; the root-owned global sealed
   parent (root:root mode 0755) is retained. Run IDs are not secret; the security
   invariant is that the service user cannot create, remove, or rename its
   root-owned mode-0750 run leaf. The script fails closed before zero evidence.
