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
| Version | **v20260526.2335** |
| Status | Recommended stable release (supersedes v20260526.2241; doc/CI consistency follow-up — no runtime behavior change) |
| Track record | Production-validated on a home router (homert02) across **three successive in-place upgrades** (1607 → 2152 → 2241 → 2335): every routerd restart left `routerd-bgp` untouched (MainPID 2394269 unchanged across all four hops), BGP stayed 2/2 Established with uptime climbing through every upgrade (1h19m → 1h27m → 2h0m → 2h15m → 3h7m → 3h10m, never reset), 2-way ECMP via .38/.53 stayed in the kernel, `routerctl doctor dslite` finished at pass=12 warn=0, the Web Console Gateway Health page recorded good=90 / bad=0 over 180s, and `install.sh` exited rc=0 with the correct cd-into-package-dir pattern |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260526.2335 is recommended

The recommendation is **operational maturity, not feature scope.** v20260526.2335
inherits every production-safe property of v20260526.2241 (which itself inherited
v20260526.1607's Web Console secret redaction, `gatewayHealth` aggregation,
machine-readable `routerctl doctor`, and `ManagementAccess` apply guard) and
adds one documentation/CI hardening on top of the five v20260526.2241
contracts observed in real production on homert02:

- **The recommended-stable display cannot silently drift.** A new CI guard
  (`scripts/check-active-stable.sh`) reads `STABLE_VERSION` from
  `website/src/pages/index.tsx` and fails when the homepage hero, the
  per-locale intro tip, the announcement bar, or `docusaurus.config.ts`
  point at a different `vYYYYMMDD.HHmm`. The `v20260526.2241` promotion
  had left the homepage hero and four intro tips at `v20260526.1607`; the
  guard now prevents that class of split state from re-emerging in
  future promotions.

The five operationally-significant contracts carried forward from
v20260526.2241 and validated again across the 2335 apply on homert02:

- **BGP sessions survive routerd binary upgrades.** The BGP controller now
  hydrates its in-memory applied-policy state on reconcile, so a routerd
  restart no longer re-PUTs the unchanged import-policy assignment and resets
  every BGP session. Validated on homert02 across **two consecutive routerd
  restarts** (PID 3368318 → 3407972 → 3428160): BGP stayed 2/2 Established
  the whole way, uptime climbed through every restart instead of resetting,
  and 2-way ECMP via .38/.53 stayed in the kernel without re-installation.
- **`routerctl doctor dslite` aligns with reality.** Doctor now treats
  DSLiteTunnel `phase=Up` as healthy and recognizes EgressRoutePolicy
  selection through `status.selectedSource = "DSLiteTunnel/<name>"` in
  addition to the legacy `selectedCandidate` match. Production configurations
  using aggregate candidate names (`dslite-pd-balanced` on homert02) no
  longer drive WARNs while `gatewayHealth` reports `ok`. Validated: warn=4
  → pass=12 warn=0.
- **Gateway Health UI is a dedicated screen with stable rendering.** The
  Web Console moves Gateway Health off the Overview into its own screen
  (mirroring Connections/Clients) with full evidence (`selectedPath`,
  `preferredPath`, `fallbackReason`, `failedProbes`, `lastTransition`).
  Overview keeps a compact summary card. A thin-snapshot bug that briefly
  flashed `Components 0 / Unknown` during partial refreshes is fixed:
  `reconcileSummary` keeps the previous `gatewayHealth` when the incoming
  snapshot has no components but the previous one did. Validated:
  **good=90 / bad=0 over 180s, 26 components seen**.
- **`install.sh` cannot silently no-op.** Earlier installers would exit 0
  saying `routerd upgrade completed` even when launched from outside the
  release tree (`cd /tmp/release && ./pkg/install.sh ...`): the cwd-relative
  `bin/*` glob ran zero iterations and only `--with-ndpi-archive` payloads
  landed. The script now refuses to proceed with `exit 2` and a clear
  diagnostic when cwd has no `bin/routerd` payload, and a CI regression
  smoke (`scripts/install-sh-cwd-smoke.sh`) reproduces both the
  missing-payload and correct-cwd cases. Validated on homert02:
  cwd-mismatch antipattern **fails fast rc=2**; correct cd-into-package-dir
  pattern returns rc=0.

**Carry-forward (from v20260526.1607 etc.):** Web Console `/api/v1/config`
and generation endpoints redact WireGuard `privateKey` / `preSharedKey`,
Tailscale `authKey`, BGP/PPPoE/IPsec `password`, WebConsole
`initialPassword`, and bearer/token fields before serializing.
`/api/v1/summary` aggregates DNSResolver, DSLiteTunnel,
DHCPv6PrefixDelegation, EgressRoutePolicy, NAT44Rule, and HealthCheck into
`gatewayHealth`. `routerctl doctor` is a v1alpha1 machine-readable
contract (`-o json`, documented areas / status enum / summary fields,
non-zero exit on fail). `ManagementAccess` apply preflight blocks lockout
unless `--allow-mgmt-lockout`. The DNS resolver runs as its own
long-lived service unit so routerd restart/upgrade does not interrupt
DNS (0 probe failures during install). `install.sh` does not auto-restart
`routerd-bgp` on upgrade so eBGP sessions and ECMP survive routerd binary
updates. `routerctl ledger` maintenance (`integrity-check` / `vacuum` /
`backup` / `prune-events`, with an audit event on each non-dry-run prune).

## Known observations (not release blockers)

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
- **Always `cd` into the extracted release directory before running
  `install.sh`.** Running it from a sibling directory (for example
  `cd /tmp && sudo ./routerd-release-vYYYYMMDD.HHmm/install.sh ...`) will
  now refuse to proceed with `exit 2`. This is intentional — earlier
  versions silently no-op'd in that case and only installed
  `--with-ndpi-archive` payloads.
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
