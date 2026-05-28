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
| Version | **v20260528.1805** |
| Status | Recommended stable release (supersedes v20260528.0402; adds the heap-leak fixes that complete the fd-leak work, validated by a 2-hour production soak) |
| Track record | Production-validated on a home router (homert02) with both fd and heap held bounded. fd stayed completely flat across a 2-hour / 24-sample soak (all_fd=24, sockets=16, SQLite ledger family=4 at every sample, NRestarts=0, PID unchanged). `RssAnon` (true heap, excluding page cache) warmed up from ~70 MB to a ~104 MB steady-state band over the first hour and then plateaued for the second hour, oscillating 96–107 MB with clear GC reclaim dips — the signature of a bounded working set, not a leak. BGP held 2/2 Established, `routerctl doctor dslite` returned pass=12 / warn=0, and `routerctl doctor reconcile` returned pass=1 / warn=0. Across the v20260528 series, three distinct fd-leak root causes (#39 SQLite ledger, #40 control/status socket keep-alive, #40 BGP gobgp client) and two heap-growth sources (per-request OTel instrument churn, unbounded reverse-DNS cache) were each hunted down and fixed |
| Binary | Statically linked (`CGO_ENABLED=0`), passes CI and the Release workflow |

## Why v20260528.1805 is recommended

The recommendation is **operational maturity, not feature scope.**
v20260528.1805 inherits every production-safe property of v20260528.0402
(the fd-leak fixes #39 / #40, the #36 / #37 / #38 observability contracts,
BGP idempotent reconcile, doctor dslite alignment, Gateway Health
dedicated screen, install.sh fail-fast, secret redaction,
ManagementAccess apply guard, machine-readable `routerctl doctor`, the
recommended-stable display consistency guard) and adds the heap-leak
fixes that close out the long-running resource-leak investigation:

- **`/api/v1/summary` polling no longer grows the heap unbounded.**
  `recordConsoleMetrics` used to re-create seven OpenTelemetry gauges on
  every request; they are now built once via a `sync.Once` singleton
  (`getConsoleMetrics`). The `reverseDNSCache` only used its TTL to
  decide re-lookup, never pruning expired entries or capping size, so
  every distinct remote address seen in firewall logs / the connection
  table / traffic flows became a permanent map entry; it now prunes
  expired entries and enforces a 4096-entry hard cap on both call entry
  and call exit. A 2-hour homert02 soak confirmed `RssAnon` plateaus
  rather than climbing. These complete the v20260528.0402 fd-leak work
  with the matching heap-side fixes.

The two production-critical fd-leak fixes and three observability
contracts carried forward from v20260528.0402 and re-verified on
homert02 v20260528.1805:

- **routerd serve no longer leaks SQLite ledger fds.** `resource.LoadLedger`
  used to open a fresh `*sql.DB` against `/var/lib/routerd/routerd.db` on
  every call, and `Ledger` had no `Close()`. The
  `IPv4PolicyRouteController.cleanupLedgerOwnedPolicyRoutes` reconcile path
  ran every ~30 s and added one new `routerd.db` + one new
  `routerd.db-wal` fd per cycle — homert02 v20260526.2335 had grown to
  ~300 SQLite fds. The fix adds `Close()` to the `Ledger` interface,
  defers it at every `LoadLedger` call site, and sets
  `SetMaxOpenConns(1)` / `SetMaxIdleConns(1)` on `OpenSQLiteLedger` as a
  belt-and-suspenders cap. Two Linux-only regression tests assert
  `/proc/self/fd` does not grow across 10 open/close cycles. Validated:
  homert02 saw `routerd.db` family drop from ~300 to a flat 4 (#39).

- **routerd serve no longer leaks Unix-socket fds either.** Two separate
  issues, both fixed: (a) the control / status `http.Server` instances
  now call `SetKeepAlivesEnabled(false)`, and `controlapi.NewUnixClient`
  sets `Transport.DisableKeepAlives: true` — accepted connections used
  to stay open indefinitely when polling clients reused the keep-alive
  channel inside `IdleTimeout`. (b) The BGP controller's gobgp
  HTTP client (`pkg/controller/bgp/gobgp_client.go`), called twice per
  ~30 s reconcile against `/run/routerd/bgp/control.sock`, was the only
  in-tree HTTP client missing the `DisableKeepAlives` / `req.Close` /
  `defer CloseIdleConnections()` pattern; it accounted for the
  remaining +4 fd / minute drift. Validated: homert02 v20260528.0402
  ran 16 minutes with `all_fd=24` and `sockets=16` completely flat at
  every 5-minute sample, and Unix-stream ESTAB dropped from 71 to 9
  (#40).

- **HealthCheck probes now record egress / source / route evidence and
  keep a rolling per-resource failure history.** Every result carries
  `FailureKind` (timeout / connection_refused / network_unreachable /
  host_unreachable / no_route / dns_error / tls_error / ...),
  `EgressInterface`, `SourceAddress`, `SourceOrigin` (pd / ra / static /
  dynamic), `NextHop`, `OutInterface`, `RouteSource`, `TunnelLocal`,
  `TunnelRemote`. `State` exposes `FirstFailureTime`, `LastFailureTime`,
  `LastSuccessTime`, `FailureCount`, and a configurable 20-entry
  `History []ProbeRecord`. `cmd/routerd-healthcheck` gains
  `--source-origin` / `--tunnel-local` / `--tunnel-remote` operator
  hints so the daemon can label what the probe cannot infer. Event
  attributes and the existing `StatusMap` carry the new fields so
  `routerctl show / describe` surface them automatically (#37).

- **Per-controller reconcile error history surfaced via control API.**
  `ControllerStatus` gains `ReconcileErrorHistory []ReconcileErrorEntry`
  and `MaxDurationAt *time.Time`. Each entry records `StartedAt` /
  `CompletedAt` / `Duration` / `DurationMs` / `Trigger` /
  `ResourceKind` / `ResourceName` / `Error`. The controller framework
  gains an optional `ResourceObserver` interface to plumb resource
  kind / name from each reconcile into the history without touching
  existing in-tree observers. `routerctl status --show-errors` renders
  the history vertically under each controller row in table mode;
  JSON / YAML pick up the new fields via the existing StatusMap.
  New `routerctl doctor reconcile --since <duration>` queries the
  status socket and reports pass / warn (≥1) / fail (≥10) with up to
  5 sample entries in detail. Validated on homert02 v20260528.0402:
  `doctor reconcile` returns `pass=1 warn=0`, machinery live in
  production (#38).

- **dns-queries / traffic-flows gain absolute-time range, filters, and
  aggregation.** `--from` / `--to` accept RFC3339 and other common
  forms (bare layouts treated as UTC). DNS gains `--rcode`,
  `--upstream`, `--qname-suffix`, `--duration-min`; flows gain
  `--peer-suffix`, `--protocol`, `--asymmetric`. New `--agg` /
  `--stats` mode emits `SUMMARY` plus `BY RESPONSE CODE` /
  `BY CLIENT` / `BY UPSTREAM` / `BY QNAME SUFFIX` (DNS) or
  `BY CLIENT` / `BY PEER` / `BY PROTOCOL` (flows) with duration p50 /
  p95 / p99. Direct-DB fetch is chunked (`--chunk-size`) so each chunk
  gets its own ctx deadline; on `DeadlineExceeded` the error message
  includes how many rows were fetched so far. Default `--limit`
  raised from 100 to 500, `--timeout` from 5 s to 30 s, and the
  underlying `DNSQueryFilter` / `TrafficFlowFilter` hard-cap raised
  from 1000 to 10000. Web Console gains
  `/api/v1/dns-queries/aggregate` and
  `/api/v1/traffic-flows/aggregate` endpoints (#36).

The doctor-detail, --help, and CI-display contracts carried forward
from v20260526.2335 and re-verified against homert02 v20260528.0402:

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
