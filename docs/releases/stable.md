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
| Version | **v20260608.0642** |
| Status | Recommended stable release (supersedes v20260528.2308; ADR 0014 CLI redesign — `routerd` becomes daemon-only, `routerctl` becomes the admin CLI. OpenRC supervision hardening, DNS resolver VRRP VIP support, forcefrag prerouting fix, BGP peer watch stabilization) |
| Track record | Validated on lab environments (router06/router07/k8s-rt-01/k8s-rt-02) and production router (homert02). Cloud VM tests (lab + k8s) all PASS. 12 issues resolved, 12 PRs merged |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260608.0642 is recommended

This release inherits all production-safe properties of v20260528.2308 and adds **CLI redesign** (ADR 0014) plus **OpenRC / init script reliability hardening** across 40 commits.

### ADR 0014 — CLI redesign

The routerd CLI has been cleanly split into "daemon" and "admin tool".

- **`routerd`** is daemon-only. The sole subcommand is `routerd serve`.
- **`routerctl`** is the admin CLI: `validate` / `plan` / `apply` / `doctor` / `get` / `describe` / `status` / `ledger` / `dns-queries` / `traffic-flows` and all other management operations.
- Legacy `routerd apply` / `routerd validate` / `routerd run` are removed. The `--once` flag is also retired.
- All documentation and script command references updated to the new verb surface (#254–#262).

### OpenRC / init script reliability

Six fixes applied to init script management on FreeBSD and OpenRC environments.

- **Eliminated OpenRC DNS resolver dual management** (#306) — previously both `routerd serve` and OpenRC attempted to manage the DNS resolver, causing double starts.
- **Stop old `routerd serve` on OpenRC upgrade** (#311, #313) — fixed stale processes surviving upgrades.
- **Clean managed helpers on OpenRC restart** (#315) — prevents orphan helper process accumulation.
- **DNS resolver helper supervision** (#283) — OpenRC now correctly monitors and starts DNS resolver helper processes.
- **Stale helper updates** (#280) and **nodeps OpenRC restart** (#278) — resolved service dependency issues during upgrades.

### Networking improvements

- **DNS resolver can listen on VRRP VIPs** (#319) — `IP_FREEBIND` / `IPV6_FREEBIND` socket options allow listeners to bind addresses not yet assigned. DNS service can be pre-started on VRRP backup nodes.
- **forcefrag DF clearing moved to prerouting hook** (#328) — the forward hook used `oifname` which is unavailable in prerouting; replaced with `fib daddr oifname` for routing table lookup. Fixes cases where MSS clamp was not applied correctly.
- **BGP peer watch spurious updates eliminated** (#329) — `desiredPeerMatches()` used `reflect.DeepEqual`, triggering `UpdatePeer` on every reconcile due to `dynamicExportPrefixes` changes and GracefulRestart format mismatch (`"2m"` vs `"120s"`). A stable comparison function `stableDesiredPeerEqual` now suppresses updates when configurations are semantically identical.
- **`routerd serve` auto-enables loopback at startup** (#321) — runs `ip link set lo up` on Live ISO and container environments where `lo` may be down.

### Installer improvements

- **Bootstrap installer reliably cleans up temp directories** (#324) — `exec sh ./install.sh` prevented the EXIT trap from firing; fixed.
- **Installer apply state warning fixed** (#327) — changed `routerctl get status` output format to `-o json` for accurate `lastApplyTime` detection.
- **BGP peer state watch for immediate status updates** (#304) — BGP session state changes are reflected in status immediately.
- **Restart inactive keepalived for VRRP** (#299) — fixes VRRP failover in certain edge cases.

### Documentation

- **37 Japanese source-of-truth articles + 80 Chinese translation articles added** (#322) — covers all categories: ADR / explainer / how-to / ops / reference / releases / evidence / slides. Japanese as source, zh-Hans / zh-Hant as translations.
- **All documentation diagrams regenerated with gpt-image-2** (#261) — unified visual style.

### Inherited from v20260528.2308

All production-safe properties from v20260528.2308 are carried forward.

- fd leak fixes (#39 SQLite ledger, #40 Unix socket / BGP gobgp client)
- Heap leak fixes (OTel instrument singleton, bounded reverse DNS cache)
- `routerctl doctor runtime` for ongoing resource monitoring
- BGP sessions survive routerd binary upgrades
- `doctor dslite` selectedSource alignment
- Gateway Health dedicated screen
- `install.sh` fail-fast (missing payload detection)
- Secret redaction
- `ManagementAccess` apply guard
- Machine-readable `routerctl doctor` (`-o json`)

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
