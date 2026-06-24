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
| Version | **v20260608.2325** |
| Status | Recommended stable release (supersedes v20260608.1354; peersFrom/membersFrom dynamic distribution for zero-touch leaf config) |
| Track record | Validated on k8s cluster (10 nodes: 2 RR + 8 leaf, peersFrom + membersFrom + peer-group-sync all green, full verify passed), lab (FreeBSD router01/04 upgrade verified), and production router (homert02, validate pass). 0 open issues |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260608.2325 is recommended

This release adds **peersFrom**, **membersFrom**, and **peer-group-sync** on top of v20260608.1354, enabling zero-touch leaf configuration for SAM fabrics.

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
