# Cloud SAM full-topology baseline

`full-topology-minimal` is the one-pass full-topology baseline component used
by the paid final
[`representative-redundancy`](cloud-sam-representative-redundancy.md) profile.
It remains deliberately separate from
[`sam-full-validation.sh`](../../tests/e2e/cloudedge/scripts/sam-full-validation.sh),
which remains an exhaustive engineering and failover suite and must not be
scheduled inside the release-QA mutation window.

This profile has not run for the current working tree. Its offline wrapper
check is only a portable source gate; it does not authorize host validation,
provisioning, or paid qualification.

The profile requires the reviewed full topology: ten router nodes
(`pve-rr-a`, `pve-rr-b`, and two leaves at AWS, Azure, OCI, and PVE) and eight
clients (two at each site). The shared `SAMNodeSet` has no WireGuard endpoint
for any PVE router. Each generated PVE router config instead contains local
explicit bootstrap peers for the other PVE router guests, addressed only by their
QGA-discovered guest-management IPs. Those bootstrap addresses never appear in cloud
configs and are not PVE host or public addresses. PVE routers initiate the
cloud peer handshakes outbound; cloud peers learn the resulting endpoint from
WireGuard handshakes. It deploys the exact artifact to all ten routers, then
accepts only this one baseline:

- the control-plane/dataplane readiness gate;
- all 56 directed client-to-client hostname flows;
- all 42 cloud-origin directed cloud-ingress hostname flows; and
- the `MobilityPool` provider readiness/no-conflict gate.

It does **not** run legacy protocol probes, performance probes, load-balance
reports, transfer probes, failover/rejoin, provisioning, or destruction.
The profile verifies the expected matrix row counts and every result before it
returns success; an incomplete matrix is a failure, not a skipped check.

## Baseline-only budget

The standalone wrapper retains its 20-minute cap so it remains a bounded
baseline tool. The final release contract does **not** select this profile by
itself: it selects `representative-redundancy`, which adds a staged PVE RR
`A -> AB -> B-only -> AB` transition without repeating the complete traffic
matrix after each transition. Its current contract budget is:

```json
"qualification": {
  "profile": "representative-redundancy",
  "runScope": "full-representative",
  "provisioningBudgetSeconds": 1080,
  "qualificationBudgetSeconds": 1920,
  "minimumSupervisorReserveSeconds": 300
}
```

The guard rejects another final profile, a provision/certification budget over
18 minutes, a representative qualification budget over 32 minutes, a reserve
below five minutes, or a sum that exceeds the 55-minute mutation TTL. The
artifact contract pins the representative wrapper and its `sam-e2e.sh` harness
dependency.

The release-QA mutation driver spends the first bounded budget across cloud and
PVE provision/certification. It then invokes the profile with the second
budget. Each boundary uses `timeout`; a timeout fails closed and the durable
supervisor terminates the mutation process group before cleanup. The
supervisor—not the profile—runs the run-scoped OpenTofu destroy and exhaustive
zero inventory. Cleanup and inventory each retain their pinned per-attempt
bounds and recovery is never abandoned merely because the paid mutation timer
expired.

Before a paid cloud run, the same pinned contract may use
`"runScope": "pve-certification-only"`. That is one full PVE topology gate
only: after its PVE certificate succeeds, the mutation driver exits before
cloud provisioning or `routerd` product qualification. Cleanup, the complete
seven-scope zero-inventory proof, and PVE token revocation still run under the
supervisor. It is evidence for proceeding to a new fresh
`full-representative` run, not a release pass.

This makes provisioning/certification, qualification, and cleanup distinct
contracts. It avoids treating a multi-hour fault-injection suite as if it fit a
55-minute paid mutation deadline.

## Execution

Only run this after the mandatory pre-host audit in
[`cloud-sam-rearchitecture-goal.md`](cloud-sam-rearchitecture-goal.md) has
passed, all local Cloud SAM checks have passed, and an authorized, fresh
release-QA contract has passed read-only precheck. This includes a
pre-provisioned topology: do not run the profile while the audit prohibits
host or cloud validation. The canonical paid entry point remains the durable
supervisor:

```sh
tools/release-qa-labs/drivers/start-supervised-release-qa.sh \
  /var/lib/routerd-release-qa/<run-id>/runtime/contract.json
```

For a pre-provisioned full topology, the exact profile command is:

```sh
tests/e2e/cloudedge/scripts/sam-full-topology-minimal.sh \
  --tofu-output /var/lib/routerd-release-qa/<run-id>/runtime/tofu-output-full.json \
  --artifact /var/lib/routerd-release-qa/<run-id>/runtime/routerd-<version>-linux-amd64.tar.gz \
  --tfvars /var/lib/routerd-release-qa/<run-id>/runtime/terraform.tfvars \
  --ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/guest_ssh \
  --pve-ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/pve_ssh \
  --pve-known-hosts /var/lib/routerd-release-qa/<run-id>/runtime/pinned/pve-known_hosts \
  --evidence-root /var/lib/routerd-release-qa/<run-id>/runtime/evidence/qualification/full-topology-minimal \
  --max-runtime-seconds 1200
```

`tofu-output-full.json` in this command is the QGA-patched output written by
the PVE certification driver, not raw `tofu output -json`: every PVE router
must have `management_ip`, `pve_management_source: qga-dhcp`, and
QGA-validated `ssh_host_keys` marked `ssh_host_key_source: qga` before the
generator can render its local WireGuard bootstrap peers. The same QGA step
creates a mode-0600 known-hosts artifact that binds those keys to the guest
management addresses, so direct PVE guest SSH does not learn a key from the
shared management network.

The command performs no provisioning or destruction. Do not add a teardown
argument: the durable supervisor owns unconditional cleanup so a failed
qualification cannot strand paid resources.

## Offline source checks

The following checks exercise only the profile wrapper with fake local files;
they do not provision instances, start `routerd`, bind a routerd socket, or
touch DHCP/RA/network state:

```sh
make cloudedge-full-topology-minimal-offline-test
shellcheck -x tests/e2e/cloudedge/scripts/sam-full-topology-minimal.sh \
  tests/e2e/cloudedge/scripts/sam-full-topology-minimal-offline-test.sh \
  tools/release-qa-labs/drivers/qualification-driver.sh \
  tools/release-qa-labs/drivers/mutation-driver.sh
```

Do not run the release-QA Python suite as a host-safe preflight. Some of its
cases deliberately exercise `sudo`, service-manager, socket, or namespace
contracts; those belong only to the separately authorized release-QA phase.
