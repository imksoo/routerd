---
title: Stable milestone
sidebar_label: Stable milestone
sidebar_position: 0
---

# Stable milestone

routerd ships frequently using the `vYYYYMMDD.HHmm` scheme. From those builds we pick a **production-recommended** release at each milestone. When you start a new deployment, use the version listed here.

## Current recommended release

| Item | Value |
| --- | --- |
| Version | **v20260619.1730** |
| Status | Current production-recommended stable release |
| Track record | v20260626.2350 promotion was retracted after post-release fresh full-topology validation failed twice on SAM provider-action/capture behavior. v20260627.1107 later passed an isolated-PVE rerun for fresh baseline, representative AWS/Azure/OCI/PVE leaf failover/rejoin, BFD restart-safe regression coverage, and cleanup state 0, but it still has stale capture/provider-action operator residuals and is not promoted to production-recommended stable. |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## v20260626.2350 promotion retracted

v20260626.2350 is still a useful release artifact for the Live ISO
qemu-guest-agent firstboot fix and for the replacement PVE qualification path,
but it is no longer recommended as the production SAM rollback baseline.
Post-release fresh full-topology validation failed after the PVE provisioning
path and clean Ubuntu workload clients were corrected.

The release manifest is recorded in `docs/releases/manifests/v20260626.2350.yaml`.

### v20260626.2350 qualification

- **SAM baseline:** inherited from v20260626.1921, after the stale provider action feedback fix. The recorded full-topology run converged in 196s, passed the SSH matrix 56/56, cleaned up successfully, and left stateAfterDestroy 0.
- **v20260626.2050 delta:** PR #665 is a HealthCheck WhenFalse status display fix with no observed SAM dataplane impact. The failed 2050 fresh run is documented as an obsolete PVE template-clone lab provisioning problem, not a routerd/SAM blocker.
- **v20260626.2350 delta:** PR #681 fixes Live ISO qemu-guest-agent startup. `go test ./tests/liveiso` passed, the v20260626.2350 Release workflow passed, and a real PVE ISO boot on pve07 responded to `qm agent ping` with `qemu-guest-agent` active.
- **Post-release PVE qualification path:** the PVE live ISO + qnap config media path remains the intended replacement for the obsolete template-clone path for router/leaf VMs. The first 2026-06-27 4VM run is discarded because it used static ens18 management addresses; the later p2350-dhcpmgmt run corrected this by keeping ens18 on DHCP and discovering management addresses from QGA. Clean reusable Ubuntu workload clients were then provisioned separately on pve07 with qnap storage, auto VMIDs, ens18 DHCP, ens19 fixed 10.77 overlay addresses, QGA, and overlay ping verified.
- **Post-release SAM follow-up:** the p2350-dhcpmgmt fresh full-topology baseline failed before matrix/failover/BFD because OCI attempted a duplicate same-site secondary private IP assignment (`10.77.60.11`). PVE router/leaf DHCP, QGA, qnap media, auto VMID, serial console, and cleanup were verified. The clean Ubuntu client evidence does not change that SAM failure classification; it only removes the PVE client provisioning gap. The OCI failure is tracked as a SAM planner/provider-action follow-up for the next release line.
- **Clean-client rerun:** the p2350-cleanclients fresh full-topology run used clean reusable Ubuntu PVE clients and still failed control-plane readiness. `aws-leaf-a` retained one failed AWS `assign-secondary-ip` action for `10.77.60.16/32` and stale capture evidence for remote/provider addresses. OCI leaves were Ready and OCI OS diagnostics did not identify the baseline blocker. Initial matrix reached 50/56, with six hostname-check failures, but readiness timed out at 620s.

### Inherited from v20260608.2325

## v20260627.1107 post-release validation

v20260627.1107 is the current post-2350 validation candidate, but it is not the
production-recommended stable release.

The discarded p1107-cleanclients run failed because the PVE overlay still used a
shared segment contaminated by existing non-test VMs. The rerun
`p1107-rsamclnt-20260627T121655Z` moved PVE leaves and reusable clients onto the
dedicated `rsamclnt` bridge, kept PVE management on DHCP/QGA, used qnap-backed
live ISO plus config media, and passed:

- fresh full-topology baseline: convergence 110s, SSH matrix 56/56
- representative AWS/Azure/OCI/PVE leaf failover/rejoin: all matrix phases 56/56
- BFD restart-safe controller regression tests
- cleanup: OpenTofu destroy 54 resources, plan-destroy exit code 0, cloud
  provider inventory PASS, PVE leaf VMIDs absent

The release is not promoted because `routerctl mobility explain` still showed
12 Pending/StaleCapture rows and final `routerctl action list` snapshots still
contained historical failed provider-action rows on cloud leaves. Those did not
block dataplane or readiness in the rerun, but they are too noisy for a strong
rollback baseline.

The manifest is recorded in `docs/releases/manifests/v20260627.1107.yaml`.

This release carries forward the prior stable milestone features: **peersFrom**, **membersFrom**, and **peer-group-sync** for zero-touch leaf configuration in SAM fabrics.

### peersFrom + SAMPeerGroup (#332, #333)

`SAMTransportProfile` gains `spec.peersFrom` referencing `SAMPeerGroup` resources. Union semantics: imported peers load first, static `peers` override by `nodeRef`. `publishPeerGroup: true` on RR generates a `SAMPeerGroup` `DynamicConfigPart` automatically.

### Peer group sync (#334, #336)

Lightweight HTTP service on port 19652 over WireGuard inner network. RR serves `GET /v1/peer-groups`; leaf discovers WireGuard peers and fetches matching groups automatically. No manual `SAMPeerGroup` distribution needed.

### MobilityMemberSet + membersFrom (#339, #340)

`MobilityMemberSet` Kind carries shared identity-only pool members (`nodeRef`, `site`, `role`). `MobilityPool.spec.membersFrom` imports them; leaves keep only their own capture/discovery details inline. `publishMemberSet: true` generates and distributes the member set via `GET /v1/member-sets`. Reduces O(N²) config duplication — svnet1 configs reduced by 78 lines (2624 → 2546).

### FreeBSD legacy flag compatibility (#337, #338)

Removed `routerd serve` flags (`--observe-interval`, `--controller-chain*`) are now accepted and ignored with a warning, preventing upgrade failures when `/etc/rc.conf` retains stale entries.

### Inherited from v20260608.1354

All properties from v20260608.1354 are carried forward: pair-stable addressing, ADR 0014 CLI redesign, and all prior production-safe fixes.

## Known observations (not release blockers)

- **`routerd-bgp` may keep running with the old executable inode after `install.sh`.** This is intentional: `install.sh` does not restart `routerd-bgp` on upgrade so established BGP sessions and ECMP survive the routerd binary update.
- **`routerctl doctor mgmt` SKIPs when no `ManagementAccess` is declared.** This is a live-config choice, not a release defect.

:::warning Upgrading
- **From v20260528.2308:** ADR 0014 changed the CLI verb surface. `routerd apply` → `routerctl apply`, `routerd validate` → `routerctl validate`, etc. Rewrite service units or scripts that use old commands. `install.sh` auto-deploys new service units, so systemd-managed units update automatically.
- **Always `cd` into the extracted release directory before running `install.sh`.**
- **From v20260523.1542 or earlier:** the `disabled:` field was removed (use `enabled: false`) along with `--controller-chain*` / `--observe-interval` flags.
- **DNS resolver service unit:** the resolver runs as `routerd-dns-resolver@<name>.service`. The first upgrade performs a one-time cutover with a brief DNS blip.
:::

## What "stable" means here

:::warning The API is still v1alpha1
A "stable milestone" means **this build is production-quality**. It does **not** promise backward compatibility of the API (resource schema).
:::

- The routerd resource API is currently **v1alpha1**. **Breaking changes can land between releases.**
- When upgrading, do not rely on backward compatibility. Plan to **rewrite your configuration (YAML) against the new schema**.
- There is no migration shim by policy. Review the per-release deltas in the [changelog](./changelog.md).

## Install and upgrade

See [Install and upgrade](../install-and-upgrade.md) for the procedure. Start upgrades from a recommended milestone release.
