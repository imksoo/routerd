---
title: Release environment certification
---

# Release environment certification

Release qualification may only run on an environment that has first been
certified. The certification phase proves that the PVE and cloud substrate is
healthy enough to be used as a test environment. It is not a routerd product
test.

## Phase boundary

Release validation is split into two phases:

1. **Environment certification**: repair and certify PVE, bridges, QGA, config
   disks, cloud bootstrap, credentials, quotas, images, and basic reachability.
2. **Release qualification**: run routerd readiness, connectivity matrix, and
   provider action checks on a certified environment.

All substrate repair belongs to the certification phase. Once release
qualification starts, operators must not fix PVE VMs, bridges, QGA, config
disks, cloud-init, NoCloud media, provider bootstrap, identity, routes, security
groups, or equivalent lab substrate in place. Stop the qualification run, mark
it blocked by an infra failure, repair in certification, and issue a new
certification manifest.

## Certification status

A certification manifest has one of these statuses:

| Status | Meaning |
| --- | --- |
| `pass` | The environment is certified until `expiresAt`. Release qualification may start. |
| `fail` | The environment is not usable. This is an infra failure, not a routerd product failure. |
| `blocked` | Required substrate state could not be inspected or repaired. This is an infra failure. |
| `expired` | A previous `pass` is older than its validity window. Qualification must not start. |

Certification is valid only for the environment, topology, tool revisions, and
commit range recorded in the manifest. Any material change to PVE hosts, VM
templates, bridge layout, cloud bootstrap images, cloud account wiring, provider
networking, or routerd-labs certification scripts requires a new certification.

Default validity is 24 hours unless the manifest states a shorter window.
Release qualification must compare current UTC time with `expiresAt`; stale
certification is equivalent to no certification.

## Required checks

Certification must cover both local and cloud substrate.

### PVE substrate

The PVE certification script is expected to verify:

- PVE API access and expected node inventory.
- Required bridges and VLAN-aware settings exist.
- VM templates or base images exist and match the expected identifiers.
- QGA is enabled and responsive for certification test VMs.
- Config disk or NoCloud media is attached, readable, and regenerated when
  intentionally repaired during certification.
- VM boot reaches SSH and the expected hostname/user-data state.
- Required firewall, forwarding, and L2/L3 reachability checks pass between PVE
  test endpoints.

Repairs are allowed here. The manifest must record each repair action, including
what was changed and how it was verified afterwards.

### Cloud substrate

The cloud certification script is expected to verify:

- Provider credentials and selected account/subscription/tenancy identity.
- Required regions, quotas, images, instance shapes, SSH keys, and bootstrap
  templates.
- Provider networks, route tables, security groups, public IP behavior, and
  metadata service access needed by the release topology.
- Cloud-init or equivalent bootstrap reaches SSH and expected hostname/user-data
  state.
- Provider inventory and teardown filters are constrained by release run IDs or
  test tags.
- Basic east/west and cloud-to-PVE reachability checks pass for certification
  endpoints.

Repairs are allowed here. The manifest must record each repair action and the
post-repair check that made the environment certifiable.

## routerd-labs script contract

The script implementations live in the routerd-labs repository. This repository
defines their interface so release qualification can rely on the same contract.

### `certify-pve-substrate.sh`

Purpose: inspect and, when requested, repair the PVE substrate before release
qualification.

Required interface:

```sh
certify-pve-substrate.sh \
  --environment <name> \
  --topology <name> \
  --out <manifest.json> \
  [--repair] \
  [--valid-for 24h]
```

Behavior:

- Without `--repair`, perform read-only checks and fail if repair is required.
- With `--repair`, perform PVE-only substrate repair before producing the final
  certification result.
- Exit `0` only when the PVE portion is certified.
- Exit non-zero for infra failure or blocked inspection.
- Write a manifest that conforms to
  `docs/releases/manifests/release-environment-certification.schema.json`.

### `certify-cloud-substrate.sh`

Purpose: inspect and, when requested, repair cloud substrate before release
qualification.

Required interface:

```sh
certify-cloud-substrate.sh \
  --environment <name> \
  --topology <name> \
  --providers aws,azure,oci \
  --out <manifest.json> \
  [--repair] \
  [--valid-for 24h]
```

Behavior:

- Without `--repair`, perform read-only provider checks and fail if repair is
  required.
- With `--repair`, perform cloud-only substrate repair before producing the
  final certification result.
- Exit `0` only when every requested provider is certified.
- Exit non-zero for infra failure or blocked inspection.
- Write a manifest that conforms to the certification schema.

### `release-environment-preflight.sh`

Purpose: verify that release qualification is allowed to start.

Required interface:

```sh
release-environment-preflight.sh \
  --certification <manifest.json> \
  --environment <name> \
  --topology <name> \
  --providers aws,azure,oci,pve
```

Behavior:

- Validate the manifest schema.
- Fail if status is not `pass`.
- Fail if `expiresAt` is in the past.
- Fail if environment, topology, or provider set does not match the requested
  release qualification run.
- Fail if required PVE/cloud check groups are missing.
- Never repair substrate.

### `release-qualification-smoke.sh`

Purpose: run product qualification on a certified environment.

Required interface:

```sh
release-qualification-smoke.sh \
  --certification <manifest.json> \
  --release <version-or-commit> \
  --out <qualification-result.json>
```

Behavior:

- Run `release-environment-preflight.sh` first and abort if it fails.
- Run routerd readiness, connectivity matrix, and provider action smoke checks.
- Never repair PVE, bridge, QGA, config disk, cloud bootstrap, provider network,
  identity, or quota issues.
- Classify failures from routerd readiness, matrix, or provider action checks as
  routerd product failures only when preflight passed.
- Classify any discovered substrate defect as infra failure and stop the run.

## Evidence and retention

Certification manifests are release evidence. Store them under
`docs/releases/manifests/` or attach them to the release PR using the same schema.
Do not commit secrets, private IPs that identify private infrastructure unless
they are already public in the test topology, provider resource IDs, or raw CLI
debug logs. Record logical names, check results, timestamps, tool versions, and
repair summaries instead.
