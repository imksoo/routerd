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
| Version | **v20260523.1542** |
| Status | Recommended stable release (supersedes v20260522.1334) |
| Track record | Running in production on a home router (homert02); 2-way ECMP over BGP is maintained, and the binary upgrades with zero downtime via graceful restart |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260523.1542 is recommended

- **It carries the complete BGP control-plane work from v20260522.1334.** routerd runs its own `routerd-bgp` daemon (no FRR), and the next-hop rewrite fix (#26) keeps 2-way ECMP even when an upstream advertises a third-party next-hop.
- **It fixes live-ISO BGP (#28).** On the Alpine/OpenRC live ISO, the managed GoBGP daemon (`routerd-bgp`) now starts under OpenRC, so BGP works from the live ISO. v20260522.1334 had this broken — so 1334 is no longer recommended, especially if you run BGP from the live ISO.
- **It adds the built-in DPI classifier and NixOS renderer fixes.**
- **It runs in production** (home router), ships as a static binary, and passes CI.

## What "stable" means here

:::warning The API is still v1alpha1
A "stable milestone" means **this build is production-quality**. It does **not** promise backward compatibility of the API (resource schema).
:::

- The routerd resource API is currently **v1alpha1**. **Breaking changes can land between releases.**
- When upgrading, do not rely on backward compatibility. Plan to **rewrite your configuration (YAML) against the new schema**.
- There is no migration shim by policy. Review the per-release deltas in the [changelog](./changelog.md).

## Install and upgrade

See [Install and upgrade](../install-and-upgrade.md) for the procedure. Start upgrades from a recommended milestone release.
