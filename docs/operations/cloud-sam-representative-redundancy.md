# Cloud SAM representative redundancy qualification

`representative-redundancy` is the cost-conscious full-topology Cloud SAM
transition profile. It is separate from both the baseline-only
[`full-topology-minimal`](cloud-sam-full-topology-minimal.md) profile and the
exhaustive engineering
[`sam-full-validation.sh`](../../tests/e2e/cloudedge/scripts/sam-full-validation.sh)
suite.

The reviewed source contract selects this profile, but no live qualification
has been run for the current tree. Running its offline test does not authorize
an execution host, a PVE or cloud operation, `routerd`, DHCP, IPv6 RA, DHCPv6,
BGP, or SSH traffic.

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

The baseline verifies the complete topology once. The two transition phases
do not repeat a 98-flow baseline; doing so adds cloud time without adding an
independent redundancy fact. Conversely, the profile deliberately does not
claim that an untested `pve-rr-b` outage is covered by magic: it is omitted
only because both RR configurations are normalization-equivalent and the
chosen fault direction has already proven `A -> B-only -> AB`. If that
equivalence changes (priority, OS, provider/NIC behavior, BGP policy,
transport/bootstrap configuration, or fault domain), add a distinct transition
class rather than silently
reusing this result.

It never provisions or destroys resources. The durable release-QA supervisor
is the only owner of creation, teardown, and exhaustive zero inventory.

## Command shape

Only use this after the source audit and an authorized, fresh release-QA
contract. The eventual supervisor invokes this shape, rather than an
interactive shell:

```sh
tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  --tofu-output /var/lib/routerd-release-qa/<run-id>/runtime/tofu-output-full.json \
  --artifact /var/lib/routerd-release-qa/<run-id>/runtime/routerd-<version>-linux-amd64.tar.gz \
  --tfvars /var/lib/routerd-release-qa/<run-id>/runtime/terraform.tfvars \
  --ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/guest_ssh \
  --pve-ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/pve_ssh \
  --pve-known-hosts /var/lib/routerd-release-qa/<run-id>/runtime/pinned/pve-known_hosts \
  --evidence-root /var/lib/routerd-release-qa/<run-id>/runtime/evidence/qualification/representative-redundancy \
  --max-runtime-seconds 1920
```

`1920` seconds is both the wrapper's hard cap and the release-contract
qualification budget. A standalone invocation cannot extend the paid
window.

Its `--tofu-output` input is the QGA-patched PVE certification output, not a
raw OpenTofu output: each PVE router must carry a QGA-derived `management_ip`,
`pve_management_source: qga-dhcp`, and QGA-validated `ssh_host_keys` with
`ssh_host_key_source: qga` before configuration generation. The certification
driver binds those keys to the discovered management addresses in a mode-0600
known-hosts artifact; `sam-e2e` uses the same pins for direct PVE guest SSH
instead of host-key scanning the shared PVE management network.

The wrapper passes only:

```text
--staged-rr-pair pve-rr-a pve-rr-b
--failover-node pve-rr-a
--rejoin-after-failover
--transition-canary
--skip-legacy-protocols
--skip-load-balance-report
--success-evidence-minimal
```

It never passes `--destroy-cmd`, a performance flag, legacy-protocol flag, or
a B-side failure flag.

## Offline source check

The fake-harness check verifies the exact argument contract, PVE RR
host-fault-domain and capture-bridge separation, ordered A/B BGP membership
evidence, one full baseline, and four-row transition evidence. It uses no
real endpoint or daemon:

```sh
make cloudedge-representative-redundancy-offline-test
make cloudedge-pve-bridge-audit-offline-test
shellcheck -x tests/e2e/cloudedge/scripts/sam-e2e.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy-offline-test.sh
```
