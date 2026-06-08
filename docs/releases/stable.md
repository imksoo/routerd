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
| Version | **v20260608.1354** |
| Status | Recommended stable release (supersedes v20260608.0642; pair-stable SAM transport addressing — `addressingMode: pair-stable` for compact leaf-spine config authoring) |
| Track record | Validated on lab environments (7 compact configs), k8s cluster (10 nodes: 2 RR + 8 leaf, all BGP Established, FIB correct, connectivity pass), and production router (homert02, unaffected). 0 issues found |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260608.1354 is recommended

This release adds **pair-stable SAM transport addressing** on top of v20260608.0642.

### Pair-stable addressing (#330, #331)

`SAMTransportProfile` gains `spec.addressingMode: pair-stable`, a new /31 slot allocation algorithm that produces deterministic, stable tunnel addresses using fnv64a hashing of inner prefix and canonical peer key.

- **Compact config authoring.** Leaf nodes no longer require `topologyNodeRefs`, eliminating repetitive per-node topology declarations. svnet1 configs reduced by ~100 lines.
- **Stable across topology changes.** Adding or removing a node does not reassign addresses for existing peers (unlike `edge-index` which depends on sort order).
- **Backward compatible.** Existing `edge-index` (default) configurations are unchanged.
- **Collision detection.** `routerd validate` / `routerctl validate` detects /31 slot hash collisions at config time.

### Inherited from v20260608.0642

All properties from v20260608.0642 are carried forward: ADR 0014 CLI redesign, OpenRC hardening, DNS VRRP VIP support, forcefrag prerouting fix, BGP peer watch stabilization, and all prior production-safe fixes.

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
