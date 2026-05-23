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
| Version | **v20260522.1334** |
| Status | First recommended stable milestone |
| Track record | Running in production (verified 2-way ECMP over BGP and a zero-downtime upgrade) |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI |

## Why v20260522.1334 is recommended

- **The BGP control-plane migration is complete.** routerd moved from FRR to its own `routerd-bgp` daemon, which now holds the eBGP peers directly.
- **The next-hop rewrite defect (#26) is fixed.** Even when an upstream advertises a third-party next-hop, routes are installed into the kernel FIB via the learned peer address, preserving 2-way ECMP.
- **It has a production track record.** Upgrading to 1334 completed with no observed outage.
- **It ships as a static binary** and passes CI (build, tests, and the Release workflow).

## What "stable" means here

:::warning The API is still v1alpha1
A "stable milestone" means **this build is production-quality**. It does **not** promise backward compatibility of the API (resource schema).
:::

- The routerd resource API is currently **v1alpha1**. **Breaking changes can land between releases.**
- When upgrading, do not rely on backward compatibility. Plan to **rewrite your configuration (YAML) against the new schema**.
- There is no migration shim by policy. Review the per-release deltas in the [changelog](./changelog.md).

## Install and upgrade

See [Install and upgrade](../install-and-upgrade.md) for the procedure. Start upgrades from a recommended milestone release.
