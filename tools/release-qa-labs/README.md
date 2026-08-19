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

The remote QA coordinator never starts `routerd`, including sandbox mode.
Generated-config validation copies the artifact and inputs over the
QGA-pinned guest SSH path and runs the sandbox process only as an unprivileged
process on a disposable PVE client. That sandbox has every network mutator,
DHCP/DHCPv6/RA emitter, BGP component, and service-manager action in dry-run
mode; it is not a system service and it is removed before deployment starts.

`execution.mode` is mandatory and is either `production` or
`staging-no-mutation`. Missing and unknown modes fail closed. Staging additionally
requires a `relqa-staging-...` run ID and the exact environment
`routerd-release-qa-staging`; production rejects that identity. The mode is read
only from the contract, copied into the pinned contract, and digest-bound in
durable state. It is not a launcher option.

`qualification.runScope` is mandatory and is also contract-pinned. Use
`pve-certification-only` for the one full PVE topology gate: it creates only
the staged PVE template and the six pinned PVE guests, performs the QGA and
guest-SSH attestation, then writes evidence that cloud provisioning and product
qualification were not run. The regular supervisor still performs cleanup,
all seven zero-inventory scopes, and token revocation. A successful lifecycle
in this scope is a PVE substrate result, not a release qualification. Use a
new, fresh `full-representative` run only after that gate passes; that scope is
the only one that may provision cloud resources and run the representative
`A -> B-only -> AB` qualification.

The durable supervisor persists this state machine:

```text
PRECHECK -> MUTATING -> STOPPING -> CLEANING -> VERIFYING_ZERO -> REVOKING_TOKEN -> DONE|FAILED
PRECHECK -> STAGING_ARMED -(unit restart)-> STOPPING -> CLEANING -> VERIFYING_ZERO -> REVOKING_TOKEN -> STAGING_DONE|FAILED
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
- `runtime/secrets/pve_ssh`
- `runtime/secrets/guest_ssh`
- `runtime/secrets/pve-known_hosts`
- `runtime/secrets/pve-token.tfvars`
- `runtime/secrets/pve-ca.pem`

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

The PVE token source is exactly `runtime/secrets/pve-token.tfvars`; it is
copied to `runtime/pinned/pve-token.tfvars` with the other supervisor inputs
before precheck. Drivers consume only that pinned copy. A later deletion or
modification of the mutable source is recorded as input tampering but cannot
remove the credential required for supervised cleanup.

Create a distinct API token for each run under the pre-provisioned,
least-privilege `pve.tokenOwner` service account; the release lifecycle never
creates PVE users or ACLs. Create the token with privilege separation disabled:
`pveum user token add <pve.tokenOwner> <runId> --privsep 0 --output-format json`.
This is required so that the ephemeral token inherits only that service
account's already-reviewed, least-privilege ACLs. Do not broaden the account
ACLs or add a global role for a run. `--privsep 1` is unsupported unless
identical token-specific ACLs have been separately provisioned and audited;
the tracked lifecycle neither creates nor audits token-specific ACLs, and that
mode can hide the source template from authenticated inventory. Its only
assignment must use the form
`pve_api_token = "<pve.tokenOwner>!<runId>=<secret>"`: the token name is
exactly the contract `runId`, while the command's returned secret is transferred
directly to that mode-0600 run input and never to a terminal, log, or shell
history. `root@pam` is rejected. The precheck rejects a reusable, differently
named, or differently owned token. This lets the reviewed post-zero hook
identify and delete that one token without ever printing, logging, or sending
its secret to the PVE host; it never deletes PVE users or ACLs.

PVE hypervisor host keys are likewise a run input: `pveSshKnownHosts` must
name exactly `runtime/secrets/pve-known_hosts`, with ordinary (not hashed or
wildcard) entries for the leaf PVE host and both RR PVE hosts declared by the
contract. The supervisor copies it to `runtime/pinned/pve-known_hosts`; every
`root@PVE` SSH call uses that file through `UserKnownHostsFile` together with
`GlobalKnownHostsFile=/dev/null` and `StrictHostKeyChecking=yes`. It never
falls back to the service account's or system-wide ambient known-hosts files.

`guestSshPrivateKey` must name exactly `runtime/secrets/guest_ssh`. It is a
separate non-root guest/cloud identity: its derived OpenSSH public key must
equal both `guestSSH.publicKey` in the contract and `ssh_public_key` in the
run-confined tfvars. The PVE root key and guest key may not be the same public
key. Before mutation, authenticated AWS preflight also proves that the named
EC2 key pair has this same public key; the evidence retains only fingerprints.
Azure, OCI, and PVE receive the tfvars public key directly. No guest or cloud
SSH path may use `pve_ssh`.

The PVE cluster CA is exactly `runtime/secrets/pve-ca.pem`; it is copied to
`runtime/pinned/pve-ca.pem` under the same 0600, digest-checked supervisor
boundary. Release qualification rejects `pve_insecure = true`. The PVE API
preflight uses the pinned file with `curl --cacert`, and the OpenTofu wrapper
passes it as `SSL_CERT_FILE` only to the OpenTofu/provider child process; it
does not modify the host trust store or consume an ambient CA path. A deleted
or changed mutable CA is recorded as tampering while the pinned copy remains
available to finish supervised cleanup.

The contract must bind:

- exact untagged RC commit, frozen main parent, canonical origin and artifact
  SHA-256;
- SHA-256 identities for every release and QA script used by the run;
- the canonical tracked QA commit and origin;
- the approved remote execution host and provider mirror versions;
- mutation TTL no greater than 55 minutes and heartbeat-stale less than TTL;
- exact regions, instance types, provider counts and a cost ceiling no greater
  than USD 1.00.
- `safety.pveManagementControlPlane: none`, `safety.pveTLS: pinned-ca`, and
  `pve.managementAddressSource: qga-dhcp`. The PVE management bridge is a
  shared underlay whose existing DHCP service assigns the six guest addresses.
  After PVE apply, QGA must discover each address before configuration
  generation. The release profile rejects generated PVE router configs
  containing DHCP (v4 or v6) or IPv6 RA resources before it deploys `routerd`.
- `stateMode: fresh-fabric-fresh-state`; release qualification never imports,
  moves, or updates a legacy AWS-RR state. Baseline inventory and OpenTofu
  state must both be zero before any provider operation.
- separate PVE identities: `pve.sshHost` is a DNS FQDN used by every PVE
  DNS/TCP/SSH consumer, while `pve.node` is the short Proxmox cluster node ID
  used by `pvesh /nodes/...` and Terraform `pve_node_name`. The FQDN's first
  label must exactly equal the cluster node ID; `pve_endpoint` must be the
  corresponding `https://<pve.sshHost>:8006/` API endpoint. `pve.rrNodes`
  must contain exactly `pve-rr-a` and `pve-rr-b` on distinct PVE node/FQDN
  pairs, with distinct VMIDs; `pve.rrFaultDomain` must be `host-redundant`.
  Static management CIDRs and gateways are prohibited. Each PVE output starts
  as `pending-qga-dhcp`; the PVE certification driver queries the guest's own
  declared PVE host through QGA and writes the observed management address
  before configuration generation. In the same authenticated QGA transaction
  it reads and validates each guest's public SSH host key, binds it to that
  address in a mode-0600 `guest-known_hosts` artifact, and marks the patched
  output with `ssh_host_key_source: qga`. PVE guest readiness/deployment uses
  that strict pin; it never learns a shared-underlay guest key with
  `StrictHostKeyChecking=accept-new` or `ssh-keyscan`. The leaf
  `pve.underlayBridge` and each RR `underlayBridge` are pinned to tfvars and
  must differ from the leaf-only `pve.captureBridge`; the PVE certification
  audit subsequently reads each RR host's `qm config` and requires one NIC on
  that pinned underlay bridge with no capture-bridge attachment. `pve.vmids`
  is a six-entry map keyed by the exact PVE topology node names; the guard
  binds every value to its corresponding tfvars input, including both leaf and
  client pairs. `pve.templateStage` adds one distinct, unstarted seventh VMID:
  the certification driver makes a full copy of the immutable source template
  onto the shared `qnap` datastore, verifies its PVE config is a template with
  qnap-backed data disks, and only then plans the six leaf/RR clones. The
  capture bridge is not an API-token Terraform resource or a persistent PVE
  network configuration: a pinned strict `root@PVE` driver creates only a
  live, portless, addressless bridge after read-only preflight, records exact
  run ownership in its Linux link alias, checks the management address/default
  route did not change, and deletes it only after cluster inventory reports
  all seven run VMIDs absent. It never calls the PVE network write API,
  leaves no `/etc/network/interfaces.new`, and never performs a PVE-wide
  network reload. IPv6 is disabled before the bridge is brought up; both
  creation and deletion prove no L3 address or bridge ports, while a
  same-name persistent PVE configuration or foreign link alias is refused.
  The shared `SAMNodeSet`
  carries no public PVE WireGuard
  endpoint. PVE routers receive PVE-local peer bootstrap endpoints that target
  only other QGA-discovered PVE guest-management addresses; PVE-to-cloud peers
  initiate outbound and cloud peers learn the source endpoint from WireGuard
  handshakes. A new certified VMID range therefore needs no static IPAM
  reservation, source edit, or auto-assigned VM ID.

`contract.example.json` and `terraform.tfvars.example` are parseable templates,
not runnable lab inputs. Their PVE hosts, bridges, routes and API endpoints use
`<certified-...>` placeholders. The contract keeps `0` only as a type-preserving
VMID sentinel: both the release guard and Terraform require positive, distinct
VMIDs, so a copied template stops before any PVE API operation. Populate a
run-confined copy only after the PVE substrate review has certified the actual
hosts, bridges, source template, qnap datastore and a fresh seven-ID range.

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
- the six exact PVE workload VMIDs, the disposable shared-template stage VMID,
  and the exact capture bridge.

Missing, duplicate, partial, unknown or failed queries are not zero. The same
inventory gate runs after unconditional destroy.

After every successful cleanup and final zero inventory, the supervisor enters
`REVOKING_TOKEN` and automatically invokes the reviewed run checkout's
post-zero hook as the `routerd-release-qa` service account. This happens for
both production and staging runs, including a precheck failure. `DONE`,
`FAILED`, and `STAGING_DONE` are not written until that idempotent action
succeeds; a service restart in this phase retries only revocation, never a
cleanup or paid mutation.

If a PVE provider create fails before the VM reaches OpenTofu state, cleanup
still runs the reviewed `pve-orphan-cleanup.sh` before capture-bridge removal.
It first obtains authoritative cluster inventory, admits only the seven
contract-pinned VMIDs on their exact nodes, verifies the generated name, run
marker, and role tags for every present target, and only then deletes workload
VMs before the disposable template stage. The remote command verifies the
same identity immediately before stop and destroy, and cluster inventory is
re-read after each deletion. SSH, PVE API, lock, ambiguity, node/type, or
identity errors fail closed and delete nothing unadmitted. This helper is part
of the required `qaImplementation.scriptBlobs` pre-mutation review boundary.

If a fresh `staging-no-mutation` run fails precheck solely because its already
pinned `terraform.tfvars` has a stale artifact `commit`, the recovery wrappers
may bypass that one equality only after proving durable precheck-failed state,
matching pinned contract/tfvars hashes, no mutation history, and no OpenTofu
state/output. They still reject a changed run ID, PVE identity, path, mode, or
input hash; precheck, mutation, production cleanup, and every OpenTofu action
continue to require the exact artifact commit.

The supervised hook accepts only `REVOKING_TOKEN`, tamper-free pinned inputs,
and the exact seven complete zero-inventory scopes. It uses only
`root@<pve.sshHost>` SSH with `StrictHostKeyChecking=yes`, sends only the
non-secret user/token identity to `pveum`, writes mode-0600 private SSH
diagnostics if needed, and stores a non-secret receipt at
`runtime/evidence/final-token-revocation/revocation.json`. The root-only
sealed provider-auth finalizer requires that matching receipt, so it cannot
discard recovery credentials before the run token is gone. The manual form of
the hook is production-only and is reserved for independently verified
terminal recovery; it still refuses an active/unknown unit.

## Bounded Cloud SAM qualification

The release contract has one permitted final Cloud SAM profile:
`representative-redundancy`. It is not an alias for the exhaustive
`sam-full-validation.sh` engineering suite. It stages the two PVE RRs as
`A -> AB`, runs one full non-legacy/non-performance baseline (control,
provider, 56 directed client flows, and 42 cloud-ingress flows), then proves
`A -> B-only -> AB` with ordered RR BGP-membership evidence, all-leaf
control/provider gates, and four cross-site hostname canaries. It deliberately
does not repeat the symmetric B outage.
It neither provisions nor destroys resources.

The contract binds at most 18 minutes for cloud/PVE provision and
certification, at most 32 minutes for the profile, and at least five minutes
of supervisor reserve inside a 55-minute mutation TTL. Each stage is
hard-bounded; a timeout fails and transfers control to the existing quiesce,
run-scoped cleanup, and exhaustive zero-inventory path. Two cleanup attempts
can extend the recovery envelope to 85 minutes; that is a recovery ceiling,
not a permitted test duration. The USD ceiling and topology allowlist do not
change. The exact artifact contract pins both the profile wrapper and its
`sam-e2e.sh` harness dependency.

Use the durable supervisor as the only paid entry point. For the exact profile
command, evidence contract, and local offline test, see
[`docs/operations/cloud-sam-representative-redundancy.md`](../../docs/operations/cloud-sam-representative-redundancy.md).

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
   REVOKING_TOKEN -> STAGING_DONE`. A missing unit restart, failed cleanup,
   failed token revocation, or nonzero/partial
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
