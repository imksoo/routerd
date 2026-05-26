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
| Version | **v20260526.1607** |
| Status | Recommended stable release (supersedes v20260525.1631) |
| Track record | Production-validated on a home router (homert02): DNS keeps serving through routerd restart/install (NG 0), `/api/v1/config` exposes 0 raw secrets, `gatewayHealth` overall=ok across 26 components, `routerctl doctor` rc=0 (pass=32 warn=4 fail=0 skip=1), and BGP 2/2 + 2-way ECMP preserved across binary install |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260526.1607 is recommended

The recommendation is **operational maturity, not feature scope.**
v20260526.1607 inherits the production-safe DNS and BGP upgrade behavior of
the prior recommended build and adds four operator-facing contracts that have
been validated on a real production home router (homert02):

- **Web Console no longer leaks secrets.** `/api/v1/config` and the
  generation-config / diff endpoints redact WireGuard `privateKey` /
  `preSharedKey`, Tailscale `authKey`, BGP/PPPoE/IPsec `password`,
  WebConsole `initialPassword`, and bearer/token fields before serializing.
  Marker values preserve key structure for the UI. Validated on homert02:
  **0 sensitive-key detections** in the actual response.
- **`gatewayHealth` aggregates the whole egress path.** `/api/v1/summary`
  now unifies DNSResolver, DSLiteTunnel, DHCPv6PrefixDelegation,
  EgressRoutePolicy, NAT44Rule, and HealthCheck. The Web Console banner
  surfaces selected vs preferred egress path with a visible warning when
  a fallback candidate is in use. Validated on homert02: **overall=ok,
  26 components**.
- **`routerctl doctor` is a stable machine-readable contract.** `-o json`
  output is documented as a v1alpha1 contract (areas, status enum, summary
  fields, exit code); non-zero exit on fail makes it scriptable. Validated
  on homert02: **rc=0 (pass=32 warn=4 fail=0 skip=1)**.
- **`ManagementAccess` declarative apply guard.** Apply preflight fails
  (unless `--allow-mgmt-lockout`) when a declared management interface is
  missing, when the firewall would drop SSH to it, or when WebConsole binds
  to all addresses — the documented v1alpha1 way to prevent lockout, also
  surfaced by `routerctl doctor mgmt`.

**Carry-forward (from v20260525.1631 etc.):** the DNS resolver runs as its
own long-lived service unit so routerd restart/upgrade does not interrupt
DNS (homert02 validation: 0 DNS probe failures during `routerd.service`
restart and during install). `install.sh` does not auto-restart
`routerd-bgp` on upgrade so eBGP sessions and ECMP survive routerd binary
updates (homert02 validation: 2/2 Established, 2-way ECMP, HTTP 200
throughput across install). Complete BGP control plane (no FRR; #26 next-hop
rewrite, #28 OpenRC live-ISO start). `routerctl ledger` maintenance
(`integrity-check` / `vacuum` / `backup` / `prune-events`, with an audit
event on each non-dry-run prune).

## Known observations (not release blockers)

- **DS-Lite doctor may WARN while egress is healthy.** When AFTR AAAA
  probing or tunnel-device observation is intermittently noisy, doctor's
  `dslite` area can report WARN even though `gatewayHealth=ok` and real
  egress (HTTP 200) succeeds. This is conservative diagnostic noise, not
  a dataplane failure. Future tuning will align DS-Lite doctor severity
  with `gatewayHealth` selected-path evidence.
- **`routerd-bgp` may keep running with the old executable inode after
  `install.sh`.** This is intentional: `install.sh` does not restart
  `routerd-bgp` on upgrade so established BGP sessions and ECMP survive
  the routerd binary update. The running process keeps the old inode until
  the operator picks a graceful-restart window and runs
  `systemctl restart routerd-bgp`.
- **`routerctl doctor mgmt` SKIPs when no `ManagementAccess` is declared.**
  This is a live-config choice, not a release defect — the guard is
  opt-in. To activate the apply lockout protection and the doctor mgmt
  verdict, declare a `ManagementAccess` resource (see
  [`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)).

:::warning Upgrading
- **From v20260523.1542 or earlier:** the `disabled:` field was removed
  (use `enabled: false`) along with the no-op `--controller-chain*` /
  `--observe-interval` flags. Re-author affected config and host service
  units before upgrading.
- **DNS resolver service unit:** the resolver now runs as
  `routerd-dns-resolver@<name>.service`. The first upgrade onto this
  model performs a one-time child-process → unit cutover with a brief
  DNS blip; afterwards routerd restarts and upgrades no longer interrupt
  DNS.
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
