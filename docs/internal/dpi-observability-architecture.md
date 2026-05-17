# DPI observability architecture backlog

## Scope

This note records the implementation direction for using DPI as an observation
and analysis signal in routerd. DPI must not sit on the forwarding verdict path
for this work. Packet handling should remain copy-based and best-effort: if DPI
is slow, missing, or crashing, traffic forwarding continues and only analysis
quality degrades.

The goal is to make connection and client analysis more useful than port-based
guesses. The first useful outcome is a lower `port-fallback` ratio and richer
flow metadata in the Web Console, not application-aware blocking.

## Current state

- `routerd-firewall-logger` receives NFLOG copies from nftables accept/drop
  logging and can call `routerd-dpi-classifier`.
- `routerd-dpi-classifier` exposes HTTP+JSON over a Unix socket and supports
  `builtin`, `ndpi-agent`, and `auto` engines. `auto` falls back to the built-in
  parser when the agent is unavailable, returns unknown, times out, or errors.
- The external `ndpiReader` option is deprecated compatibility only. Its
  presence does not change classification behavior.
- `routerd-ndpi-agent` is implemented as an optional CGO/libndpi service. The
  non-`libndpi` build reports an unavailable service boundary instead of
  silently pretending to classify.
- `traffic-flows.db` remains conntrack-derived and payload-free. It is enriched
  from `dpi_flow`, DNS/SNI/HTTP host metadata, resolved hostnames, and port
  fallback while preserving existing DPI fields when later conntrack updates do
  not carry payload-derived details.
- The Web Console separates protocol/application evidence from provider labels
  more clearly than before. Client summaries use the recent traffic window and
  map provider-only nDPI names such as Google, AWS, Microsoft, Apple,
  Cloudflare, and Nintendo back to the observed transport protocol where
  appropriate.

## Target architecture

Keep routerd core and the existing Go helpers independent of `libndpi`. Add a
separate optional native service for nDPI-backed analysis:

```text
nftables NFLOG packet copy
  -> routerd-firewall-logger
  -> routerd-dpi-classifier       pure Go facade, cache, fallback, API
  -> routerd-ndpi-agent           optional CGO/libndpi service
  -> firewall-logs.db dpi_flow
  -> traffic-flow enrichment / Web Console / client analysis
```

`routerd-dpi-classifier` remains the stable integration point. It owns timeout
policy, fallback to the built-in parser, source labeling, and compatibility with
callers. `routerd-ndpi-agent` owns `libndpi` flow state and native resource
lifetime.

## Process boundaries

### routerd-firewall-logger

- Continues reading NFLOG/pflog packet copies.
- Sends only candidate packets to `routerd-dpi-classifier`.
- Never waits on DPI for a forwarding verdict.
- Records accepted flow classifications into `dpi_flow`.
- Enriches deny events from cached `dpi_flow` entries when available.

### routerd-dpi-classifier

- Remains pure Go.
- Provides the public `/v1/status`, `/v1/healthz`, and `/v1/classify` API.
- Maintains engine selection: `builtin`, `ndpi-agent`, or `auto`.
- Calls `routerd-ndpi-agent` when enabled and falls back to the built-in parser
  on timeout, unavailable socket, unknown result, or agent error.
- Adds result source metadata:
  - `ndpi-agent`
  - `builtin`
  - `port-fallback`
  - future sources such as `dns-cache`, `sni-cache`, and `cloud-ip`
- Applies policy limits before forwarding work to the agent:
  - first payload packets per flow
  - copy range
  - flow TTL
  - flow limit
  - per-request timeout

### routerd-ndpi-agent

- Is optional and isolated from routerd core.
- Uses CGO and links to `libndpi`.
- Keeps nDPI detection modules and per-flow detection state in one process.
- Accepts packet observations over a Unix socket.
- Returns protocol, application, category, confidence, risk, and metadata.
- Exposes status counters and resource usage so routerd can show whether nDPI is
  actually contributing.
- Failure is non-fatal; systemd may restart it without disrupting forwarding.

## Configuration direction

Introduce a DPI classifier resource or extend the existing generated systemd
resources with a typed policy shape. Prefer a resource once the fields are
stable enough to expose:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DPIClassifier
  metadata:
    name: default
  spec:
    engine: auto            # builtin | ndpi-agent | auto
    socket: /run/routerd/dpi-classifier/default.sock
    ndpiAgentSocket: /run/routerd/ndpi-agent/default.sock
    firstPayloadPackets: 10
    copyRange: 2048
    flowTTL: 1h
    flowLimit: 100000
    requestTimeout: 200ms
```

`FirewallLog.spec.log.acceptSampleRate` still controls how much accepted traffic
is copied into NFLOG. The DPI policy controls what happens after the packet copy
reaches routerd.

## Data model backlog

- [x] Extend `dpi.ClassifyResult` with `Engine` and `Source`.
- [x] Extend `dpi.ClassifyResult` with typed protocol and risk fields such as
  `DetectedProtocol`, `MasterProtocol`, `ApplicationProtocol`, `Category`,
  `Risk`, and generic `Metadata`.
- [x] Keep existing fields such as `AppName`, `AppCategory`, `TLSSNI`,
  `HTTPHost`, and `DNSQuery` for API compatibility until a cleaner typed shape
  replaces them.
- [x] Add source and engine details to firewall log hints.
- [x] Add structured source and engine columns to `DPIFlowEntry` and propagate
  them to `traffic-flows.db`.
- [x] Add schema migration tests for new `traffic_flows` SQLite columns.
- [x] Add schema migration coverage for `dpi_flow` and `traffic_flows`
  source/engine columns.
- [x] Keep `traffic-flows.db` payload-free. Enrich traffic flows by joining
  against `dpi_flow`, DNS query history, SNI/HTTP host metadata, and port
  fallback.
- [x] Expose nDPI-agent status counters through the agent status endpoint.
- [x] Store enough cross-component counters to explain value in the Web
  Console:
  - packets observed
  - packets sent to built-in parser
  - packets sent to nDPI agent
  - nDPI classified flows
  - fallback classified flows
  - unknown flows
  - timeout/error counts

## Implementation backlog

### Phase 1: Make the current state honest

- [x] Rename user-facing engine labels from nDPI to built-in parser where
  appropriate.
- [x] Remove or de-emphasize `ndpiReader` status fields that imply active nDPI
  classification.
- [x] Keep `routerd-dpi-classifier` API stable.
- [x] Add tests proving `ndpiReader` availability does not change
  classification.

### Phase 2: Introduce engine abstraction

- [x] Add an internal classifier engine interface in `routerd-dpi-classifier`.
- [x] Move the existing parser behind a `builtin` engine.
- [x] Add result source and engine fields.
- [x] Add unit tests for agent fallback behavior and unknown results.
- [x] Add an explicit timeout-path unit test with a deliberately slow agent.
- [x] Update Web Console labels so provider labels are not treated as protocol
  labels in connection and client summaries.
- [x] Add Web Console source counters for `ndpi-agent`, `builtin`, and
  `port-fallback` where structured source data is available.
- [x] Add Web Console connection filtering by classification source
  (`dpi`, `port-fallback`, `identifying`, `none`).
- [x] Add Web Console traffic-flow filtering by structured source
  (`ndpi-agent`, `builtin`, `port-fallback`).

### Phase 3: Add optional `routerd-ndpi-agent`

- [x] Add a new command under `cmd/routerd-ndpi-agent`.
- [x] Keep the default command as an unavailable service boundary when the CGO
  `libndpi` backend is not enabled.
- [x] Build the `libndpi` backend only when CGO and `libndpi` headers are
  available through the explicit `libndpi` build tag.
- Define a small Unix-socket HTTP+JSON API:
  - [x] `GET /v1/status`
  - [x] `GET /v1/healthz`
  - [x] `POST /v1/observe-packet`
  - optionally `POST /v1/reset-flow`
- [x] Keep nDPI flow state in the agent, not in `routerd-dpi-classifier`.
- [x] Enforce flow TTL, flow limit, and inspected-packet limit in the service
  boundary.
- [x] Add a selftest/status path that reports whether `libndpi` is loaded.
- [x] Add a `libndpi`-tagged test that classifies a synthetic TLS ClientHello
  with the native backend.

### Phase 4: Wire service management

- [x] Render systemd/OpenRC/FreeBSD/NixOS service scaffolding for
  `routerd-ndpi-agent` only when configured by classifier engine selection.
- [x] Make `routerd-dpi-classifier.service` want and start after
  `routerd-ndpi-agent.service` when engine is `auto` or `ndpi-agent`.
- [x] Keep `routerd-firewall-logger.service` depending only on
  `routerd-dpi-classifier.service`.
- [x] Update `install.sh` dependency handling so `libndpi` is optional and
  explicit.
- [x] Ensure upgrades restart active helper services that are running deleted
  binaries.

### Phase 5: Improve analysis surfaces

- [x] Add Web Console observed-flow source metrics for nDPI, built-in, port
  fallback, and unidentified traffic where data is available.
- [x] Add Web Console service health metrics for DPI engine status and nDPI
  error counters.
- [x] Add explicit classifier timeout-rate counters.
- [x] Add client-level protocol/category summaries based on the same one-hour
  window used for traffic, DNS, firewall, and DHCP evidence.
- [x] Add filters for `source=ndpi-agent`, `source=builtin`, and
  `source=port-fallback`.
- [x] Keep destination provider labels separate from protocol/application
  labels.
- [x] Persist traffic-flow enrichment from `dpi_flow`, SNI, HTTP host, DNS
  query, resolved hostname, and port fallback.
- [x] Persist typed DPI fields (`detectedProtocol`, `applicationProtocol`,
  `category`, `confidence`, `risk`, and `metadata`) through `dpi_flow`, active
  traffic flows, control API JSON, and the Web Console classification path.
- [x] Keep read-only Web Console queries compatible with legacy SQLite files
  until the writer has had a chance to add the new DPI columns.

### Phase 6: Production evaluation on homert02

- [x] Enable `routerd-ndpi-agent` on homert02 only.
- [x] Keep `acceptSampleRate: 1` initially, but cap `copyRange` to 1536 or 2048
  bytes.
- [x] Confirm current nDPI-agent health on homert02, including `libndpiLoaded`
  and `errorPackets`.
- [x] Confirm current traffic-flow enrichment counts on homert02.
- [x] Capture full before/after metrics:
  - `port-fallback` ratio
  - unknown ratio
  - nDPI classified ratio
  - CPU and RSS for `routerd-firewall-logger`,
    `routerd-dpi-classifier`, and `routerd-ndpi-agent`
  - NFLOG backlog/drop indicators if available
  - Web Console first-load latency
- [x] Exercise rollback by disabling only `routerd-ndpi-agent`; built-in DPI
  remains.

Production notes from homert02 on 2026-05-16:

- The nDPI agent was healthy with `libndpiLoaded=true`, `libndpiVersion=4.2.0`,
  and `errorPackets=0`.
- Before the new deploy, the nDPI agent reported 3,118 active flows, 5,531
  observed packets, 4,791 backend packets, 1,530 classified packets, 3,261
  unknown packets, and 740 skipped packets.
- After enabling `copyRange: 2048`, `routerd` needed a service restart so the
  long-running daemon reloaded the edited config and regenerated
  `/run/routerd/firewall.nft`. A one-shot apply alone wrote the snaplen-enabled
  file used by that process, but the already-running daemon still had the
  previous in-memory config.
- The live nftables ruleset then exposed `snaplen: 2048` on routerd firewall
  log rules.
- A Web Console summary fetch with 600 traffic flows returned HTTP 200 in 2.73s.
  The sample contained 51 nDPI-agent flows (8.5%), 1 built-in flow (0.2%), 444
  port-fallback flows (74.0%), and 104 unidentified flows (17.3%).
- Process snapshots after deploy showed roughly 35 MiB RSS / 22% CPU for
  `routerd-firewall-logger`, 12 MiB RSS / 0.3% CPU for
  `routerd-dpi-classifier`, and 17 MiB RSS / 0.2% CPU for
  `routerd-ndpi-agent`.
- Host counters did not expose a direct NFLOG drop counter. Netlink inspection
  showed the firewall logger socket present, and UDP receive-buffer error
  counters were zero.
- The routerd supervisor restarted `routerd-ndpi-agent` quickly when it was
  stopped. A classifier self-test with an intentionally missing agent socket
  fell back to the built-in TLS SNI classifier, preserving analysis without
  inline packet verdict impact.

## Non-goals

- Do not use DPI for firewall verdicts in this work.
- Do not introduce NFQUEUE or inline packet verdict handling.
- Do not make routerd core depend on CGO or `libndpi`.
- Do not store packet payloads in persistent databases.
- Do not claim application identity is authoritative when the source is only a
  port guess or CDN/provider heuristic.

## Open questions

- Whether `DPIClassifier` should be a first-class resource or remain generated
  from `FirewallLog` until the configuration shape settles.
- Whether `routerd-ndpi-agent` should use HTTP+JSON initially or a smaller
  binary protocol after the API stabilizes.
- Which `libndpi` package names and versions are acceptable for Ubuntu, Alpine,
  FreeBSD, and NixOS.
- How much nDPI risk metadata should be exposed in the v1alpha1 API.
- Whether cloud IP/FQDN intelligence should live in the same classifier pipeline
  or a separate enrichment pipeline.
