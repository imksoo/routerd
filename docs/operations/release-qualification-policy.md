---
title: Release qualification policy
---

# Release qualification policy

routerd release qualification tests the product, not the test substrate. The
test substrate must be certified before the release run starts.

## Gating rule

Release qualification must not start unless all of the following are true:

- A certification manifest exists.
- The manifest validates against
  `docs/releases/manifests/release-environment-certification.schema.json`.
- `status` is `pass`.
- `expiresAt` is later than the current UTC time.
- The manifest environment, topology, provider set, and script revisions match
  the requested qualification run.

No certification, failed certification, blocked certification, mismatched
certification, or expired certification is a hard stop.

## Failure classification

| Phase | Failure example | Classification |
| --- | --- | --- |
| Environment certification | PVE bridge missing, QGA dead, config disk broken, cloud-init broken, cloud quota missing | Infra failure |
| Environment certification | Repair succeeds and all checks pass | Certified environment |
| Release preflight | Manifest missing, expired, invalid, or mismatched | Infra failure / no-go |
| Release qualification | routerd readiness fails on certified substrate | routerd product failure |
| Release qualification | connectivity matrix fails on certified substrate | routerd product failure |
| Release qualification | provider action check fails on certified substrate | routerd product failure |
| Release qualification | PVE VM, bridge, QGA, config disk, cloud bootstrap, identity, or quota defect is discovered | Infra failure; stop qualification |

Only failures observed after a passing preflight and within routerd readiness,
matrix, or provider action checks count as routerd product failures.

## No repair during qualification

After release qualification starts, operators and automation must not repair:

- PVE VMs, templates, bridges, VLANs, firewall state, or node services.
- QGA readiness, guest networking, SSH bootstrap, config disks, or NoCloud media.
- Cloud images, quotas, identities, provider networks, route tables, security
  groups, public IPs, metadata access, or cloud-init bootstrap.
- Cross-substrate reachability that belongs to the lab fabric rather than
  routerd behavior.

If any of these fail during qualification, stop the run and return to
environment certification. The next qualification attempt must reference a new
passing certification manifest.

## Dirty fabric with a fresh database

Release qualification does not support reusing a dirty lab fabric with a fresh
routerd state database. The fabric includes provider-owned secondary IPs, PVE
bridges and guests, guest `/tmp/routerd-*` residue, config media, provider
action side effects, and any other substrate state that can outlive a routerd
process or database.

This combination produces ambiguous evidence: routerd sees no prior action or
ownership rows, while the fabric can still contain old holders, stale local
artifacts, or provider-side mutations. Treat the result as an unsupported test
setup, not as a routerd product failure or a valid passing release signal.

Operators must choose one of these supported paths before qualification:

- reuse both the certified fabric and its matching routerd state;
- clean the fabric and start with a fresh routerd state database;
- recertify after explicitly repairing and recording every retained substrate
  artifact that the fresh database will encounter.

## Product qualification scope

With a certified environment, release qualification may evaluate:

- routerd process readiness and API/status readiness.
- Config validation, plan, apply, and rendered service state.
- Full connectivity matrix expected for the release topology.
- Provider action execution and fencing behavior.
- Failure handling that is intentionally injected by the routerd qualification
  scenario, as long as the substrate remains certified.

The qualification result should reference the certification manifest ID and
record whether each failure is `product_failure`, `infra_failure`, or
`preflight_failure`.

## Operator workflow

1. Run PVE and cloud certification in routerd-labs.
2. Review any repairs made during certification.
3. Store or attach the certification manifest.
4. Run release preflight against that manifest.
5. Run release qualification smoke on the certified environment.
6. If qualification finds substrate damage, stop and recertify; do not repair in
   place during the product run.

This separation keeps release evidence interpretable: certification proves the
lab was trustworthy, and qualification proves routerd behavior on that trusted
lab.
