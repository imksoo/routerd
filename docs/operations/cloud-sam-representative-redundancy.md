# Cloud SAM representative redundancy qualification

`representative-redundancy` is the cost-conscious full-topology Cloud SAM
transition profile. It is separate from both the baseline-only
[`full-topology-minimal`](cloud-sam-full-topology-minimal.md) profile and the
exhaustive engineering
[`sam-full-validation.sh`](../../tests/e2e/cloudedge/scripts/sam-full-validation.sh)
suite.

It includes the representative RR-A transition and one edge-A stop/rejoin
scenario at each of AWS, Azure, OCI, and PVE. The former RR-only scope no
longer describes this profile. B-side stop/rejoin scenarios are intentionally
outside the selected scope.

The reviewed source contract selects this profile, but no live qualification
has been run for the current tree. Running its offline test does not authorize
an execution host, a PVE or cloud operation, `routerd`, DHCP, IPv6 RA, DHCPv6,
BGP, or SSH traffic.

The coordinator may be remote (the default `execution.hostPolicy:
approved-remote`) or explicitly authorized on the local machine. Local
coordination requires `execution.hostPolicy: local-supervised`,
`execution.requireRemote: false`, and an `execution.host` that exactly matches
the machine's `socket.getfqdn()` result. It uses the same tracked systemd
supervisor, staging proof, source/artifact checks, budgets, and cleanup; the
interactive shell is not the lifecycle owner. This changes only the
coordinator location, not the AWS/Azure/OCI/PVE topology or the E2E coverage.
The coordinator itself never runs `routerd`, including config-validation
sandbox mode. See the [release-QA runbook](https://github.com/imksoo/routerd/blob/main/tools/release-qa-labs/README.md)
for the required runtime preparation.

Before baseline inventory, prepare only the selected Azure account's credentials in
the run's mode-0700/0600 secret store and its initial writable
`runtime/provider-state/azure` copy. A service principal requires its selected
`service_principal_entries.json` entry; an Azure profile or MSAL cache alone is
insufficient. Test authenticated read-only APIs as the service user, not just
cached `az account show`. The OCI CLI must run as that user without opening the
operator's home. Verify provider ZIP hashes against the RC lock file, install
the unpacked `linux_amd64` mirror, and match installed units to the reviewed RC.
The launcher later reconstructs Azure working state from its sealed snapshot.

Validate PVE token creation's `info.privsep` as zero (`0` or `"0"`) before
writing the protected token input. A parse failure must not trigger another
creation attempt. Existing broad `PVEAdmin` ACLs are not run-ID-scoped or a
least-privilege PASS; never broaden them for a run. Explicit scoped lab
authorization is not production-infrastructure authorization.

The resource lifecycle requires all seven inventory scopes zero before
`MUTATING` and again after cleanup. Between PVE and cloud certification, keep
the successfully certified PVE guests, template, capture bridge, and state:
they are required by the cloud/qualification stages. The cloud saved plan still
passes the closed resource-type, count, and create-action guards; there is no
interphase all-scope zero check or new cloud-only inventory gate. This resource
inventory baseline is distinct from the later traffic baseline below.

## What it proves

The topology is fixed at ten routers and eight clients:

- `pve-rr-a` and `pve-rr-b` are route reflectors on PVE, never AWS compute;
- AWS, Azure, OCI, and PVE each have two leaves and two clients;
- the RR VMs are on distinct PVE hosts, with distinct PVE-host SSH FQDNs and
  QGA-discovered guest-management addresses used only for PVE-local WireGuard peer
  bootstrap; and
- every PVE leaf and RR has a management/underlay bridge distinct from the
  leaf-only capture bridge. RR VMs have no capture NIC.

Before the profile starts, the PVE certification audit reads `qm config` on
each RR's declared PVE host. It requires exactly one RR NIC, its pinned
underlay bridge, and no attachment to the leaf capture bridge; Terraform
output alone is not accepted as evidence of that isolation.

The signed release contract also requires
`safety.pveManagementControlPlane: none`, `safety.pveTLS: pinned-ca`, and
`pve.managementAddressSource: qga-dhcp`. The PVE API is trusted only through
the run-pinned cluster CA; qualification never uses an insecure TLS mode or
changes the execution host's trust store. PVE guests receive management
addresses only from the existing PVE-underlay DHCP service; after PVE apply,
QGA must discover every guest address before configuration generation. The generator emits no
management DHCP resource, and the harness rejects every generated PVE router
config that contains a DHCPv4/DHCPv6, IPv6RAAddress, or IPv6 router-advertisement resource
before any `routerd` service is deployed. It does not use the shared management
L2 as a DHCP, DHCPv6, or RA test network.

The profile rejects `topology_scale != full`, a non-`host-redundant` PVE RR
fault domain, or a same-host RR pair. A same-host pair may be useful as a
labelled cost smoke, but it is not host-redundant qualification. The shared
`SAMNodeSet` does not publish PVE WireGuard endpoints: generated PVE configs
use QGA-discovered guest-management bootstrap peers locally, while PVE-to-cloud peers
initiate outbound and cloud peers learn their endpoints from handshakes.

### PVE guest NIC and image prerequisites

The closed PVE template profile uses `eth0` for management on all six guests,
`ens19` for leaf capture, and `eth1` for client capture. PVE/cloud-init renames
NICs that have an `ipconfigN` entry to `ethN`: management has DHCP, clients have
capture-IP initialization, and leaves deliberately have no capture-IP seed.
The certification driver selects the management and leaf-capture names for QGA;
the qualification driver passes the same mapping through
`PVE_MANAGEMENT_INTERFACE`, `PVE_CAPTURE_INTERFACE`, and
`PVE_CLIENT_CAPTURE_INTERFACE`. Standalone script defaults are not changed.

Keep QGA's exact address and capture-MAC checks. On an address-discovery failure,
inspect the saved QGA interface list before classifying it as missing DHCP.
The source image must also contain `qemu-guest-agent`; enabling the PVE device
does not install or start the guest daemon. Prepare and clean any derivative
image during environment certification, then certify fresh clones. Do not
ignore PVE's per-guest IP data or rename live NICs during qualification to make
an old interface assumption pass.

Its sequence is deliberately one-directional:

1. Deploy all leaf routers.
2. Deploy `pve-rr-a`, wait for its service/status socket and an actual BGP
   membership observation, then record that A joined before B is deployed.
3. Deploy `pve-rr-b`, wait for the same membership observation, then record
   that the pair is ready.
4. Run the full baseline: control/dataplane and provider gates, all 56
   directed client hostname flows, and all 42 cloud-origin ingress flows.
5. Stop `pve-rr-a`. First prove that `pve-rr-b` still has an observed BGP
   membership, retain the all-leaf control/ownership and provider gates, then
   run four hostname canaries around AWS → Azure → OCI → PVE → AWS.
6. Rejoin `pve-rr-a` and run the same transition gates and canaries.
7. In order, run one independent harness invocation for `aws-leaf-a`,
   `azure-leaf-a`, `oci-leaf-a`, and `pve-leaf-a`. Each invocation stops only
   its selected A, verifies the surviving routers including that site's B,
   restores A, and finishes that rejoin before the next site's scenario.
   Both the stopped-A and restored-A phases require all surviving leaves'
   control/dataplane, provider/ownership, and RR membership gates, all 56
   directed client hostname flows, and all 42 cloud-origin ingress flows.

The initial RR invocation retains one complete baseline and four cross-site
canaries for each RR transition. The later edge invocations do not repeat
deployment or the initial baseline, but **do** repeat both complete traffic
matrices after every edge stop and every edge rejoin. They never substitute
the RR canaries for edge E2E evidence. Missing, duplicate, failed, or skipped
required evidence is not a pass.

Here, a stop means stopping the guest's `routerd.service` and its separate
`routerd-bgp.service` when present, then starting those services for rejoin.
It is not a VM power-off, a physical-host outage, or proof that an in-flight
application connection survives without interruption.
Both RR and edge scenarios require explicit successful stop/inactive and
start/active acknowledgements as well as their traffic and membership checks.
A successful matrix alone cannot establish that the requested service
transition actually occurred.

### A/B coverage boundary {#ab-coverage-boundary}

This profile permits only the default PVE `single-router` ownership gate.
CARP is outside its scope: the generator's optional CARP mode assigns
different primary/secondary priorities and cannot be treated as symmetric.

Static source review found shared same-role A/B generation paths for OS/image
selection, capture/provider mode, and timers in the default topology. This
is a limited template comparison, not proof of identical deployed state:

- RR placement is deliberately on different hosts, and each RR can have its
  own underlay bridge/VLAN. A also joins before B during bootstrap.
- PVE leaf A/B share a host and capture bridge; their service-level scenario
  does not establish host-fault redundancy.
- Identity, addresses, keys, provider resource identities, observed ownership,
  and bootstrap results remain node-specific. Shared unpinned image/CLI
  selectors do not prove matching installed versions.
- The PVE run1 verification/control annotations differ by default, but the
  current generator does not use them to set `gratuitousARPOnSeize`.
- The existing generator fixture does not exhaustively compare all cloud
  A/B pairs or the optional CARP mode.

A passing A-side scenario establishes only its observed stop/rejoin direction
and the surviving B's participation. B's own stop/rejoin and the reverse
transition remain **unverified**, not normalization-equivalent passes.
Changes to priority, OS, policy, provider/NIC behavior, timers, bootstrap, or
fault domain require a fresh scope review; do not silently extend this result.

It never provisions or destroys resources. The durable release-QA supervisor
is the only owner of creation, teardown, and exhaustive zero inventory.

## Command shape

Only use this after the source audit and an authorized, fresh release-QA
contract. The eventual supervisor invokes this shape, rather than an
interactive shell:

```sh
PVE_MANAGEMENT_INTERFACE=eth0 \
PVE_CAPTURE_INTERFACE=ens19 \
PVE_CLIENT_CAPTURE_INTERFACE=eth1 \
tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  --tofu-output /var/lib/routerd-release-qa/<run-id>/runtime/tofu-output-full.json \
  --artifact /var/lib/routerd-release-qa/<run-id>/runtime/routerd-<version>-linux-amd64.tar.gz \
  --tfvars /var/lib/routerd-release-qa/<run-id>/runtime/terraform.tfvars \
  --ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/guest_ssh \
  --pve-ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/pve_ssh \
  --pve-known-hosts /var/lib/routerd-release-qa/<run-id>/runtime/pinned/pve-known_hosts \
  --evidence-root /var/lib/routerd-release-qa/<run-id>/runtime/evidence/qualification/representative-redundancy \
  --max-runtime-seconds 5400
```

### Approved budget {#approved-budget}

The approved source budget policy supersedes the former 32-minute
qualification / 55-minute mutation window. It does not change the topology,
failure scenarios, evidence gates, or cleanup requirements:

| Boundary | Approved value |
| --- | --- |
| Provision/certification | At most 18 minutes (1080 seconds) |
| Entire qualification wrapper | At most 90 minutes (5400 seconds) |
| Minimum supervisor reserve | At least 5 minutes (300 seconds) |
| Mutation TTL | At most 115 minutes (6900 seconds) |
| Cleanup/inventory allowance | Two attempts of 10 + 5 minutes, unchanged |
| Planned paid cleanup envelope | At most 145 minutes (8700 seconds) |
| Cost policy estimate / admission ceiling | USD 1.55 / USD 1.60 |

The minimum allocation is `18 + 90 + 5 = 113` minutes, leaving two minutes
of headroom within the 115-minute TTL. The planned cleanup envelope is
`115 + 2 × (10 + 5) = 145` minutes; it is not extra qualification time.
The cost figures are policy estimates, not current provider price quotes or
a cap on the actual bill. Authoritative cleanup and zero inventory remain
mandatory even if recovery exceeds that estimate.

`5400` seconds is both the wrapper's hard cap and the release-contract
qualification budget for the **entire** sequence, not for each of its five
harness invocations. Later invocations receive only the remaining budget;
evidence verification also consumes that time. Completion of this expanded
sequence has still not been measured in a live run. The larger approved
budget and offline PASS are not live qualification PASS or cost/duration
guarantees. Source approval alone is not live admission: a fresh approved
contract, canonical-source admission, and the required prechecks still apply;
this document does not establish that a revision was pushed or tested live.
A timeout is a failure and transfers control to the durable supervisor's
quiesce, cleanup, and exhaustive zero-inventory path; it does not authorize a
longer paid window or a reduced matrix.
The qualification driver also enforces an independent outer deadline. A
stalled wrapper or output pipeline therefore fails rather than extending the
run; the supervisor quiesces the mutation process group before cleanup.

Its `--tofu-output` input is the QGA-patched PVE certification output, not a
raw OpenTofu output: each PVE router must carry a QGA-derived `management_ip`,
`pve_management_source: qga-dhcp`, and QGA-validated `ssh_host_keys` with
`ssh_host_key_source: qga` before configuration generation. The certification
driver binds those keys to the discovered management addresses in a mode-0600
known-hosts artifact; `sam-e2e` uses the same pins for direct PVE guest SSH
instead of host-key scanning the shared PVE management network.

The first RR invocation retains these scenario flags:

```text
--staged-rr-pair pve-rr-a pve-rr-b
--failover-node pve-rr-a
--rejoin-after-failover
--transition-canary
--skip-legacy-protocols
--skip-load-balance-report
--success-evidence-minimal
```

Each subsequent edge invocation uses one selected site, with the original
generated configs and a separate evidence directory:

```text
--staged-rr-pair pve-rr-a pve-rr-b
--failover-node <site>-leaf-a
--rejoin-after-failover
--skip-deploy
--reuse-deployed-topology
--skip-initial-validation
--configs-dir <initial-RR-evidence>/config-gen/configs
--full-cloud-ingress
--skip-legacy-protocols
--skip-load-balance-report
--success-evidence-minimal
```

The staged-RR flag retains membership checks in each edge scenario; with
`--skip-deploy`, it does not redeploy the RRs. Edge invocations never pass
`--transition-canary`. `--skip-initial-validation` skips only the already
completed baseline, not the stop/rejoin validation sets or their mandatory
control/provider gates. `--success-evidence-minimal` omits optional successful
diagnostic snapshots, not those gates or either traffic matrix.

`--reuse-deployed-topology` requires both `--skip-deploy` and `--configs-dir`.
It skips PVE dataplane setup, guest-sandbox config validation, and client
hostname, SSH-key, and route setup. Read-only preflight and the generated-config
safety audit remain mandatory. Thus later scenarios do not rerun preparatory
repair or reconfiguration between transitions; they exercise the already
deployed topology with the requested service stop/rejoin.

It never passes `--destroy-cmd`, a performance flag, legacy-protocol flag, or
a B-side failure flag.

The initial evidence remains under `<evidence-root>/representative-redundancy`.
The four edge scenarios use sibling directories `edge-aws-leaf-a`,
`edge-azure-leaf-a`, `edge-oci-leaf-a`, and `edge-pve-leaf-a`. The profile result
records each edge scenario separately and keeps B-side equivalence unproven;
an aggregate result must not hide a failed or missing scenario.

## Offline source check

The fake-harness check verifies the exact argument contract, PVE RR
host-fault-domain and capture-bridge separation, ordered A/B BGP membership
evidence, the initial full baseline and RR canaries, and all four independent
edge-A stop/rejoin matrix contracts. Negative cases must reject incomplete
evidence, unexpected B-side scenarios, and exhausted shared budgets. It uses
no real endpoint or daemon and does not establish live failover success:

```sh
make cloudedge-representative-redundancy-offline-test
make cloudedge-pve-bridge-audit-offline-test
shellcheck -x tests/e2e/cloudedge/scripts/sam-e2e.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy-offline-test.sh
```
