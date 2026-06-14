---
title: Changelog
---

# Changelog

routerd release history. The format follows [Keep a Changelog](https://keepachangelog.com/).
Changes are grouped under Added, Changed, Deprecated, Removed, Fixed, and Security.
Versions, however, do not follow Semantic Versioning; routerd uses date-and-time-based
release versions in `vYYYYMMDD.HHmm` format.
The software is at the v1alpha1 stage; releases may contain breaking changes.

## Unreleased

### Added

- `routerctl doctor routes` compares installed `IPv4Route` status rows with
  the Linux host FIB and reports stale or mismatched destination, gateway,
  device, preferred-source, and metric drift as operator evidence (#439).

## v20260608.2325

### Added

- `SAMTransportProfile.spec.peersFrom` and `SAMPeerGroup` Kind for
  reusable transport peer references. Union semantics: `peersFrom`
  members load first, static `peers` override by `nodeRef` (#332, #333).
- `SAMTransportProfile.spec.publishPeerGroup` generates a `SAMPeerGroup`
  `DynamicConfigPart` on route-reflector nodes for automatic
  distribution to leaf routers (#332).
- SAM peer group sync: lightweight HTTP service on port 19652 over
  WireGuard inner network. Publisher serves `GET /v1/peer-groups`;
  consumer discovers WireGuard peers and fetches matching groups
  automatically. Eliminates manual `SAMPeerGroup` distribution (#334, #336).
- `MobilityMemberSet` Kind and `MobilityPool.spec.membersFrom` for
  shared identity-only pool member distribution. Leaves import the
  shared topology and keep only their own capture/discovery details
  inline, reducing O(N²) config duplication (#339, #340).
- `MobilityPool.spec.publishMemberSet` generates a `MobilityMemberSet`
  `DynamicConfigPart` on RR nodes; leaves fetch via
  `GET /v1/member-sets` on the same sync service (#340).

### Fixed

- FreeBSD/NixOS upgrade no longer fails when `/etc/rc.conf` contains
  legacy `routerd serve` flags (`--observe-interval`,
  `--controller-chain*`). Stale flags are accepted and ignored with a
  warning (#337, #338).

## v20260608.1354

### Added

- `SAMTransportProfile` pair-stable addressing mode
  (`spec.addressingMode: pair-stable`). Uses fnv64a hash of inner
  prefix and canonical peer key for /31 slot allocation, making
  addresses stable across node additions. Leaf nodes no longer require
  `topologyNodeRefs`; existing `edge-index` mode is unchanged (#330, #331).

## v20260608.0642

### Added

- **ADR 0014 — CLI redesign.** `routerd` is now daemon-only (`routerd serve`);
  all management operations moved to `routerctl` (`validate` / `plan` /
  `apply` / `doctor` / `get` / `describe` / `status` / `ledger` etc.).
  Legacy `routerd apply` / `routerd validate` / `routerd run` and `--once`
  removed (#254–#262).
- DNS resolver `IP_FREEBIND` / `IPV6_FREEBIND` support so listeners can
  bind VRRP VIP addresses before they are assigned (#319).
- `routerd serve` auto-enables loopback (`ip link set lo up`) at startup
  for Live ISO and container environments (#321).
- Bootstrap installer (`bootstrap.sh`) for curl-based one-liner installs (#295).
- Resource lifecycle registry and GC planner for deterministic teardown
  of derived artifacts on resource deletion (#222–#229, ADR 0014).
- Router config wizard: browser-based starter config generator with
  Home Router, SAM, and Kubernetes BGP profiles (#233, #236, #237, #239, #240).
- Generated JSON Schema published for YAML editor completion (#232).
- CloudEdge Selective Address Mobility Phase G: autonomous BGP /32
  address mobility across AWS, Azure, OCI, and on-prem sites. Mobility
  now runs over a WireGuard overlay with iBGP and an on-prem
  route-reflector; ownership is the BGP best path, liveness uses
  per-node marker /32s with identity communities, cloud traps are
  RIB-driven, and same-site standby seizure is liveness-driven. The
  data plane keeps NAT disabled, preserves source addresses, and leaves
  client default gateways unchanged. Cloud capture uses AWS ENI,
  Azure NIC `ipConfig`, and OCI VNIC secondary IPs; on-prem capture uses
  proxy ARP plus GARP, optionally gated by VRRP for HA pairs, with doctor
  split-brain checks failing deterministically.
- Pluggable overlay underlays through `TunnelInterface`, including
  IPIP, GRE, FOU, and GUE UDP encapsulation. WireGuard remains the
  default overlay transport. See ADR 0009.
- IPv4 force-fragment controls through
  `OverlayPeer.pathMTU.forceFragmentIPv4` and
  `TunnelInterface.pathMTU.forceFragmentIPv4`, defaulting off, for
  controlled PMTU blackhole mitigation. See ADR 0013.
- A more declarative `MobilityPool` authoring model with
  `profiles.cloudCaptures`, `spec.values`, `capture.targetFrom`,
  `ownershipDiscovery.subnetRefFrom`, `members[].profileRef`,
  self-complete local members, and identity-only remote peers.
- `MobilityPool` on-prem `proxy-arp` capture members can declare
  `capture.sourceAddress` or `capture.sourceAddressFrom` for
  BGP-mode capture-prefix route preferred source.
- Least-privilege CloudEdge IAM templates under
  `examples/cloudedge-mobility-demo/iam/` for scoped AWS, Azure, and
  OCI provider access.
- DHCP lease sync between DHCPv4Server and DHCPv6Server resources (#100, #107).
- NAT44 session sync for HA pairs (#106).
- Documentation: 37 Japanese source-of-truth articles + 80 Chinese
  (zh-Hans / zh-Hant) translation articles (#322). All documentation
  diagrams regenerated with gpt-image-2 (#261).

### Changed

- Mobility delivery now uses BGP best path as the single ownership
  plane. ADR 0012 records the clean Option B architecture and supersedes
  ADR 0006's earlier overlay-reachability source-of-truth model.
- `SAMTransportProfile` derives per-peer tunnel, BGP, and route
  resources from a shared topology declaration.

### Removed

- The AddressLease, ownershipEpoch, and heartbeat-event based mobility
  control plane was removed as part of the clean Option B migration.
- Legacy `routerd apply` / `routerd validate` / `routerd run` CLI
  entry points and the `--once` flag (ADR 0014).

### Fixed

- forcefrag DF clearing moved from forward hook to prerouting hook,
  using `fib daddr oifname` for routing lookup since `oifname` is
  unavailable in prerouting. Fixes MSS clamp not being applied in
  certain forwarding paths (#328).
- BGP peer watch no longer triggers unnecessary `UpdatePeer` calls.
  `desiredPeerMatches()` replaced `reflect.DeepEqual` with a stable
  comparison that ignores `dynamicExportPrefixes` changes and
  GracefulRestart format differences (`"2m"` vs `"120s"`) (#329).
- OpenRC DNS resolver dual management eliminated (#306); old
  `routerd serve` stopped on OpenRC upgrade (#311, #313); managed
  helpers cleaned on restart (#315); DNS resolver helper supervision
  added (#283); stale helper updates (#280); nodeps restart (#278).
- Bootstrap installer EXIT trap now fires reliably (#324).
- Installer apply state detection uses `routerctl get status -o json`
  for accurate `lastApplyTime` (#327).
- BGP peer state changes reflected in status immediately via watch (#304).
- Inactive keepalived restarted for VRRP failover (#299).
- The GoBGP backend now applies `BGPPeer.spec.exportPolicy.allowedPrefixes`
  as a peer export policy instead of accepting the field only in API
  validation (#95). Runtime changes trigger soft reset out (#98).
- `MobilityPool` on-prem `proxy-arp` capture now supports
  `capture.activeWhen.type: single-router` as an explicit always-active
  single-router mode, while keeping `vrrp-master` for HA pairs.
- `FirewallEventLog` readers and logger defaults now derive
  `firewall-logs.db` from the platform state directory, and Web Console
  dnsmasq lease candidates now include the platform managed lease path.
- Deleted resource stale status cleanup (#189).
- Lifecycle GC for derived artifacts on resource deletion (#222–#229).

## v20260528.2308

### Added

- `routerctl doctor runtime`: a new doctor area that reports routerd's own
  process footprint (heap, goroutine count, GC, open / max file
  descriptors) from a new read-only control-API `/runtime` endpoint. WARN
  when `numGoroutine` exceeds 10000 or open fds reach ≥80% of
  `RLIMIT_NOFILE`; observational, never FAILs. The endpoint is wired into
  both the control socket and the no-sudo read-only status socket, and the
  `routerctl doctor runtime -o json` shape is documented.

### Changed

- The Web Console Firewall "Deny activity" chart is now an explicit
  labeled bar chart instead of a bare unlabeled sparkline. One bar per
  5-minute bucket over 24h, with a Y axis (peak at top, 0 at the baseline,
  "taller = more denies"), an X axis ("24h ago" → "now"), an accessible
  `role="img"` label, and a "No denies in the last 24 hours" empty state.

### Fixed

- `reverseDNSCache.lookupMany` no longer spawns one goroutine per pending
  address. A fixed-size worker pool (`reverseDNSLookupConcurrency = 8`)
  bounds the goroutine count regardless of how many addresses a single
  `/api/v1/summary` resolves, and a new `reverseDNSPendingMax = 1000` caps
  the per-call work independent of the caller's own limit (excess addresses
  resolve on a later call). The `Options.ReverseLookup` contract now
  documents that implementations must honor ctx cancellation, and
  `RuntimeStats.OpenFDs` is documented as a sample-time approximate count.

## v20260528.1805

### Fixed

- `reverseDNSCache.lookupMany` now runs `pruneLocked` a second time
  after storing freshly resolved entries, so the
  `reverseDNSCacheMaxEntries` (4096) hard cap holds across every call
  boundary, not just at call entry. Previously a single request that
  resolved more new addresses than the free slot count could briefly
  push the cache past the cap until the next lookup pruned it; with
  the exit-time prune the invariant is maintained continuously. A
  new regression test
  (`TestReverseDNSCacheLookupManyEnforcesCapAfterStore`) pre-fills the
  cache to cap-100, resolves 200 brand-new addresses, and asserts the
  post-call size stays within the cap. This is the polish item from
  the external review of the v20260528.0832 heap-leak fixes; the
  underlying monotonic growth was already gone, and a 2-hour homert02
  soak confirmed `RssAnon` plateaus (~70 MB warm-up to a ~104 MB
  steady-state band with GC dips) while fd stays flat at all_fd=24 /
  sockets=16 / db_family=4 with NRestarts=0.

## v20260528.0832

### Fixed

- The Release workflow no longer treats a slow Web Console screenshot
  job as a release blocker. v20260528.0751 cut a real release commit
  and tag, but the screenshots job's 13 captures took 10 minutes 21
  seconds on the CI runner, the `timeout-minutes: 10` we'd added as
  protection against SSE-driven hangs (#40 era) fired, the Quality
  workflow reported failure, and the dependent build / publish jobs
  were skipped — so the binary the heap-leak fixes were meant to land
  in never reached GitHub Releases. Screenshots are a "nice to have"
  visual reference for the docs site, not a contract the routerd
  binary must honor. `webconsole-screenshot` now declares
  `continue-on-error: true` at the job level so its failure is
  reported but does not propagate into `needs: [quality]` on the
  Release workflow. The `Capture Web Console screenshots` step
  `timeout-minutes` is also raised from 10 to 15 minutes as a small
  cushion for slower runners. The `v20260528.0751` tag exists but
  was never published because of this hang — this release
  supersedes it with the same heap-leak fixes plus this CI guard.

## v20260528.0751

### Fixed

- `/api/v1/summary` polling no longer accumulates OpenTelemetry
  instrument allocations on every request. `recordConsoleMetrics`
  used to call `meter.Int64Gauge` / `meter.Float64Gauge` inside the
  request path, so the seven gauges (`routerd.controller.dry_run.count`,
  `routerd.controller.reconcile.errors`,
  `routerd.controller.reconcile.last_duration_ms`,
  `routerd.resource.phase.count`, `routerd.dhcp.lease.active`,
  `routerd.dhcp.sticky.held`, `routerd.client.active.count`) were
  re-constructed on every poll. They are now built exactly once via
  a `sync.Once` singleton (`getConsoleMetrics()`) and reused for the
  lifetime of the process. Combined with #39 / #40, this closes the
  remaining per-API-call heap growth path that summary polling
  produced.
- `reverseDNSCache` now drops expired entries and enforces an upper
  bound. Before this fix the cache only used its TTL to decide
  whether to re-lookup; expired entries stayed in the map, and every
  distinct remote address that appeared in firewall logs / the
  connection table / traffic flows added a permanent entry. The new
  `pruneLocked` removes expired entries on each `lookupMany`, and if
  the cache is still over `reverseDNSCacheMaxEntries = 4096` it drops
  the oldest-expiring entries until it is back under the cap. Two new
  tests (`TestReverseDNSCachePrunesExpiredEntries`,
  `TestReverseDNSCacheCapsAtMaxEntries`) lock in the behaviour.

## v20260528.0402

### Fixed

- `routerd serve` no longer leaks Unix socket file descriptors against
  `/run/routerd/bgp/control.sock` from the BGP controller's periodic
  reconcile (root cause for the residual fd growth still observed on
  homert02 v20260528.0325 even after the server-side
  `SetKeepAlivesEnabled(false)` fix). `pkg/controller/bgp/gobgp_client.go`
  was the only internal HTTP client whose Transport was missing
  `DisableKeepAlives: true`. Every BGP reconcile (~30 s) dialed the
  routerd-bgp control socket twice (AppliedConfig + SaveAppliedConfig)
  and left the connection in the Transport's idle pool until garbage
  collection, accounting for the steady +~4 fd / minute drift. The
  fix mirrors the conntrack-observer / dhcpv4-client pattern: set
  `DisableKeepAlives: true` on the Transport, set `req.Close = true`,
  and `defer client.CloseIdleConnections()` so the connection is gone
  before the next reconcile tick. Other in-tree internal HTTP clients
  (ingressservice, conntrackobserver, dhcpv4client, chain, phase2,
  pppoesession, dnsresolver) were already on that pattern and were
  audited as part of this fix.

## v20260528.0325

### Added

- HealthCheck probes now record egress / source / route evidence on
  every result and keep a rolling per-resource history (#37).
  `pkg/healthcheck`'s `State` gains `FirstFailureTime`,
  `LastFailureTime`, `LastSuccessTime`, `FailureCount`,
  `History []ProbeRecord`, and `LastEvidence`. Each `ProbeRecord` /
  `ProbeEvidence` carries `FailureKind` (timeout /
  connection_refused / network_unreachable / host_unreachable /
  no_route / dns_error / tls_error / address_in_use / permission /
  other), `EgressInterface`, `SourceAddress`, `SourceOrigin`
  (pd / ra / static / dynamic), `NextHop`, `OutInterface`,
  `RouteSource`, `TunnelLocal`, `TunnelRemote`. Linux probes call
  `ip -j route get` for nexthop / oif / src; non-Linux stubs keep
  cross-compile clean. `cmd/routerd-healthcheck` adds
  `--source-origin`, `--tunnel-local`, `--tunnel-remote` operator
  hints so the daemon can label evidence it cannot infer. Event
  attributes (`routerd.healthcheck.failureKind`,
  `network.egress.interface`, `network.source.address`,
  `network.source.origin`, `network.nexthop.address`,
  `network.out.interface`, `network.route.source`,
  `network.tunnel.local`, `network.tunnel.remote`, plus
  `lastSuccessAt` / `lastFailureAt` / `firstFailureAt` /
  `failureCount`) and `StatusMap` carry the new fields, so
  `routerctl show / describe` already surface them via the existing
  status map. History defaults to 20 entries, configurable via
  `ROUTERD_HEALTHCHECK_HISTORY`.
- Per-controller reconcile error history surfaced through the
  control API (#38). `ControllerStatus` gains
  `ReconcileErrorHistory []ReconcileErrorEntry` and
  `MaxDurationAt *time.Time`. Each `ReconcileErrorEntry` records
  `StartedAt`, `CompletedAt`, `Duration`, `DurationMs`, `Trigger`,
  `ResourceKind`, `ResourceName`, `Error`. The controller framework
  gains an optional `ResourceObserver` interface so the runtime
  store can plumb resource kind / name from each reconcile through
  to the history entry without touching existing in-tree observers.
  History is in-memory only (per the issue's out-of-scope clause),
  capped at 20 entries per controller, settable via
  `SetErrorHistoryLimit`. `routerctl status --show-errors` renders
  the history as a vertical block under each controller row in
  table mode; JSON / YAML output pick up the new fields via the
  existing StatusMap path. New `routerctl doctor reconcile --since
  <duration>` queries the read-only status socket and reports
  pass / warn (≥ 1 error in window) / fail (≥ 10 errors) with up to
  5 sample entries in detail. `parseDiagnoseOptions` gained the
  corresponding `--since` and `--status-socket` flags.

### Fixed

- `routerd serve` no longer leaks Unix-socket file descriptors on
  the control or status endpoints even when polling clients fire
  every < `IdleTimeout` seconds (follow-up to #40). The
  v20260528.0244 attempt at fixing #40 only set timeouts; idle
  timeout never fired because polling kept the keep-alive
  connection technically non-idle, so on homert02 v20260528.0244
  the routerd.db fds stayed flat at 4 (per #39) but `all_fd` still
  climbed +4 / minute. The new fix calls
  `http.Server.SetKeepAlivesEnabled(false)` on both internal API
  servers, and `controlapi.NewUnixClient` now sets
  `Transport.DisableKeepAlives: true`. Every request closes its
  connection after the response, so socket fd cannot drift upward
  over long uptime. Read / write / idle timeouts remain as
  belt-and-suspenders for malformed peers. Unix-socket accept is
  cheap; this is a per-request close, not a re-dial penalty in any
  hot path.

## v20260528.0244

### Fixed

- `routerd serve` no longer leaks Unix-socket file descriptors on the
  control (`/run/routerd/routerd.sock`) and read-only status
  (`/run/routerd/routerd-status.sock`) endpoints (#40). Both
  `http.Server` instances previously only set `ReadHeaderTimeout`, so
  accepted connections from polling clients (routerctl, webconsole,
  internal daemons) stayed open indefinitely. After this fix, all
  three socket-level deadlines are bounded the same way the Web
  Console HTTP server already was: `ReadTimeout: 30 s`,
  `WriteTimeout: 60 s`, `IdleTimeout: 2 min`. Neither socket exposes
  Server-Sent Events, so a strict `WriteTimeout` is safe.
  Production observation on homert02 v20260528.0158 showed
  `routerd.db` ledger fds flat at 4 (per #39) while `all_fd` still
  climbed from 41 to 86 over ~12 minutes — that residual growth is
  what this fix targets.

## v20260528.0158

### Fixed

- The release / CI workflow's "Capture Web Console screenshots" job no
  longer hangs indefinitely waiting for `networkidle` after navigation.
  The Web Console opens a long-lived `/api/v1/events/stream`
  Server-Sent Events connection on mount, which kept
  `playwright.page.goto({ waitUntil: "networkidle" })` from ever
  resolving on certain runs. `webconsole/scripts/screenshot.mjs` now
  uses `waitUntil: "domcontentloaded"` plus a 30 s navigation timeout,
  a 15 s `waitForSelector("main")`, and a 5 s soft
  `waitForLoadState("networkidle")` that swallows its own timeout.
  `.github/workflows/quality.yaml` also caps the screenshot step at
  `timeout-minutes: 10` as belt-and-suspenders so a future flaky run
  cannot stall the entire release. The `v20260528.0114` tag exists but
  was never published because of this hang — this release supersedes it
  with identical functional content plus the CI fix.

## v20260528.0114

### Fixed

- **production-critical**: `routerd serve` no longer leaks SQLite file
  descriptors against `/var/lib/routerd/routerd.db` on every reconcile
  (#39). The `Ledger` interface gains a `Close()` method,
  `SQLiteLedger.Close()` closes the underlying `*sql.DB`, and every
  `resource.LoadLedger()` call site now defers `Close()`. The primary
  leak was `IPv4PolicyRouteController.cleanupLedgerOwnedPolicyRoutes`,
  which ran every ~30 s and added one new `routerd.db` + one new
  `routerd.db-wal` fd per cycle on homert02 v20260526.2335.
  `OpenSQLiteLedger` also gains `SetMaxOpenConns(1)` /
  `SetMaxIdleConns(1)` defensively, mirroring `pkg/state/sqlite.go`,
  so a missed Close cannot hold more than one connection per path. Two
  Linux-only regression tests (`pkg/resource` and
  `pkg/controller/chain`) assert `/proc/self/fd` does not grow across
  10 open/close cycles.
- `routerctl doctor` NAT/firewall nftables checks no longer reduce a
  failure to bare "exit status 1" (#34). The check now reports
  `table=<family>/<name> cmd=<command> exit=<N> stderr=<≤200 chars>
  stdout=<≤200 chars>`, and downgrades to **warn** (rather than fail)
  when `nft` exits non-zero but the table listing is actually present
  in stdout. Adjacent `NAT44Rule` / `FirewallZone` / `FirewallPolicy` /
  `FirewallRule` status tallies (active / pending / missing) are
  appended to the detail so an operator can correlate the nft-side
  signal with the resource-side signal in one place.

### Added

- Every `routerctl` subcommand now prints proper `Usage: / summary /
  Flags: / Examples:` when invoked with `--help` (#35), instead of
  the previous bare "flag: help requested". Covered subcommands:
  `dns-queries`, `connections`, `traffic-flows`, `firewall-logs`,
  `status`, `events`, `tailscale peers`, `wireguard list`, `ledger`
  (integrity-check / vacuum / backup / prune-events), `apply`,
  `delete`, `set-log-level`, `restart-dns-resolver`, `firewall test`,
  `diagnose`, `doctor`. Summaries document the duration form of
  `--since` explicitly and note that absolute-time `--from` / `--to`
  arrive in this same release.
- `routerctl dns-queries` and `routerctl traffic-flows` gain
  absolute-time range and aggregation (#36):
  `--from` / `--to` accept `RFC3339`, `2006-01-02T15:04:05` (UTC if no
  zone), and `2006-01-02 15:04:05`. New filters: `--rcode`,
  `--upstream`, `--qname-suffix`, `--duration-min` (DNS);
  `--peer-suffix`, `--protocol`, `--asymmetric` (flows). New
  `--agg` / `--stats` mode emits a `SUMMARY` plus
  `BY RESPONSE CODE` / `BY CLIENT` / `BY UPSTREAM` /
  `BY QNAME SUFFIX` (DNS) or `BY CLIENT` / `BY PEER` / `BY PROTOCOL`
  (flows) with p50 / p95 / p99 duration percentiles. Direct-DB fetch
  is now chunked (`--chunk-size`) so each chunk gets its own ctx
  deadline; on `DeadlineExceeded` the error message includes how many
  rows were fetched so far and which `last ts` to narrow against.
  Default `--limit` raised from 100 to 500, `--timeout` from 5 s to
  30 s, and the underlying `DNSQueryFilter` / `TrafficFlowFilter`
  hard-cap raised from 1000 to 10000. The Web Console gains
  `/api/v1/dns-queries/aggregate` and
  `/api/v1/traffic-flows/aggregate` endpoints and the same filter
  query parameters on the existing row endpoints (UI unchanged for
  this release).

## v20260526.2335

Documentation and CI consistency follow-up to v20260526.2241. No
binary or runtime behavior changes.

### Added

- `scripts/check-active-stable.sh` is a CI guard that fails when the
  homepage hero, the docs intro tip, the announcement bar, or
  `docusaurus.config.ts` drift from the `STABLE_VERSION` constant in
  `website/src/pages/index.tsx`. The release-changelog narrative and
  the supersedes / carry-forward history in `stable.md` keep their
  intentional historic references and are excluded from the guard.

### Fixed

- The homepage "Latest stable" card, the docs intro tip in each of the
  four locales, and `STABLE_VERSION` in `website/src/pages/index.tsx`
  now all point at `v20260526.2241`. They had been left at
  `v20260526.1607` when the announcement bar and `stable.md` were
  promoted, so the homepage and the announcement bar disagreed on
  the recommended milestone.
- The `v20260526.2241` install.sh changelog entry has been rewritten
  to match the shipped implementation: `install.sh` remains
  cwd-relative for its payload (so `tests/install` keeps working) and
  adds a `bin/routerd` presence check that exits 2 with a clear
  diagnostic when run from a cwd that has no payload. The earlier
  wording described a `cd $script_dir` design that had been reverted
  in `d9f8817c` because it broke the test harness.

## v20260526.2241

### Fixed

- `install.sh` remains cwd-relative for the release payload (so it
  keeps working with the test harness in `tests/install`), but now
  refuses to proceed when the current working directory does not
  contain an executable `bin/routerd`. It exits non-zero with a clear
  diagnostic instead of silently running zero `bin/*` iterations and
  printing a successful upgrade message. Previously, running the
  installer from outside its release tree — for example
  `cd /tmp/routerd-release-vYYYYMMDD.HHmm && sudo ./pkg/install.sh ...`
  — left cwd outside the payload, standard routerd / routerctl
  binaries were not updated, and only `--with-ndpi-archive` payloads
  ever landed, yet the script still exited 0 and printed `routerd
  upgrade completed`. The installer now exits 2 unless run from inside
  the extracted package directory, and a regression smoke
  (`scripts/install-sh-cwd-smoke.sh`, wired into CI) covers both the
  missing-payload and correct-cwd cases.
- The Web Console no longer briefly renders Gateway Health as
  `Components 0 / Unknown / No gateway component status observed`
  during partial refreshes. `reconcileSummary` previously used
  `next.gatewayHealth ?? current.gatewayHealth`, which only falls back
  on `null`/`undefined`; a thin snapshot like
  `{ overall: "unknown", components: [] }` would overwrite the
  populated previous state and the banner would flash empty until the
  next refresh. The merge now keeps the previous `gatewayHealth` when
  the incoming snapshot has no components but the previous one did.

## v20260526.2152

### Added

- `gatewayHealth` in `/api/v1/summary` now exposes per-component
  evidence: `selectedPath`, `preferredPath`, `fallbackReason`,
  `failedProbes`, and `lastTransition`. The Web Console highlights the
  active fallback target when the selected path differs from the
  preferred one.

### Changed

- The Web Console moves Gateway Health off the Overview into its own
  screen, mirroring the Connections/Clients pattern. Overview keeps a
  compact summary card with overall status, pass/warn/fail/skip counts,
  a jump button, and a one-line worst-component hint when degraded or
  down.

### Fixed

- The BGP controller now hydrates its in-memory applied-policy state on
  reconcile, so restarting routerd no longer re-PUTs the unchanged
  import-policy assignment and resets every BGP session. Production
  users (homert02) previously saw all peers drop and re-establish on
  each routerd restart; ECMP recovery then took up to a hold-time worth
  of stale paths.
- `routerctl doctor dslite` now treats DSLiteTunnel `phase=Up` as
  healthy and recognizes EgressRoutePolicy selection through
  `status.selectedSource = "DSLiteTunnel/<name>"` in addition to the
  legacy `selectedCandidate` name match. Previously every healthy
  DSLiteTunnel showed up as WARN on production-style configurations
  using aggregate candidate names such as `dslite-pd-balanced`, even
  while `gatewayHealth` correctly reported them as `ok`.

## v20260526.1607

### Added

- `routerctl ledger prune-events` records a `routerd.ledger.events.pruned`
  audit event on each non-dry-run prune (with `cutoff`, `deletedRows`, and
  the invoking `uid`/`gid`), so the prune itself is auditable from the
  events table.

### Changed

- `gatewayHealth` in `/api/v1/summary` now also aggregates
  `EgressRoutePolicy`, `NAT44Rule`, and `HealthCheck`. The Web Console
  Overview banner surfaces the selected vs preferred egress path and
  visibly warns when a fallback candidate is in use.

### Security

- The Web Console `/api/v1/config` and generation-config / diff endpoints
  now redact secrets before serializing — WireGuard `privateKey` /
  `preSharedKey`, Tailscale `authKey`, BGP/PPPoE/IPsec `password`,
  WebConsole `initialPassword`, bearer/token fields, and similar. Marker
  values keep the keys present so the UI is unaffected. Privileged
  channels (control socket, `routerctl describe`) are unchanged. Closes a
  read-only Web Console exposure path where any operator reachable on the
  management network could see raw secrets.

## v20260526.1225

### Added

- `routerctl doctor [area]` runs a battery of read-only checks (wan, dns,
  dslite, dhcpv6-pd, nat, firewall, rollback, disk, mgmt) and reports
  PASS/WARN/FAIL with a remediation hint; exits non-zero when anything
  FAILs so it is scriptable.
- `routerctl ledger` maintenance commands for the SQLite state DB:
  `integrity-check`, `vacuum`, `backup <dest>`, and `prune-events
  --older-than <dur>`. Prune is events-only so the generations, objects,
  and artifacts that back rollback and audit history are preserved.
- `ManagementAccess` resource declares management interfaces and admin
  source CIDRs. A non-dry-run `apply` fails (unless `--allow-mgmt-lockout`)
  when a declared management interface is missing, the firewall would drop
  SSH to it (no `mgmt`/`trust` FirewallZone covers it), or an enabled
  WebConsole binds to all addresses.
- `api/v1/summary` now includes a `gatewayHealth` object that aggregates
  `DNSResolver`, `DSLiteTunnel`, and `DHCPv6PrefixDelegation` into an
  overall verdict plus per-component status. The Web Console Overview
  shows a Gateway Health banner at the top, prominent when degraded or
  down with the reason and waiting list.
- Canonical `examples/home-router-mgmt-protected.yaml`: a minimal "safe
  starting point" for replacing a home router with routerd, using the
  3-role firewall (untrust/trust/mgmt), DS-Lite preferred with PPPoE
  fallback, `ManagementAccess`, and a `WebConsole` bound to the mgmt
  address.

### Changed

- The Go module path is now `github.com/imksoo/routerd` (was `routerd`).
  This is invisible when installing from release archives but enables
  `go install github.com/imksoo/routerd/...` and Go-module imports from
  external projects.

## v20260525.1631

### Added

- `routerctl restart-dns-resolver [name]` explicitly restarts a DNS resolver
  service unit, for recovery when the daemon is unhealthy.

### Changed

- `DNSResolver` now runs as an independent, long-lived service unit
  (`routerd-dns-resolver@<name>.service`) instead of a child process of
  `routerd serve`. Restarting or upgrading routerd no longer interrupts DNS;
  config changes (including DHCPv6-PD convergence) apply in place through the
  daemon's reload endpoint without a process restart; and `install.sh` no
  longer auto-restarts the resolver on upgrade. The daemon also starts idle
  when its config file is not yet present and is configured at runtime.

## v20260525.0112

### Changed

- `DNSResolver` brings the daemon up partially at startup instead of waiting for
  every dependency: it serves with the listen addresses and sources that already
  resolve, reports `phase: Degraded` with a `waiting` list while the rest are
  pending, and converges to `Applied` when dependencies resolve. This removes
  the boot-time window where DNS was refused while waiting on a DHCPv6 prefix
  delegation.

## v20260525.0006

### Added

- `routerd rollback --list` and `routerd rollback --to <generation>`: list stored
  config generations and re-apply one through the normal apply path (built on the
  existing SQLite generations; no separate snapshot store).
- `routerctl set-log-level <debug|info|warning|error|default>`: change log
  verbosity at runtime over the control socket without restarting; the override
  also applies to the OTLP log sink.
- `routerctl describe` now shows a resource's Phase, Reason, and Message, plus a
  remediation hint for non-healthy phases.
- The generated config JSON Schema now carries field descriptions (from godoc)
  for non-obvious fields, improving editor completion and validation messages.
- The installer creates a `routerd` system group; operators added to it can run
  `routerctl status` without sudo.

### Changed

- The read-only status socket is now owned `root:routerd` with mode `0o660`;
  routerd sets the group ownership itself when creating the socket, so it no
  longer depends on the service unit's `Group=` setting. The read-write control
  socket stays root-only.

### Removed

- Removed the `disabled:` field; use `enabled: false` instead on `PPPoESession`,
  `HealthCheck`, `DSLiteTunnel`, and `EgressRoutePolicy` candidates. **Breaking:**
  re-author any config that used `disabled:`.
- Removed the no-op `--controller-chain` / `--controller-chain-*` flags and the
  `--observe-interval` scheduled observe (the event-driven controller chain is
  always active; `--apply-interval` is unchanged). Host units that still pass
  these flags must be updated before upgrading.

### Fixed

- `install.sh` no longer auto-restarts `routerd-bgp` during an upgrade, so BGP
  sessions and ECMP are preserved across routerd binary updates.
- An unresolved dynamic reference (`*From` / `upstreamFrom`) during bootstrap is
  now reported as `Pending` and re-reconciled when the dependency's status
  appears, instead of logging a hard error or silently dropping the value
  (DNS resolver, DS-Lite, DHCP servers, VRRP static addresses).
- No more `sql: database is closed` log noise during shutdown; the state store
  rejects access after close gracefully.

### Security

- The read-only status socket is no longer world-accessible; access is limited to
  root and members of the `routerd` group.

## v20260523.2327

### Added

- Added `qemu-guest-agent` to Alpine package defaults in `install.sh` so
  Alpine installs include the virtual-console agent by default.
- Added automatic startup for the QEMU guest agent during virtualized
  `scripts/build-live-iso.sh` boots when virtualization is detected.

### Changed

- Changed dependency defaults to include SSH server packages (`openssh` /
  `openssh-server`) across supported package managers for environments that
  want interactive access.

## v20260523.1542

### Added

- Promoted the built-in DPI classifier into a useful nDPI-free traffic
  classifier. It now records payload-derived application hints, distinguishes
  payload evidence from port fallback, tracks unknown accepted flows with a
  bounded first-packet budget, and adds lightweight protocol detection for
  common local protocols while still allowing the nDPI agent to enrich results
  when available.

### Fixed

- Fixed NixOS rendering for router-managed dnsmasq and DHCPv4 client units by
  allowing `AF_PACKET` in `RestrictAddressFamilies` for raw packet needs,
  rendering dnsmasq through `${pkgs.dnsmasq}`, and pinning generated
  `accept_ra_defrtr = 0` sysctls in the NixOS golden output.
- Fixed the Alpine/OpenRC live ISO so configurations with managed GoBGP start
  `routerd-bgp` under OpenRC before `routerd serve`, resolving issue #28.

## v20260522.1334

### Added

- Added `BGPPeer.spec.ebgpMultihop` for routed eBGP peering. Values `0` and `1`
  keep the direct-peer default, while `2` through `255` configure GoBGP
  `EbgpMultihop.MultihopTtl`; the setting is persisted in `routerd-bgp`
  applied state so daemon restarts restore the same peer TTL.

## v20260522.1045

### Fixed

- Restored the former FRR `set ip next-hop peer-address` import behavior in the
  GoBGP backend. `BGPRouter.spec.importPolicy.nextHopRewrite` now defaults to
  `peer-address`, so accepted eBGP routes install into the kernel FIB via the
  learning peer addresses and preserve ECMP when downstream speakers advertise
  a third-party next-hop. Router status now exposes the rewrite mode and
  installed next-hops.

## v20260522.0824

### Fixed

- Removed `ProtectSystem` and `ReadWritePaths` from generated `routerd.service`
  units. `routerd` already runs without systemd filesystem protection, and the
  explicit write-path list could make clean hosts fail service startup with
  systemd namespace errors when optional directories did not exist.

## v20260522.0742

### Fixed

- Removed the NixOS module `services.routerd.extraFlags` escape hatch so
  NixOS deployments cannot keep passing removed `--controller-chain*` flags
  after upgrading. The generated `routerd.service` now uses the fixed
  `routerd serve` invocation that matches the simplified service lifecycle.

## v20260522.0658

### Fixed

- Fixed in-place upgrades from legacy routerd releases that still passed
  removed `--controller-chain*` flags or declared `SystemdUnit` resources.
  `serve` and `apply` now warn and ignore legacy controller-chain flags instead
  of failing before reconciliation, and the installer replaces legacy
  routerd service units while removing user-facing `SystemdUnit` resources from
  preserved configs before restarting the service.

## v20260522.0006

### Changed

- Replaced the BGP controller backend with a long-lived `routerd-bgp` daemon
  built on GoBGP. `BGPRouter` and `BGPPeer` now map directly to typed GoBGP API
  objects over a local gRPC Unix socket, `apply` no longer renders FRR
  artifacts, and `routerd` restarts no longer restart the BGP process or drop
  established sessions. Observed peer/path status comes from
  `ListPeer`/`ListPath` instead of `vtysh` text parsing. Learned IPv4 best paths
  matching import policy are installed into the kernel FIB, including ECMP next
  hops for equal best paths; unsupported BFD intent is reported as Pending
  instead of being silently ignored. Learned routes that cannot be installed into
  the kernel FIB, such as IPv6 FIB routes in the MVP or non-Linux platforms, now
  degrade the router status with a per-prefix install reason instead of being
  silently dropped. The `routerd-bgp` daemon persists its last applied global,
  peer, and advertisement intent in `/var/lib/routerd/bgp/applied.json` with an
  atomic rename, restores it on daemon restart, and lets `routerd` detect config
  drift after reconnect instead of silently adopting stale live peers.
- Controller runtime status now separates cumulative reconcile failures from
  the current health signal. `reconcileErrorCount` remains a lifetime counter,
  while `currentError`, `consecutiveErrorCount`, `lastErrorTime`, and
  `lastErrorClearedAt` show whether the latest reconcile is still failing or a
  previous transient error has already recovered.
- Added regression coverage for `EgressRoutePolicy` no-op reconciliation so
  unchanged default-route selection, including `mode: priority` dry-run status,
  does not churn `routerd.lan.route.changed` or resource status events.
- `DHCPv6Information` now reports a Pending state while waiting for the
  supervised DHCPv6 client socket during startup instead of logging a repeated
  bootstrap WARN for the expected socket creation race.
- Added an auto-derived `RogueRADetector` for each `IPv6RouterAdvertisement`.
  The new `routerd-ra-observer` daemon passively observes ICMPv6 Router
  Advertisements on the serving interface and reports non-self routers through
  status and `routerd.ipv6.ra.rogue_detected` events without attempting active
  RA Guard on flat L2 segments.
- Renamed selection-only `EgressRoutePolicy` status/event terminology from
  hard-coded `dryRun: true` to `role: advisory` / `advisory: true`. CLI
  `--dry-run` continues to mean preview without applying host changes.
- Stale legacy client daemon unit cleanup now defers active units with a
  Pending status and warning event instead of stopping them, while inactive
  stale units are still removed with status/event evidence.

## v20260521.1953

### Fixed

- Preserved existing nftables dataplane rules during routerd restarts when
  rendered firewall and TCP MSS clamp rules are unchanged, avoiding needless
  `flush table` reloads for `routerd_filter` and `routerd_mss`.
- Hardened unchanged reconcile paths so stale client daemon unit cleanup is
  reported through status/events, static and DHCP IPv4 routes skip matching
  live kernel routes, dynamic nftables address sets update by element diff
  instead of flushing the whole set, and NTP/BGP service actions expose their
  reasons.

## v20260521.1155

### Fixed

- Fixed `EgressRoutePolicy` `mode: priority` so it honors
  `selection: highest-weight-ready`, candidate `weight`, and
  `disabled: true`, reports selected route details consistently, and removes
  stale ledger-owned policy-route rules and route tables after candidates are
  removed.

## v20260521.0918

### Fixed

- Stopped `EgressRoutePolicy` selection-only reconciliation from overwriting
  `mode: priority`, `mode: mark`, and `mode: hash` policy-route status. These
  modes now have a single status owner, preventing dry-run
  `routerd.lan.route.changed` churn when the applied policy selection is
  unchanged.

## v20260521.0843

### Fixed

- Fixed repeated `IPv6DelegatedAddress` apply events on Linux when the kernel
  reports an existing delegated host address with a different prefix length
  such as `/128` instead of the configured `/64`.
- Stopped `routerd.resource.status.changed` events from being emitted for
  `lastTransitionAt` timestamp-only status refreshes.

## v20260521.0827

### Added

- Added `NTPServer.spec.allowCIDRFrom` so LAN NTP client allow ranges can be
  derived from dynamic status fields such as
  `IPv6DelegatedAddress/<name>.address` or
  `DHCPv6PrefixDelegation/<name>.currentPrefix`.

## v20260521.0802

### Added

- Added `install.sh --with-ndpi-archive PATH` so a normal static routerd
  archive and the native `routerd-ndpi-agent-libndpi` archive can be applied in
  one rollback transaction. The installer validates the feature archive target,
  path safety, checksum when present, and `libndpiLoaded: true` self-test before
  the install can satisfy `--with-ndpi`.

### Fixed

- Added serve-startup cleanup for stale object status rows whose resource kinds
  have been removed from the current schema. routerd creates a timestamped
  SQLite backup before deleting those legacy rows, records an audit event, and
  skips cleanup if the backup cannot be created.

## v20260521.0731

### Fixed

- Preserved an already-installed native `routerd-ndpi-agent` when the standard
  release archive contains only the static fallback agent, and made
  `install.sh --with-ndpi` fail if the final agent self-test does not report
  `libndpiLoaded: true`.
- Marked `TrafficFlowLog` as `Pending` with
  `TrafficFlowApplicationLayerUnavailable` when
  `spec.includeApplicationLayer: true` is configured but the nDPI agent does
  not have its native `libndpi` backend loaded.
- Registered the derived `routerd_mss` nftables table as a router-owned
  artifact, so it is no longer reported as an orphan while routerd still
  regenerates it.
- Hid stale derived state from `routerctl show derived-resources` by default,
  added `--include-stale` for audit/debug views, and added
  `routerctl delete --force` so deleted or renamed resource kinds can be
  removed from the state DB without manual SQLite edits.
- Made TCP MSS clamping source-path aware and downward-only. `Interface.spec.mtu`
  can now describe low-MTU source interfaces such as `tailscale0`; routerd uses
  `min(source MTU, destination path MTU)` per source/destination path while
  nftables only rewrites SYN packets whose advertised MSS is higher than the
  derived value.

## v20260521.0039

### Fixed

- Garbage-collect deleted `PPPoESession` artifacts recorded in the ownership
  ledger, including generated PPP peer files, runtime sockets, runtime
  directories, state directories, and the stopped/disabled systemd unit.
- Let the Live ISO import router config from read-only ISO9660/UDF config media
  attached as CD-ROM devices, including Proxmox `media=cdrom` config ISOs
  labeled `ROUTERD_CONFIG`.
- Prevent a persisted OpenRC `routerd` default-runlevel entry from starting
  `routerd serve` before Live ISO USB config restore. The live autostart helper
  now removes that runlevel entry and restarts an already-running `serve`
  process after `apply`, so restored BGP config can be reloaded into FRR.

## v20260520.2307

### Fixed

- Added `CAP_DAC_OVERRIDE` to the generated `routerd.service` only when the
  router config contains FRR/keepalived integrations. Ubuntu FRR commonly keeps
  `/run/frr` as `frr:frr` with mode `0755`; supplementary `frr` group access is
  enough for VTY sockets but not enough for `frr-reload.py` to create its
  `/var/run/frr/reload-*.txt` scratch file under systemd capability bounding.
- Classified `frr-reload.py` permission failures as
  `FRRReloadPermissionDenied` instead of the generic `FRRReloadFailed`.
- Removed stale routerd-managed WireGuard interfaces and peer statuses when
  `WireGuardInterface` / `WireGuardPeer` resources disappear from the config,
  so deleting the resources and restarting `routerd serve` no longer requires
  manually editing the state DB.

### Changed

- Updated Kubernetes BGP examples to import the MetalLB LoadBalancer pool
  `10.250.0.0/24` and adjusted the home-router sample to peer with the two
  k8s route nodes individually.

## v20260520.2227

### Fixed

- Fixed the Live ISO build after adding the OpenRC `routerd` service script by
  creating the overlay `/etc/init.d` directory before writing the script.

## v20260520.2222

### Added

- Added BGP route-selection diagnostics to observed prefix status and
  `routerctl show bgp`, including select-deferred, no-best-path, and
  not-installed-to-zebra states when FRR exposes those fields.
- Added `BGPRouter.spec.convergenceProfile: fast` for Kubernetes/edge routers.
  The fast profile derives short BGP timers and disables graceful restart by
  default to avoid stale-path selection deferral on fresh boot.
- Added Live ISO config import from USB partitions labeled `ROUTERD_CONFIG`.
  The boot helper now selects `/routerd/hosts/<hostname>.yaml`,
  `/routerd/hosts/<mac>.yaml`, or `/routerd/router.yaml`, then records the
  source and SHA256 under `/run/routerd/`.

## v20260520.2107

### Added

- Added the BGP / FRR control-plane design note covering readiness, reload,
  verification, failure status, and Live ISO acceptance scenarios.

### Fixed

- Reconciled the FRR service state on every BGP controller cycle. If FRR is
  stopped or failed on Alpine/OpenRC or systemd hosts, routerd now starts or
  restarts the service before probing `vtysh` and running `frr-reload.py`.
- Tightened BGPRouter health so service state, `vtysh` round-trip,
  `tcp/179` listen state, and the rendered `router bgp <asn>` stanza must all
  be present before the router is reported as healthy.
- Aggregated `routerctl status` from resource phases so a pending or failed
  BGP resource can no longer be hidden by a controller runtime success update.

## v20260520.2007

### Fixed

- Removed the FRR TCP VTY readiness gate from the BGP controller and now use
  `vtysh -c "show running-config"` as the control-plane probe and running
  config diff source. This lets Alpine FRR builds with TCP VTY disabled still
  run `frr-reload.py` on first convergence.
- Added explicit pending status details for FRR control unavailability,
  permission failures, reload attempts, and incomplete reload verification.
- Prevented Alpine Live ISO autostart from launching a second `routerd serve`
  process when one is already running.

## v20260520.1904

### Fixed

- Retried transient FRR reload lock failures during BGP controller reconcile so
  first boot can reach `bgpd` configuration without manual `frr-reload.py`.
- Kept the Alpine Live ISO DHCP client running after the initial lease, derived
  a stable DHCP hostname for live routers, and left DHCP option 61 unset by
  default so Windows DHCP reservations continue to match the Ethernet MAC.

## v20260520.1737

### Added

- Added a FreeBSD CARP backend for `VirtualAddress` in `mode: vrrp`,
  including runtime controller support, rc.d rendering, validation, tests, and
  a minimal `examples/freebsd-vrrp.yaml`.
- Added listen-port collision validation for ingress/local router services and
  `IngressService` `sourceHash` / `random` backend distribution on Linux
  nftables.
- Added FRR BGP connected/static redistribution, BGP community send/accept/set
  policy, observed community status parsing, and
  `examples/lan-advertise-with-community.yaml`.
- Added multi-instance `BGPRouter` support with VRF-backed FRR BGP instances,
  listen-address collision validation, per-router observed status, and
  `examples/multi-instance-bgp.yaml`.
- Added BFD support for FRR-managed BGP peers, FRR `bfdd` daemon rendering,
  BGP watcher tuning fields, BFD status observation, and
  `examples/bgp-bfd.yaml`.
- Added BGP export policy allow-lists for transit routing and automatic FRR
  `bgpd` daemon enablement when a `BGPRouter` is present.
- Added `ClusterNetworkRoute` helpers for Kubernetes Pod / Service CIDR static
  routes, plus `passwordFrom` / `authenticationFrom` secret sources for BGP
  peer passwords and VRRP/CARP authentication.
- Added `routerctl drain` / `undrain` for temporary `IngressService` backend
  maintenance and VRRP production tuning documentation with
  `examples/vrrp-tuning-presets.yaml`.
- Added live BGP / VRRP / IngressService Web Console operational pages with
  SSE refresh, filtered event logs, and lightweight local SVG metric trends.
- Added stateful firewall rule expressions for ICMP / ICMPv6 types, multi-port
  source and destination matches, nftables rate limits, and per-source
  connection limits.
- Added dual-stack BGP rendering and observation for IPv4/IPv6 unicast, plus
  `VirtualAddress` VRRPv3/CARP VIP support, automatic AAAA records, and
  dual-stack BGP/Kubernetes API VIP examples.
- Added `ObservabilityPipeline` for OTLP environment rendering and built-in
  routerd event forwarding to stdout, syslog, or Loki, plus `RouterdCluster`
  file-lease high availability gating for apply/controller mutation.
- Added Alpine/OpenRC VRRP render support: `routerd apply` writes the
  keepalived config artifact, while controller runtime manages the OpenRC
  `keepalived` service and observes live VRRP roles.
- Polished the Alpine live ISO path with live VRRP controller defaults, live
  `routerctl show vrrp` role observation, commit-aware version output, FRR
  reload tooling dependencies, and non-blocking setup wizard behavior.
- Avoided no-op keepalived reloads during live VRRP reconcile and exposed the
  last keepalived reload/restart time and reason in controller status.
- Kept VRRP daemon lifecycle in controller runtime. `routerd apply`
  renders keepalived artifacts and records controller handoff status without
  reload/restart.
- Decoupled IngressService live nftables apply from independent NAT44 dry-run
  mode and relaxed hostname DNSZone coverage to warnings with an `externalDNS`
  opt-out for externally managed DNS names.
- Auto-enabled same-interface IngressService hairpin SNAT and runtime
  `ip_forward` sysctls for forwarding configs, and added
  `routerctl show ingress --verbose` dataplane checks for forwarding, nftables,
  and conntrack state.
- Fixed IngressService `hairpin.mode: auto` for live ISO-style configs without
  a declared listen-interface prefix by treating same private `/24`
  listen/backend addresses as hairpin-required, and made verbose ingress output
  warn when the expected nftables SNAT is missing.
- Added a `pkg/servicemgr` abstraction for systemd, OpenRC, rc.d, and NixOS
  service artifact naming and lifecycle commands, then routed service artifact
  intent generation through it to reduce per-resource OS switch drift.
- Added render golden tests for all checked-in example configs across Linux,
  Alpine/OpenRC, FreeBSD/rc.d, and NixOS snapshots, plus a netns compatibility
  wrapper. Extended `pkg/servicemgr` with lifecycle hooks so FRR config-check
  + live reload, keepalived reload-vs-restart, and signal-based daemon reloads
  remain expressible instead of collapsing into generic restarts.
- Added bespoke lifecycle command golden tests and a `make check-bespoke-lifecycle`
  gate covering FRR live reload, keepalived no-op/reload behavior, dnsmasq
  SIGHUP, DHCP daemon IPC, BFD daemon enablement, IngressService nftables-only
  backend rotation, VRRP track artifacts, DS-Lite dataplane hooks, DHCP event
  daemon ordering, and FRR graceful-restart observation.
- Added a no-behavior-change firewall backend abstraction for nftables and pf
  render/diff/reload paths, with regression contracts protecting nftables
  `ct state`, `jhash`, `numgen`, hairpin conntrack expressions and pf
  `rdr`, `nat-anchor`, and hairpin NAT syntax.
- Added a no-behavior-change network config backend abstraction for netplan,
  systemd-networkd drop-ins, NixOS modules, and FreeBSD rc.conf fragments,
  backed by common IPv4/IPv6 address and route declarations.
- Reworked service-backed artifact intents into a ServiceManager declaration
  table so systemd, OpenRC, rc.d, and NixOS service ownership stays consistent
  across PPPoE, VRRP/CARP, FRR, dnsmasq, DHCPv6 PD, DNS resolver, and Tailscale
  resources without changing rendered output.
- Expanded render golden coverage for firewall hole derivation and OS-specific
  interface/network artifacts, including Linux netplan/systemd-networkd output
  and Alpine nftables snapshots.
- Strengthened abstraction-layer regression coverage with cross-OS semantic
  tests, invalid-spec checks, firewall backend error propagation status/events,
  edge-case declarations, race-tested reload calls, 80% coverage gates, and a
  four-OS bespoke lifecycle command matrix.

### Fixed

- Separated BGP apply-once rendering from daemon lifecycle. `routerd apply
 ` now writes the FRR config and daemon artifact only; `routerd serve
  --controller runtime` owns bgpd enable/restart, `vtysh` validation, live reload,
  and peer observation.
- Fixed BGP observation for FRR JSON fields emitted as strings and made
  `routerctl show bgp` refresh stale stored status from live `vtysh` output.
- Kept FRR readiness and reload status in the BGP controller path so
  controller runtime can report pending/error state without making
  `apply` wait on bgpd or `frr-reload.py`.
- Added a Web Console Routes view and `/api/v1/routes` endpoint that combines
  kernel, BGP, static, DHCP, and policy route information with BGP peer state.
- Added `pkg/api/provides.go` declarative status output contract and reference
  validation: `addressFrom` / `gatewayFrom` / `dnsServerFrom` /
  `sourceAddressFrom` / `dependsOn` references are checked against missing
  kinds and against the referenced kind's `provides` field set at load time.
- Added `routerctl show derived-resources` to inspect auto-derived host
  packages, kernel modules, sysctl entries, systemd-networkd/resolved
  adoption drop-ins, and tunnel `rp_filter` derivations.
- Added `spec.when` `any:` / `all:` recursive predicates so a resource (or
  `IPv4DefaultRoutePolicy` candidate, `EgressRoutePolicy` candidate, etc.)
  can be conditionally active without a separate `StatePolicy` resource.
- Added new high-level kinds: `DHCPv4Client`, `PPPoESession`, `VirtualAddress`
  (`spec.family: ipv4|ipv6`), `EgressRoutePolicy` with `mode: priority|mark|
  hash` and candidate `targets[]`, `DNSForwarder`, `DNSUpstream`,
  standalone `BFD`, `FirewallEventLog`, standalone `LogRetention`. Each
  absorbs or replaces older lower-level kinds (see Removed below).
- Added typed `LogSink` (`type: syslog|otlp|webhook|file|journald`) and a
  `FirewallEventLog` with `events: deny|allow|rateLimit|connLimit` filters,
  `fromZones`/`toZones`/`rules` selectors, `sampleRate`, `sinks`, and
  `retention` references.
- Added a `make check-examples-line-limits` blocking CI gate enforcing
  `examples/*.yaml` ≤ 200 lines and ≤ 50 lines per resource. Compacted all
  shipped examples (e.g. `examples/home-router.yaml` from ≈1800 to 194 lines)
  so each resource fits on one screen.
- Added automatic derivation of host packages (network-utils for HealthCheck,
  vrrp/keepalived for `VirtualAddress mode: vrrp` on Linux, etc.), kernel
  modules, sysctls, MSS clamp / RA MTU, NetworkAdoption drop-ins, and
  default LAN-to-WAN masquerade so common router intent does not require
  explicit `Package` / `Sysctl` / `SysctlProfile` / `NAT44Rule` declarations.

### Changed

- Split `DNSResolver` into `DNSResolver` (listen + cache + query log only) +
  `DNSForwarder` (conditional forwarder, references a resolver and upstreams)
  + `DNSUpstream` (single upstream, protocol enum `udp|tcp|dot|doh`, supports
  TCP and DoT `tlsName`). The controller composes the runtime resolver
  source list from forwarder/upstream graph.
- Reworked BGP BFD: `BGPPeer.spec.bfd` is now a `BFD/<name>` reference;
  inline BFD config is rejected with a migration guide.
- Renamed `TrafficFlowLog.spec.includeNDPI` to `spec.includeApplicationLayer`
  and moved retention out to standalone `LogRetention`.
- Reshaped `ClientPolicy.classification` into `mode` + structured `match`
  (`macs`, `ouiPrefixes`, `hostnamePatterns`, `dhcpFingerprints`).
- DHCPv4 reservations may now sit outside the dynamic pool range, matching
  dnsmasq behavior for static-only assignments.
- Changed loader to error on unknown or removed kinds and on removed fields
  with migration guides instead of silently ignoring them.

### Removed

- Removed `SystemdUnit` user-facing kind. routerd derives systemd / OpenRC /
  rc.d / NixOS service units from declared intent.
- Removed `KernelModule`, `NetworkAdoption`, `Link`, `NixOSHost`,
  `IPv4ReversePathFilter`, `PathMTUPolicy`, `StatePolicy`,
  `IPv4DefaultRoutePolicy`, `IPv4PolicyRoute`, `IPv4PolicyRouteSet`,
  `IPv4SourceNAT`, `DHCPv4Lease`, `PPPoEInterface`, `VirtualIPv4Address`,
  `VirtualIPv6Address`, `DHCPv4Scope`, `DHCPv6Scope`, and `FirewallLog`
  user-facing kinds. Each is rejected at load time with a migration guide
  to the replacement (auto-derive, narrow override, or absorbed kind).
- `Package`, `Sysctl`, and `SysctlProfile` remain only as narrow escape
  hatches; normal router intent should not need them.
- Removed low-level mechanics fields: `HealthCheck` `daemon` / `socketSource`
  / `fwmark` / `sourceInterface` / `sourceAddress*` / `via`; BGP `keepalive`
  / `holdTime` / `connectRetry`; VRRP `advertInterval` / `preemptDelay`;
  WireGuard `fwmark` / `table`; Tailscale `operator` / `binaryPath`;
  DHCPv6PrefixDelegation `iaid` / `duidType`. routerd derives the underlying
  daemon/socket/timer/sysctl from higher-level intent.
- Removed `DNSResolver.spec.sources`; declare `DNSForwarder` + `DNSUpstream`
  resources that reference the resolver instead.
- Removed `--controller-chain` public flag from `routerd serve` and `routerd
  apply`; the controller chain is always the production runtime path.

## v20260519.0743

### Changed

- Sanitized public documentation and example configuration names so internal
  lab hostnames, domains, and management-network addresses stay in internal
  notes instead of website or reusable examples.
- Moved internal design and soak notes out of the public Docusaurus docs tree,
  and documented the lab validation policy for native nDPI and RA/DHCPv6-PD
  coverage under `internal/notes/`.

## v20260519.0713

### Fixed

- `routerctl show bgp`, `routerctl show vrrp`, and `routerctl show ingress`
  no longer open the ownership ledger, so they work with an explicit status
  store even when the default ledger path is not writable.

## v20260519.0708

### Added

- Added FRR-backed `BGPRouter` / `BGPPeer`, keepalived-backed
  `VirtualAddress`, and runtime `IngressService` backend health/failover
  control for Kubernetes edge use cases.
- Added `routerctl show bgp`, `routerctl show vrrp`, and
  `routerctl show ingress` table views, derived DNS records from VIP/ingress
  `hostname` fields, and BGP/VRRP/Ingress OpenTelemetry metrics for transitions
  and backend health.
- Added Web Console dedicated BGP, VRRP, and IngressService views and JSON
  endpoints.

### Changed

- FRR BGP config is now syntax-checked with `vtysh -C -f` and applied through
  `frr-reload.py --reload`. VRRP defaults to unicast peers with `nopreempt`,
  supports track hysteresis and `preemptDelay`, and Linux firewall holes are
  derived for BGP, VRRP, and IngressService listener ports.
- BGP reconcile no longer lets dry-run writes mask a later live apply, and the
  first live observation compares FRR running-config before deciding to reload
  so an already-matching session is not reset by a no-op reload.

## v20260518.1810

### Added

- Added a separate `routerd-ndpi-agent-libndpi-linux-amd64` release archive for
  hosts that opt into native nDPI classification. The normal Linux release
  archives remain fully static, while the optional nDPI agent override is built
  with `CGO_ENABLED=1 -tags libndpi` and verified with a libndpi self-test.

## v20260518.1431

### Added

- Added controller reconcile runtime status to the control API, logs, OpenTelemetry
  metrics/traces, and the Web Console controller view. Controller status now
  reports interval, trigger, run/error counts, last/average/max duration, and the
  latest error when present.

## v20260518.1301

### Changed

- Removed dead compatibility helpers and obsolete raw systemd unit renderers
  that are no longer used by the current controller runtime configuration path.

## v20260517.2339

### Added

- Added a Configuration examples documentation section with numbered topology
  diagrams, diagram-to-YAML mapping comments, safety notes, and validated sample
  YAML for basic IPv4 NAT, LAN DHCP/DNS, DS-Lite, PPPoE, port forwarding,
  guest isolation, multi-WAN failover, local DNS redirect, Tailscale,
  WireGuard, and telemetry export patterns.
- Health checks referenced by IPv4 route policy resources now derive their
  socket mark from the referencing route candidate or target. Direct
  `spec.fwmark` remains available for standalone probes, and validation rejects
  conflicting explicit marks.

### Changed

- Linux upgrades now refresh routerd helper systemd services only when a helper
  is still running a deleted pre-upgrade binary or its unit file was regenerated
  after the helper process started. The installer waits for `routerd.service`
  and routerd-managed unit files to settle before making that decision.
- The release installer now skips host service-manager changes on NixOS, so
  archive-based binary updates do not fail when `/etc/systemd/system` is
  read-only and service units are managed declaratively.
- Conntrack observation now records an `Unavailable` status instead of logging a
  warning every interval when conntrack procfs files are not present on the
  host.
- FreeBSD `--skip-service-manager` apply now suppresses rc.d/service operations
  for generated helpers, managed dnsmasq, and pf/pflog service activation while
  still allowing rc.conf-backed network state and direct `pfctl` rule loading to
  proceed. This keeps recovery and bootstrapping paths from racing the base rc
  boot sequence.
- FreeBSD upgrades now preserve a config-managed `routerd` rc.d script instead
  of replacing it with the generic bootstrap template, matching the existing
  Linux behavior for config-managed `routerd.service`.
- `routerd serve` now handles SIGTERM/SIGINT by shutting down its control and
  status sockets cleanly, allowing FreeBSD rc.d restarts under `daemon(8)` to
  stop without falling through to forced KILL.
- The routerd state SQLite database now uses WAL mode with the existing busy
  timeout, reducing transient `SQLITE_BUSY` failures when status readers and the
  controller overlap.

## v20260517.1808

### Fixed

- The Debian/Ubuntu release installer now installs `dnsmasq-base` instead of
  the full `dnsmasq` package, avoiding an enabled distro `dnsmasq.service`
  racing with routerd-managed dnsmasq instances.

## v20260517.1800

### Fixed

- One-shot HTTP-over-Unix calls from controllers and helper probes now disable
  keep-alive and close idle transports explicitly. This prevents periodic
  status polling from leaving large numbers of established Unix sockets open in
  `routerd`, health check helpers, DHCP clients, and DNS/DPI helper services.

## v20260517.1533

### Fixed

- The release helper now regenerates checked-in config and control API schemas
  before running schema checks, so API type changes are included in the release
  commit instead of failing late during release.
- `routerctl` now retries transient Unix-socket connection failures for
  read-only control API requests during daemon startup. `routerctl status` now
  uses a separate read-only status socket by default, while apply and delete
  continue to use the privileged control socket and are not retried.

## v20260517.1510

### Added

- Web Console Connections now marks flows that were handled by
  `LocalServiceRedirect`, including the redirect rule and destination
  `IPAddressSet` when the live conntrack tuple and resolved set status identify
  the match.
- Web Console Firewall now shows destination `IPAddressSet` matches on deny-log
  rows, distinguishing explicit `FirewallRule.destinationSetRefs` matches from
  destinations that are currently present in a configured set.

## v20260517.1401

### Fixed

- Fixed Web Console disk usage collection so it compiles on FreeBSD, where
  `syscall.Statfs_t` block counters use signed integer types.

## v20260517.1353

### Fixed

- The release helper now rejects changelogs whose first release section is not
  `Unreleased`, and the stale empty release headings left by older helper runs
  were removed from the maintained changelog files.

## v20260517.1351

### Changed

- `routerd-dpi-classifier` now has an explicit classifier engine facade. The
  default engine is the built-in parser, and `auto` / `ndpi-agent` modes can
  query a future `routerd-ndpi-agent` Unix-socket service with built-in fallback.
- Web Console Connections now labels TCP port 4317 as OTLP and TCP port 4318 as
  OTLP/HTTP when DPI has not identified the flow.
- Web Console Overview now shows host CPU, memory, and root filesystem usage,
  plus classifier-side DPI processing latency, so router-local load regressions
  are visible next to routing and DPI health.
- Web Console Clients and Connections now link to each other. Client rows can
  open a Connections view filtered to that client's observed addresses, and
  connection details can jump back to the matching local client identity.
- Web Console Connections now loads recent traffic-flow observations while the
  Clients snapshot is built, so recent IPv6 privacy addresses are more likely
  to resolve back to a client. Source endpoints also expose a Clients search
  action even when the address has not yet been merged into a known identity.
- Web Console search inputs now show an inline clear button when they contain
  text.
- The release helper now requires a clean working tree and promotes the current
  `Unreleased` changelog entries into the release tag instead of creating empty
  tag headings.

### Added

- Added `IPAddressSet` and `LocalServiceRedirect`. `IPAddressSet` can resolve
  literal IPv4/IPv6 addresses and FQDN `A`/`AAAA` records into reusable nftables named sets,
  and `LocalServiceRedirect` can redirect LAN-origin plaintext DNS/NTP traffic
  for those sets to local router services without touching DoH/DoT or
  router-originated health checks.
- `FirewallRule`, `NAT44Rule`, `IPv4PolicyRoute`, and `IPv4PolicyRouteSet` can
  now consume `IPAddressSet` resources through `destinationSetRefs` and
  `excludeDestinationSetRefs`, allowing FQDN-backed address sets to be reused for
  firewall filtering, NAT scoping, and IPv4 policy routing.
- Added a runtime `IPAddressSet` refresh controller. Referenced nftables sets are
  refreshed in place from DNS TTLs, using half of the minimum observed TTL with a
  60 second floor and an optional `refreshInterval` cap, so FQDN-backed sets stay
  current without reloading the full firewall, NAT, or policy table.
- Added the initial `routerd-ndpi-agent` service boundary as an optional
  command. Default builds report that the libndpi backend is unavailable,
  while `-tags libndpi` builds link the native library behind the same IPC
  surface.
- `routerd-ndpi-agent` now owns per-flow observation state, including flow TTL,
  flow count limits, first-payload-packet limits, and status counters for
  observed, classified, unknown, skipped, error, and pruned packets.
- Added the initial libndpi backend for `routerd-ndpi-agent`. It is opt-in via
  the `libndpi` build tag, keeps native flow state inside the agent, and can
  classify full packet observations from the firewall logger.
- Added a `make build-ndpi-agent-libndpi` target for building the optional
  native backend when libndpi development files are installed.
- Added systemd, OpenRC, FreeBSD rc.d, and NixOS rendering for
  `routerd-ndpi-agent` when `routerd-dpi-classifier` is configured with
  `--engine auto` or `--engine ndpi-agent`.
- DPI flow and traffic-flow records now persist typed classifier fields such as
  detected protocol, application protocol, category, confidence, risk, and
  metadata in addition to the legacy app label fields.
- `routerd-dpi-classifier` status now reports average and maximum classify
  latency for requests handled by the daemon.

### Fixed

- On Linux upgrades, `install.sh` now restarts active routerd helper systemd
  services that are still running a deleted pre-upgrade binary after the
  replacement.
- `routerd-dpi-classifier` now preserves useful built-in packet hints such as
  TLS SNI, HTTP Host, and DNS query when an nDPI agent result identifies the
  application but lacks those details.
- DPI helper daemons now refuse to unlink a non-socket path when binding their
  Unix sockets, and `routerd-ndpi-agent` closes native libndpi state explicitly.
- Web Console traffic-flow reads now tolerate legacy SQLite files that do not
  yet have the newest DPI columns, so a read-only UI query can still succeed
  before the writer performs schema migration.

## v20260516.2302

### Changed

- Web Console Connections now keeps the source-to-destination route aligned in
  a fixed route column and moves state, protocol, provider, traffic, and timeout
  metadata into a separate badge area.
- Web Console connection labels now separate transport/application identity from
  destination providers. Legacy provider-specific labels such as `google-https`
  are canonicalized to `TLS`, while Google, AWS, Microsoft, Apple, and
  Cloudflare appear as separate destination provider badges.
- Destination service names such as `https` are now rendered as protocol badges
  when they add information to the connection row.

### Fixed

- Fixed expanded connection details so destination service and provider badges
  keep their content width instead of stretching across the full detail column.
- Fixed expanded connection details so source and destination identity text uses
  the available width and wraps instead of being ellipsized at the compact row
  width.
- Fixed the Connections `Showing` metric so it distinguishes filtered rows,
  loaded rows, and the total conntrack count when the API result is truncated by
  the requested row limit.

## v20260516.2155

### Changed

- Web Console Connections now sorts active flows by observed transfer bytes by
  default. The Connections sort menu includes a `Traffic` option, connection
  cards show total bytes, and expanded details show outbound, inbound, and total
  counters when conntrack accounting is available.
- The conntrack observer now prefers higher-byte entries within each
  family/protocol group when applying the Web Console connection limit, so large
  active flows are less likely to be hidden by low-traffic entries.

## v20260516.1413

### Fixed

- Fixed `routerd apply --dry-run` and related planning paths so a missing
  SQLite ownership ledger is treated as an empty in-memory ledger instead of
  trying to create `/var/lib/routerd` on unprivileged CI runners.

## v20260516.1405

### Added

- Added `PortForward` and single-backend `IngressService` resources under
  `firewall.routerd.net/v1alpha1` for WAN-side IPv4 TCP/UDP ingress DNAT.
- Linux nftables and FreeBSD pf rendering now publish those ingress services
  and can optionally render hairpin NAT so LAN clients can use the WAN address
  for the same port-forwarded service.
- Added generated JSON Schema, CLI aliases, API documentation, and resource
  ownership documentation for the new ingress NAT resources.

## v20260516.0804

### Changed

- Web Console Connections now groups active flows by fixed IP family and
  transport protocol buckets instead of splitting tables by DPI application.
  App labels such as TLS, DNS, and QUIC remain visible inside each group.

## v20260514.1433

### Added

- Added Alpine Linux / OpenRC apply support. `routerd apply` now renders OpenRC
  service scripts so routerd-managed services can be started and managed on
  Alpine hosts.

## v20260514.0813

### Fixed

- Fixed Web Console Clients so IP-address-based DNS, traffic, firewall, DPI, and
  DHCP fingerprint evidence is limited to the same recent one-hour observation
  window before being correlated with current DHCP leases.
- Sticky DHCP lease annotations now load only active holds for the client
  inventory path, avoiding stale lease history in current endpoint identity
  decisions.

## v20260514.0743

### Fixed

- Fixed Web Console Clients so expired dnsmasq leases are ignored instead of
  keeping old hosts visible indefinitely.
- Web Console DHCP lease merging now prefers the newest valid lease, using the
  configured lease-file order only as a tie-breaker.
- routerd now passes the controller runtime dnsmasq lease file to the Web Console
  first, so the console follows the lease file that the managed dnsmasq instance
  actually uses.

## v20260514.0654

### Fixed

- Fixed the Web Console Overview so lightweight first-load snapshots are not
  recorded as zero-valued metric samples.
- The Overview delayed refresh now loads the resource, event, conntrack, DNS,
  and recent traffic-flow data it needs while still avoiding heavier firewall,
  VPN, and client inventory work.
- Overview cards now show a loading state for omitted flow and connection data
  instead of presenting unavailable values as zero.

## v20260514.0037

### Fixed

- DHCPv4 LAN domain rendering now emits both the domain-name and domain-search options from `domain` / `domainFrom`, unless an explicit domain-search option is already configured.

## v20260514.0025

### Added

- Added `domainFrom`, `dnsslFrom`, and `domainSearchFrom` so DHCPv4,
  IPv6 RA, and DHCPv6 LAN suffix advertisement can reference
  `DNSZone/<name>.zone` instead of repeating the local domain string.

## v20260513.2358

### Changed

- Hardened long-running event processing. `EventRule` and `DerivedEvent`
  timers now clean up their map entries after firing, ignore stale timer
  callbacks, and protect shared state with controller locks.
- Bounded retained `EventRule` correlation state so high-cardinality event
  streams cannot grow memory usage indefinitely.
- Rotated daemon `events.jsonl` files at a fixed size instead of appending
  forever.
- Added request and response size limits to local control, daemon-event, DNS
  resolver, DoH, and classifier paths, and added HTTP header timeouts to local
  daemon servers and the Web Console.

### Fixed

- Removed a race in `DerivedEvent` hysteresis handling that could update
  pending transition state from a timer callback while reconcile was running.

## v20260513.2317

### Changed

- Refreshed the production reconciliation documentation after the
  `v20260513.2252` hardening work. The operations, upgrade, state ownership,
  and localized changelog pages now describe host-state drift checks, managed
  cleanup, nftables named-set updates, and config-managed `routerd.service`
  upgrade behavior.

## v20260513.2252

### Changed

- Hardened production reconciliation so controllers compare the status database
  with the host state before skipping work. This covers systemd units, dnsmasq,
  DHCPv4 lease addresses, route-policy nftables tables, NAT44, and related
  managed artifacts.
- Health checks now carry `fwmark` through the rendered systemd units, socket
  setup, status observations, and OpenTelemetry attributes. This lets probes use
  the same policy-route marks as the paths they are testing.
- Linux firewall rendering now clears routerd-managed named sets before
  redefining them. Removed zone interfaces or client-policy MAC addresses no
  longer remain in nftables, while the managed filter table is still reloaded
  without destroying the whole table.
- The release installer preserves a config-managed `routerd.service` instead of
  overwriting it with the archive template. When routerd manages its own unit,
  unit-file changes schedule a delayed self-restart through `systemd-run`.

### Fixed

- Removed stale `routerd-healthcheck@*.service` units when their `HealthCheck`
  resources disappear from YAML.
- Cleared the managed NAT44 table or pf anchor when the last NAT rule is
  removed.
- Re-applied a DHCPv4 lease address when status said it was present but the
  address was missing from the interface.
- Marked empty `WireGuardPeer` resources as `NotConfigured` instead of leaving
  them in a misleading pending state.

## v20260513.1931

### Fixed

- Stabilized health-check route failover behavior.

## v20260513.1153

### Fixed

- Stabilized idempotent controller reconciliation.

## v20260513.0836

### Added

- Added the WireGuard mesh controller.

## v20260513.0727

### Changed

- Raised the home-router UDP conntrack timeout configuration.

## v20260512.0037

### Added

- Exported DPI flow metrics from the conntrack observer.

## v20260512.0032

### Added

- Added DPI summary cards to the Web Console Overview page.

## v20260512.0027

### Added

- Added DPI activity summaries to the Web Console Clients page.

## v20260512.0008

### Added

- Added DPI classifications to the Web Console Connections page.

## v20260511.2357

### Changed

- Extended DPI enrichment to forwarded flows.

## v20260511.2307

### Fixed

- Contained horizontal overscroll in the Web Console.

## v20260511.2300

### Fixed

- Fixed horizontal scrolling in the Firewall timeline.

## v20260511.2253

### Changed

- Reworked the Web Console around content-driven layout sections.

## v20260511.2217

### Changed

- Validated the mobile Web Console layout.

## v20260511.2211

### Changed

- Preserved Web Console page state across navigation.

## v20260511.2154

### Changed

- Structured the Clients inventory view.

## v20260511.2145

### Added

- Added Web Console SSE reconciliation.

## v20260511.2130

### Added

- Added client fingerprint inference.

## v20260511.2106

### Changed

- Correlated expired conntrack return flows.

## v20260511.2045

### Changed

- Enriched firewall deny events with DPI context.

## v20260511.2018

### Changed

- Validated DPI classifier OS parity.

## v20260511.1846

### Fixed

- Fixed the Web Console time locale to English.

## v20260511.1840

### Added

- Added an isolated DPI classifier proof of concept.

## v20260511.1820

### Added

- Added Connections protocol summaries.

## v20260511.1709

### Fixed

- Fixed release artifact checksums.

## v20260511.1428

### Changed

- Improved Web Console navigation sections.

## v20260511.1240

### Changed

- Refined controller mode reasons.

## v20260511.1041

### Added

- Exposed dry-run controller visibility.

## v20260511.1017

### Changed

- Made controller dry-run modes explicit.

## v20260510.1956

### Changed

- Let `NetworkAdoption` manage resolved DNS.

## v20260510.1811

### Added

- Added the PVE live ISO serial-console validation log to `internal/notes/` so the walkthrough screenshots and execution log are preserved together as test evidence.

## v20260510.1802

### Changed

- Embedded the real PVE live ISO boot screenshots in the Japanese, Simplified Chinese, and Traditional Chinese diskless mini PC walkthroughs.
- Removed stale placeholder screenshot references from the diskless mini PC walkthroughs.

## v20260510.1750

### Added

- Added real PVE live ISO screenshots to the diskless mini PC walkthrough.
- Added missing Simplified and Traditional Chinese pages for positioning, USB persistence, and legal redistribution.

### Changed

- Changed the website footer copyright text to the conventional copyright-first form.
- Updated the diskless mini PC walkthrough to use VGA plus serial console so QEMU screenshots and `qm terminal` validation can be captured in one run.

### Fixed

- Fixed the live ISO configure wizard so DHCPv4 pool defaults are derived from the selected LAN address prefix.
- Re-ran the PVE live ISO boot test with `/tmp/iso-boot-test-20260510-1742.log`, QEMU screenshots, routerd apply, Healthy status, and USB persistence flush validation.

## v20260510.1722

### Added

- Added BSD 3-Clause SPDX identifiers to routerd Go sources, installer scripts, plugin scripts, and Web Console sources.
- Added a README license badge and linked the BSD 3-Clause license from the English and Japanese READMEs.
- Added public contributing documentation and linked it from the docs sidebar.
- Added SECURITY reporting details for email and GitHub Security Advisories.

### Changed

- Unified the root `LICENSE` copyright notice as `Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors`.
- Clarified the legal documentation that SPDX headers apply to routerd source files only; bundled third-party software remains covered by `THIRD_PARTY_LICENSES.md`.
- Removed product comparison tables from the README and kept the positioning text focused on routerd's own scope.

## v20260510.1626

### Added

- Added a public legal and redistribution page with release checklist.
- Added Go module source URLs to the generated third-party license inventory.
- Recorded an internal license audit note for the BSD routerd binary and aggregate live ISO distribution model.

## v20260510.1612

### Added

- Added an automated third-party license inventory for Go modules and Alpine packages used by the live ISO.
- Added release archive and live ISO license notice installation paths.
- Documented routerd BSD 3-Clause licensing and live ISO aggregate-distribution handling.

## v20260510.1547

### Added

- Expanded the public positioning material around routerd's own scope and deployment spectrum.
- Expanded hardware compatibility guidance for Intel NUC, N100 mini PCs, Raspberry Pi 5, thin clients, and Proxmox VMs.
- Added Chinese hardware compatibility pages and clarified the live ISO plus USB persistence path.

## v20260510.1534

### Added

- Added diskless mini PC walkthrough diagrams, tutorial index updates, and a field-note blog post.

## v20260510.1508

### Added

- Added USB persistence operations documentation and live ISO USB persistence support.

## v20260510.1451

### Added

- Added project contribution, security, license, positioning, hardware compatibility, and diskless mini PC documentation.

## v20260510.1429

### Added

- Added Alpine live ISO build and install documentation.

## v20260510.1412

### Added

- Added live ISO validation notes and installer documentation for the live ISO path.

## v20260510.1354

### Fixed

- Fixed live ISO runtime apply on Alpine.

## v20260510.1310

### Added

- Enabled serial console support for the live ISO.

## v20260510.1301

### Changed

- Switched release tags to JST timestamp format.

## 20260510.4

### Fixed

- Fixed the live ISO overlay archive path.

## 20260510.3

### Fixed

- Fixed Alpine live ISO release discovery.

## 20260510.2

### Added

- Added Alpine-based live ISO packaging.

## 20260510.1

### Added

- Added the installer configuration wizard.

## 20260510.0

### Changed

- Started the 20260510 release series after the fixed-download-asset release.

## 20260509.16

### Added

- Release archives now include fixed-name aliases such as `routerd-linux-amd64.tar.gz` in addition to versioned archives.
- Fixed-name archives and their `.sha256` files are uploaded to GitHub Releases, so documentation can use `releases/latest/download/...` URLs.

### Changed

- Quick start documentation now uses stable latest-download URLs instead of hardcoded release versions.
- The release workflow opts GitHub JavaScript actions into the Node.js 24 runtime where supported.

## 20260509.15

### Added

- Added a `CI` GitHub Actions workflow for branch pushes and pull requests.
- The CI workflow runs `go test ./...`, schema checks, example validation, and the website build on Ubuntu.
- Added an optional `scripts/pre-commit.sh` hook that runs Go tests and schema checks before local commits.
- Added development documentation that explains the split between CI, pre-commit checks, and tag-driven release publishing.

## 20260509.14

### Changed

- Validated `ClientPolicy` guest mode on an Ubuntu lab router.
- Confirmed Linux nftables renders include-mode guest MAC sets, guest DNS/DHCP/NTP access, self-isolation, and RFC 1918 / ULA deny rules.
- Confirmed exclude-mode rendering with the focused nftables renderer test.

## 20260509.13

### Added

- Expanded the guest mode guide with use cases, implementation details, full `ClientPolicy` field reference, verification steps, troubleshooting, and security limits.
- Added documented examples for include mode, exclude mode, multiple guest devices, custom deny/allow lists, local discovery services, and IoT reservations.
- `ClientPolicy.spec.guestServices` now accepts `mdns` and `ssdp` in addition to `dhcp`, `dns`, and `ntp`.

## 20260509.12

### Added

- Added `ClientPolicy`, a Linux nftables-backed guest mode that classifies LAN clients by MAC address.
- Guest clients can keep DNS, DHCP, and NTP access while private IPv4 and ULA IPv6 destinations are denied by default.
- Added `examples/guest-mode.yaml` and documentation for include-mode and exclude-mode client classification.

### Changed

- FreeBSD pf now rejects `ClientPolicy` explicitly because pf does not provide the same MAC-based routed filtering model.

## 20260509.11

### Added

- Added focused example configurations for minimal Tailscale mesh membership, WireGuard hub-spoke routing, a VRF lab, and multi-WAN home fallback.
- Added `examples/README.md` to explain when each example should be used.

### Changed

- `make validate-example` now validates every YAML file under `examples/`.

## 20260509.10

### Added

- Web Console overview now shows browser-session trend charts for generation, resource phases, and HealthCheck state.
- The Config page can compare the current YAML file with the latest applied generation before an operator runs `routerd apply`.
- Resource tables now support kind/name/phase/detail search, phase filtering, and match highlighting.
- VPN pages now include visual peer status strips for Tailscale and WireGuard.

## 20260509.9

### Added

- Release archives now carry a `share/doc/TARGET` marker, and `install.sh` checks the archive OS and architecture against the host.
- GitHub Actions now builds Linux and FreeBSD archives for both `amd64` and `arm64`.
- Release CI runs `shellcheck` against the installer and uninstaller scripts.

### Changed

- `install.sh --list-deps` now prints a structured dependency plan with OS, architecture, package manager, packages, and checked commands.
- Installer dependency sets were expanded for practical router use, including PPPoE, RA, IPsec, packet capture, routing, and firewall tooling.

## 20260509.8

### Fixed

- Fixed zh-Hant and zh-Hans documentation links so translated pages no longer point at missing locale-local documents.
- Kept translated overview pages linked to the canonical English reference pages until full translations are available.

## 20260509.7

### Added

- Multi-stage WAN fallback can now model DS-Lite primary tunnels, RA-sourced DS-Lite, PPPoE, and direct WAN fallback candidates through `EgressRoutePolicy`.
- OpenTelemetry deployment was extended across the router fleet with declarative `Telemetry` resources and OTLP environment propagation.
- DS-Lite examples now use the RFC 6333 B4-AFTR link prefix `192.0.0.0/29` for tunnel inner IPv4 source addresses.
- `PPPoESession.disabled` and disabled route-policy candidates keep PPPoE fallback definitions in YAML without leaking a production PPPoE session.

### Changed

- Release versions moved away from `0.x.y` and toward date-based values.
- `routerd --version`, `routerctl --version`, and release archives now use the same release tag value.
- NAT44 rendering was tightened around per-interface rules on Linux nftables and FreeBSD pf.
- The 3-role firewall model was verified on Linux and FreeBSD, with service holes bound to the owning ingress interface instead of broad multi-interface zones.
- FreeBSD pf gained TCP MSS clamp rendering for `PathMTUPolicy`, aligning it with Linux nftables behavior.
- dnsmasq RA generation now propagates path MTU through the IPv6 RA MTU option.

### Fixed

- FreeBSD pf service-hole rendering no longer expands DHCPv6, WireGuard, and VXLAN holes across every member of the `wan` zone.
- FreeBSD NAT artifacts are reported as `pf.anchor/routerd_nat` instead of nftables artifacts.
- PPPoE interface aliases are resolved to the real OS interface name before NAT rendering.

## 0.4.0

### Added

- The implicit-deny log lines from nftables are now ingested by `routerd-firewall-logger` and stored in `firewall-logs.db`. On Linux the logger reads `nfnetlink` directly; on FreeBSD it consumes `pflog` directly through BPF.
- The Web Console gained a Connections tab (live conntrack / pf state), a Clients tab (DHCP lease + traffic statistics combined), and a Firewall tab (deny ranking plus a per-second timeline).
- `TailscaleNode` can now advertise a router as a Tailscale exit node and subnet router through a generated systemd unit. NixOS rendering enables `services.tailscale` and includes the generated unit path.
- `WebConsole.spec.listenAddressFrom` and the listen address of `DNSResolver` resources can now be derived from `Interface/<name>.status.ipv4Addresses`. Reference fields can be used in place of literal IP values.
- Conntrack accounting (`net.netfilter.nf_conntrack_acct=1`) is enabled in the default `SysctlProfile/router-linux` profile, so `TrafficFlowLog` can record `bytesOut` and `bytesIn`.

### Changed

- The live connection view in API and CLI is unified under the name `connections` (previously `conntrack-snapshot`). Use `/api/v1/connections` and `routerctl connections`. IPv6 connections are surfaced in the same table.
- NixOS rendering was extended. `Package` (NixOS-style declarations), `SysctlProfile`, `NetworkAdoption`, and `generated service artifacts` now flow into the `routerd render nixos` output. On NixOS the `Package` resource is no longer installed at runtime; its content is owned by the generated NixOS configuration instead.
- `generated service artifacts` resources can now produce FreeBSD `rc.d` scripts via `routerd render freebsd --out-dir`.

### Fixed

- `IPv6DelegatedAddress` no longer skips applying the delegated address to a host interface when the upstream `Link/<name>` status is empty.
- `generated service artifacts` no longer restarts an already-active unit when nothing has changed.

## 0.3.0

### Added

- `Package` and `SysctlProfile` resources for declarative OS bootstrap. They cover apt, dnf, nix, and pkg package declarations as well as router-oriented sysctl tuning (`nf_conntrack_max`, socket buffers, TCP/UDP timeouts, `ip_forward`, etc.) in a single resource.
- `NetworkAdoption` disables systemd-networkd's DHCP / RA from YAML. `generated service artifacts` lets routerd render, install, and enable its own unit files.
- `routerctl events --limit N --topic X --resource K/N -o json` reports bus events without requiring `sqlite3`.
- `routerd plan --diff` previews the diff that an apply would produce.
- `DNSResolver` accepts a bootstrap forwarder so internal DNS can be tried first while public DNS acts as a fallback.

### Changed

- `${...status.field}` string references inside the configuration were replaced by typed `*From` fields (`addressFrom`, `ipv4From`, `ipv6From`, `upstreamFrom`, `prefixFrom`, `rdnssFrom`, `dependsOn`). No backwards-compatible aliases.
- The controller chain was rebuilt as a pure event-loop. A common `framework.FuncController` (Subscriptions + Bootstrap + PeriodicFunc) and an `eventedStore` wrapper guarantee that any persisted state change emits `routerd.resource.status.changed`, which downstream controllers consume.
- Bus events are emitted to the systemd journal through `slog`. `journalctl -u routerd.service -f | grep "routerd event"` traces the controller chain. High-frequency topics are at the debug level.
- All binaries are now statically linked (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`). The OS-specific package list (`dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `strongswan-swanctl`, `radvd`, `tcpdump`, etc.) is documented per Ubuntu / NixOS / FreeBSD.
- `HealthCheck.sourceInterface` is written as a resource name in YAML and resolved to an OS interface name at runtime.

### Fixed

- The `RuntimeDirectory` collision between `generated service artifacts` resources that previously deleted sockets across restarts is solved declaratively via `runtimeDirectoryPreserve`.
- `generated service artifacts` with `state: absent` is now correctly detected as Drifted and unit removal is included in the plan.
- `SysctlProfile` observation no longer reports spurious drift caused by type coercion.

## 0.2.0

### Added

- Stateful firewall: `FirewallZone`, `FirewallPolicy`, and `FirewallRule` generate the `inet routerd_filter` table for nftables.
- `EgressRoutePolicy` (formerly `WANEgressPolicy`) gained `destinationCIDRs`, `gateway`, and `gatewaySource`. `HealthCheck` accepts `via`, `sourceInterface`, and `sourceAddress` to scope the probe path.
- The DNS subsystem was reorganised. `DNSZone` (authoritative zone definition) and `DNSResolver` (forwarder / cache) cover local zones, conditional forwarding, DoH / DoT / DoQ, and plain UDP DNS. dnsmasq is now scoped to DHCPv4 / DHCPv6 / RA / relay only.
- DS-Lite (`DSLiteTunnel`), PPPoE (`PPPoESession`, `routerd-pppoe-client`), DHCPv4 client (`routerd-dhcpv4-client`, `DHCPv4Client`).
- NAT44 (`NAT44Rule`) and conntrack observation. The observer falls back to a sysctl-derived summary when `/proc/net/nf_conntrack` is unavailable.

### Changed

- `WANEgressPolicy` was renamed to `EgressRoutePolicy`. No backwards-compatible aliases.
- DHCP client kinds and binary names were aligned with RFC notation: `routerd-dhcpv4-client`, `routerd-dhcpv6-client`. No backwards-compatible aliases.

## 0.1.0

The first v1alpha1 implementation.

- Introduced the DHCPv6-PD client, the daemon contract, the event bus, and the controller framework.
- Implemented the controller chain that turns DHCPv6-PD into LAN address derivation and DNS responses.
- Added DHCPv6 information request, prototype DS-Lite, IPv4 routing, RA, DHCPv6 server, `HealthCheck`, `EventRule`, and `DerivedEvent`.

API names and implementation strategies have changed substantially since this version as part of pre-release cleanup. For current usage, refer to the `Unreleased` section above and the `examples/` directory.
