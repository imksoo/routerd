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
| Version | **v20260525.0112** |
| Status | Recommended stable release (supersedes v20260523.1542) |
| Track record | Running in production on a home router (homert02); 2-way ECMP over BGP is maintained, and the binary upgrades with zero downtime via graceful restart |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260525.0112 is recommended

- **No boot-time DNS gap.** `DNSResolver` now brings the daemon up partially: it serves with the listen addresses and forward sources that already resolve, reports `phase: Degraded` with a `waiting` list while the rest are pending, and converges to `Applied` once a DHCPv6 prefix delegation arrives. Earlier builds refused DNS during the startup window while waiting on PD.
- **The complete BGP control plane.** routerd runs its own `routerd-bgp` daemon (no FRR); the next-hop rewrite fix (#26) keeps 2-way ECMP even when an upstream advertises a third-party next-hop, and the Alpine/OpenRC live ISO starts `routerd-bgp` under OpenRC (#28).
- **Upgrades no longer disturb BGP.** `install.sh` no longer auto-restarts `routerd-bgp` on a binary upgrade, so eBGP sessions and ECMP survive routerd updates.
- **Easier operations.** `routerd rollback --list` / `--to <generation>` re-applies a stored config generation, `routerctl set-log-level` changes log verbosity at runtime, and `routerctl describe` reports Phase, Reason, and Message with remediation hints.
- **Non-root status access.** The read-only status socket is owned `root:routerd` with mode `0o660`, so operators in the `routerd` group can run `routerctl status` without sudo.
- **It runs in production** (home router homert02), ships as a static binary (`CGO_ENABLED=0`), and passes CI and the Release workflow.

:::warning Upgrading from v20260523.1542 or earlier
This milestone removed the `disabled:` field (use `enabled: false`) and the no-op `--controller-chain*` / `--observe-interval` flags. Re-author any config that used `disabled:`, and update host service units that still pass the removed flags before upgrading.
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
