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
| Version | **v20260525.1631** |
| Status | Recommended stable release (supersedes v20260525.0112) |
| Track record | Running in production on a home router (homert02); 2-way ECMP over BGP is maintained, the DNS resolver keeps serving across routerd restarts, and the binary upgrades with zero downtime via graceful restart |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260525.1631 is recommended

- **DNS keeps serving across routerd restarts.** `DNSResolver` runs as its own long-lived service unit (`routerd-dns-resolver@<name>.service`): restarting or upgrading routerd no longer interrupts DNS, config changes (including DHCPv6-PD convergence) apply in place through the daemon's reload endpoint without a process restart, and `routerctl restart-dns-resolver` provides explicit recovery. It also brings up partially at boot — serving the listen addresses and sources that already resolve (`phase: Degraded` with a `waiting` list) and converging to `Applied` — so there is no boot-time window where DNS is refused while waiting on a prefix delegation.
- **The complete BGP control plane.** routerd runs its own `routerd-bgp` daemon (no FRR); the next-hop rewrite fix (#26) keeps 2-way ECMP even when an upstream advertises a third-party next-hop, and the Alpine/OpenRC live ISO starts `routerd-bgp` under OpenRC (#28).
- **Upgrades no longer disturb BGP or DNS.** `install.sh` no longer auto-restarts `routerd-bgp` or the DNS resolver on a binary upgrade, so eBGP sessions, ECMP, and DNS survive routerd updates.
- **Easier operations.** `routerd rollback --list` / `--to <generation>` re-applies a stored config generation, `routerctl set-log-level` changes log verbosity at runtime, and `routerctl describe` reports Phase, Reason, and Message with remediation hints.
- **Non-root status access.** The read-only status socket is owned `root:routerd` with mode `0o660`, so operators in the `routerd` group can run `routerctl status` without sudo.
- **It runs in production** (home router homert02), ships as a static binary (`CGO_ENABLED=0`), and passes CI and the Release workflow.

:::warning Upgrading
- **From v20260523.1542 or earlier:** the `disabled:` field was removed (use `enabled: false`) along with the no-op `--controller-chain*` / `--observe-interval` flags. Re-author affected config and host service units before upgrading.
- **DNS resolver service unit:** the resolver now runs as `routerd-dns-resolver@<name>.service`. The first upgrade onto this model performs a one-time child-process → unit cutover with a brief DNS blip; afterwards routerd restarts and upgrades no longer interrupt DNS.
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
