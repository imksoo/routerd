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
| Status | Recommended stable release (supersedes v20260608.2325; Live ISO Ubuntu migration, SAMNodeSet/SAMSubnetPolicy, federation SLO, cloud-init bootstrap) |
| Track record | E2E validation pending (codex-lab results awaited) |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260619.1730 is recommended

This is a major milestone: the Live ISO migrates from Alpine to Ubuntu, SAM gains first-class fabric primitives, and federation observability becomes SLO-driven. 167 commits since v20260608.2325.

### Live ISO: Alpine → Ubuntu (#556–#573)

The ISO is rebuilt from Ubuntu 24.04.4 via `debootstrap`. Interface names switch to predictable naming (`ens18`, `ens19`, …). Cloud-init, IMDS (AWS/Azure/OCI/GCP), and NoCloud data sources bootstrap `router.yaml` and SSH keys at first boot. Serial console login with empty-password root is enabled.

### SAMNodeSet + SAMSubnetPolicy (#347–#354)

`SAMNodeSet` is a write-once node identity registry for SAM fabrics. Transport peers, event peers, and WireGuard peers derive dynamically from the node set. `SAMSubnetPolicy` distributes subnet-level shards across placement group members. `samEndpointFrom` resolves underlay endpoints from `SAMNodeSet` status (#527, #603).

### Ownership resolver + capture strategy (#393–#434)

A new ownership resolver replaces legacy helpers. Per-provider capture strategies (secondary-ip for AWS/Azure, route-table for OCI) are separated. No-preempt failover ensures restored nodes do not preempt equal-priority peers.

### Federation SLO + doctor federation (#537–#541)

`FederationSLO` Kind declares per-EventGroup thresholds. `routerctl doctor federation` runs 19 checks with `--remediation-plan` for typed action constants. 14 OTel metrics in `routerd-eventd` cover delivery, receiver, and loop health.

### CI pipeline acceleration (#590–#597)

Release builds run in parallel with quality checks. ISO rootfs caching on main and streaming uploads reduce end-to-end release time.

### Inherited from v20260608.2325

All properties from v20260608.2325 are carried forward: peersFrom/membersFrom dynamic distribution, peer-group-sync, pair-stable addressing, ADR 0014 CLI redesign.

## Known observations (not release blockers)

- **`routerd-bgp` may keep running with the old executable inode after `install.sh`.** This is intentional: `install.sh` does not restart `routerd-bgp` on upgrade so established BGP sessions and ECMP survive the routerd binary update.
- **`routerctl doctor mgmt` SKIPs when no `ManagementAccess` is declared.** This is a live-config choice, not a release defect.

:::warning Upgrading
- **From v20260608.2325 or earlier Alpine ISO:** Network interface names changed from `eth0`/`eth1` to `ens18`/`ens19` (systemd predictable naming). Update `router.yaml` interface references before migrating to the new ISO.
- **Alpine Linux and NixOS are no longer supported.** Ubuntu and FreeBSD are the only supported platforms.
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
