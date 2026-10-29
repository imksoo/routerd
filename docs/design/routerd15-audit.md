# routerd 1.5 repository-wide rewrite audit

Status: in progress. This is a coverage record, not a declaration of compliance.
The user expanded the task from example fixes to the entire repository. Source
inventory and compilation do not establish semantic audit or interoperability.

## Scope and completion

Review all production paths and their callers against AGENTS.md, including Go
commands/packages, web console, scripts, tests, schemas, packaging and operator
documentation. Preserve required product functions. Rewrite the paths violating
those rules; do not manufacture churn in code that already serves its function.
Completion requires the semantic review of all scope, corrected call paths and
appropriate local/communication checks. No remaining row marked pending may be
reported as audited or compliant.

## Cross-cutting work

- Closed concrete resource-spec contract: implemented; all consumers must compile.
  JSON is decoded to a concrete spec at the boundary. Internal spec retrieval
  cannot silently reinterpret a map through JSON.
- Configuration ownership: generated ordinary field copies replace YAML cloning.
  Generation follows concrete Go definitions and is checked through the Makefile.
- Health: typed daemon observation and typed persistence read; remove the second
  event-based writer of current health. This requires updated IPC on both ends.
- MTU: explicit resource handling replaces reflective field-name guessing.
- Old standalone IPv4/Health application paths: remove unreachable functions and
  tests whose only subject was those superseded paths; retain active FreeBSD paths.
- State: closed stores and invalid encodings report errors rather than success.
- Persistence: one schema 1500 shared by status and ownership. Remove old JSON
  backends, additive migrations and no-op Save calls; reject old DBs explicitly.
- Planning: read-only current observations and existing synchronous planners;
  remove temporary writable SQLite and simulated controller/event execution.
- Cancellation: policy observations retain their caller context; delayed derived
  publication checks its owner context and logs errors.
- Inventory example: controlled metadata endpoints/CLI fixtures prevent accidental
  instance metadata access during ordinary tests.

Each item still requires its executed checks below. These changes do not establish
that unrelated controllers, front-end code or packaging have been fully reviewed.

## Go inventory

The table records the initial scope inventory (before removal), not current file counts.
It includes production files, not generated assets or *_test.go files.
"In progress" means selected bodies/call paths reviewed; it does not mean the
package is fully audited. "Pending" means inventoried/scanned only.
"Bodies reviewed; integration in progress" records complete body reading by the
owning reviewer(s), including the affected callers, while repair integration and
qualification remain open. It is not a compliance certification. Original file
counts stay fixed so removals and newly written files do not inflate coverage.

| Directory | Production files | Semantic review |
| --- | ---: | --- |
| `cmd/routerctl` | 26 | Bodies reviewed; integration in progress |
| `cmd/routerd` | 29 | Bodies reviewed; integration in progress |
| `cmd/routerd-arp-observer` | 4 | Bodies reviewed; integration in progress |
| `cmd/routerd-bgp` | 1 | In progress |
| `cmd/routerd-dhcp-event-relay` | 1 | Bodies reviewed; integration in progress |
| `cmd/routerd-dhcp-fingerprint-watcher` | 1 | Bodies reviewed; integration in progress |
| `cmd/routerd-dhcpv4-client` | 4 | In progress |
| `cmd/routerd-dhcpv6-client` | 3 | In progress |
| `cmd/routerd-dns-resolver` | 9 | In progress |
| `cmd/routerd-dpi-classifier` | 1 | Bodies reviewed; integration in progress |
| `cmd/routerd-eventd` | 1 | Bodies reviewed; integration in progress |
| `cmd/routerd-firewall-logger` | 3 | Bodies reviewed; integration in progress |
| `cmd/routerd-healthcheck` | 1 | In progress |
| `cmd/routerd-ndpi-agent` | 4 | Bodies reviewed; integration in progress |
| `cmd/routerd-pppoe-client` | 4 | Bodies reviewed; integration in progress |
| `cmd/routerd-ra-observer` | 4 | In progress |
| `cmd/routerd-schema` | 1 | Bodies reviewed; integration in progress |
| `cmd/routerd-vrrp-vmac` | 1 | Bodies reviewed; integration in progress |
| `internal/cmd/specgen` | 1 | Bodies reviewed; integration in progress |
| `internal/freebsdpcap` | 1 | Bodies reviewed; integration in progress |
| `internal/hostcmd` | 1 | Bodies reviewed; integration in progress |
| `internal/mapsort` | 1 | Bodies reviewed; integration in progress |
| `internal/statusvalue` | 1 | Bodies reviewed; integration in progress |
| `internal/stringutil` | 1 | Bodies reviewed; integration in progress |
| `pkg/api` | 13 | Bodies reviewed; integration in progress |
| `pkg/apply` | 5 | Bodies reviewed; integration in progress |
| `pkg/bgp` | 1 | Bodies reviewed; integration in progress |
| `pkg/bgpdaemon` | 2 | In progress |
| `pkg/bus` | 1 | Bodies reviewed; integration in progress |
| `pkg/config` | 22 | Bodies reviewed; integration in progress |
| `pkg/conntrack` | 1 | Bodies reviewed; integration in progress |
| `pkg/conntracktuning` | 1 | Bodies reviewed; integration in progress |
| `pkg/controlapi` | 5 | Bodies reviewed; integration in progress |
| `pkg/controller/bfd` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/bgp` | 7 | In progress |
| `pkg/controller/chain` | 34 | In progress |
| `pkg/controller/conntrackobserver` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/dhcpv4client` | 1 | In progress |
| `pkg/controller/dnsresolver` | 1 | In progress |
| `pkg/controller/eventfederation` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/eventsubscription` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/firewall` | 1 | In progress |
| `pkg/controller/framework` | 1 | In progress |
| `pkg/controller/ingressservice` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/mobility` | 37 | Bodies reviewed; integration in progress |
| `pkg/controller/mobilityfib` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/nat44` | 1 | In progress |
| `pkg/controller/pppoesession` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/provideraction` | 1 | Bodies reviewed; integration in progress |
| `pkg/controller/vrrp` | 2 | Bodies reviewed; integration in progress |
| `pkg/daemonapi` | 2 | Bodies reviewed; integration in progress |
| `pkg/derived` | 1 | In progress |
| `pkg/dhcpeventqueue` | 3 | Bodies reviewed; integration in progress |
| `pkg/dhcpfingerprint` | 1 | Bodies reviewed; integration in progress |
| `pkg/dhcpv4client` | 2 | In progress |
| `pkg/dnsresolver` | 1 | In progress |
| `pkg/dpi` | 2 | Bodies reviewed; integration in progress |
| `pkg/dynamicconfig` | 2 | In progress |
| `pkg/dynamicconfig/codec` | 2 | In progress |
| `pkg/egressroute` | 1 | Bodies reviewed; integration in progress |
| `pkg/eventconsumer` | 1 | Bodies reviewed; integration in progress |
| `pkg/eventd` | 7 | Bodies reviewed; integration in progress |
| `pkg/eventfile` | 1 | Bodies reviewed; integration in progress |
| `pkg/eventlog` | 2 | Bodies reviewed; integration in progress |
| `pkg/eventrule` | 1 | In progress |
| `pkg/federation` | 2 | Bodies reviewed; integration in progress |
| `pkg/federationguard` | 1 | Bodies reviewed; integration in progress |
| `pkg/firewallbackend` | 1 | In progress |
| `pkg/ha` | 1 | Bodies reviewed; integration in progress |
| `pkg/healthcheck` | 10 | In progress |
| `pkg/hostdeps` | 1 | Bodies reviewed; integration in progress |
| `pkg/hybrid` | 1 | Bodies reviewed; integration in progress |
| `pkg/ingressdrain` | 1 | Bodies reviewed; integration in progress |
| `pkg/inventory` | 1 | Bodies reviewed; integration in progress |
| `pkg/ipsec` | 1 | Bodies reviewed; integration in progress |
| `pkg/lan` | 1 | Bodies reviewed; integration in progress |
| `pkg/lan/dhcpv4server` | 1 | Bodies reviewed; integration in progress |
| `pkg/lifecycle` | 3 | Bodies reviewed; integration in progress |
| `pkg/lock` | 2 | Bodies reviewed; integration in progress |
| `pkg/logstore` | 7 | Bodies reviewed; integration in progress |
| `pkg/manageddaemon` | 1 | Bodies reviewed; integration in progress |
| `pkg/mobilityconfig` | 4 | Bodies reviewed; integration in progress |
| `pkg/netconfigbackend` | 1 | Bodies reviewed; integration in progress |
| `pkg/nflog` | 2 | Bodies reviewed; integration in progress |
| `pkg/nftstate` | 1 | In progress |
| `pkg/observabilitypipeline` | 1 | Bodies reviewed; integration in progress |
| `pkg/observe` | 2 | Bodies reviewed; integration in progress |
| `pkg/otel` | 3 | Bodies reviewed; integration in progress |
| `pkg/pdclient` | 4 | In progress |
| `pkg/platform` | 5 | Bodies reviewed; integration in progress |
| `pkg/plugin` | 6 | Bodies reviewed; integration in progress |
| `pkg/pppoeclient` | 1 | Bodies reviewed; integration in progress |
| `pkg/provideraction` | 4 | Bodies reviewed; integration in progress |
| `pkg/providercontract` | 1 | Removed: unused production simulator |
| `pkg/providerinventory` | 2 | Bodies reviewed; integration in progress |
| `pkg/raobserver` | 1 | In progress |
| `pkg/render` | 32 | Bodies reviewed; integration in progress |
| `pkg/resource` | 3 | Bodies reviewed; integration in progress |
| `pkg/resourcequery` | 2 | Bodies reviewed; integration in progress |
| `pkg/sam` | 1 | Bodies reviewed; integration in progress |
| `pkg/samenrollment` | 1 | Bodies reviewed; integration in progress |
| `pkg/servicemgr` | 1 | Bodies reviewed; integration in progress |
| `pkg/source/daemon` | 1 | Bodies reviewed; integration in progress |
| `pkg/state` | 12 | Bodies reviewed; integration in progress |
| `pkg/status` | 2 | Bodies reviewed; integration in progress |
| `pkg/subprocessio` | 1 | Bodies reviewed; integration in progress |
| `pkg/sysctlprofile` | 2 | Bodies reviewed; integration in progress |
| `pkg/tailscale` | 1 | Bodies reviewed; integration in progress |
| `pkg/version` | 1 | Bodies reviewed; integration in progress |
| `pkg/vrf` | 1 | Bodies reviewed; integration in progress |
| `pkg/vxlan` | 1 | Bodies reviewed; integration in progress |
| `pkg/webconsole` | 25 | Bodies reviewed; integration in progress |
| `pkg/wireguard` | 1 | Bodies reviewed; integration in progress |

## Other surfaces

- Web console source and generated assets: refresh, current-config snapshots and
  diff paths reviewed and fixed; remaining UI/enrichment review is pending.
- Shell/Python test and lab tools: semantic review in progress; full remaining release/lab bodies are not yet reviewed.
- Build, installation, packaging, release and platform integration: installer
  config/restart-result paths reviewed and fixed; remaining review is pending.
- Schemas/examples/operator documentation: update contracts changed here; complete
  surface review remains pending.
- Real daemon/kernel communication: blocked by current Unix socket/Netlink
  restrictions; PVE/private-lab readiness has not been established.

## Executed checks

Record completed commands and failures for each published checkpoint. Do not turn
missing environment capabilities or an incomplete suite into a passing gate.

## Executed evidence for the cross-cutting checkpoint

Go 1.25.9, Linux/amd64, CGO_ENABLED=0 for project release builds. Checks apply
to the committed source tree; no live router or production data was changed.

- PASS: full focused packages in `make check-routerd15-focused` (configuration,
  spec copies/decoding, dynamic config/codecs, rendering, plugins, SQLite state
  and ownership, Health, advisory selection, derived events and worker status).
- PASS: selected command/chain tests for read-only planning, canonical config
  commit, priority result publication and typed-route expiry/withdrawal.
- PASS: schema generation and schema consistency checks.
- Full test execution is not established. The earlier full attempt hit denied
  Unix-socket/Netlink operations (also reproduced on the baseline), then automatic
  approval review stopped a metadata request from an example test. The example
  test now uses owned loopback metadata servers and fake provider CLIs, and its
  full package passes. No permission bypass was used.
- NOT RUN: unrestricted Unix/Netlink integration, independent-peer traffic,
  two-WAN forwarding/failure/recovery, PVE VM and physical NIC qualification.

The audit remains in progress. This checkpoint is not a release or a claim that
all packages meet every AGENTS.md rule. In particular, remaining status-map
consumers, worker lifetimes, protocol exchanges, static preparation and non-Go
surfaces still need semantic review and corresponding implementation.

## Further implementation and whole-suite findings

- Trace the actual CLI entrypoints, all init/global references and method bodies
  before deleting free functions. This conservative source call graph found 140
  unreachable routerd functions and 110 unreachable routerctl functions after
  the first checkpoint. Remove those old apply/inspection branches and tests
  that only exercised them. Active controller and inspection API paths remain.
  Reachability analysis is not a semantic audit of all reachable code.
- DHCPv6-PD now has one prefix-exchange retry owner for Solicit, Request, Renew
  and Rebind. A lost Request retries the same transaction with updated elapsed
  time, then returns to discovery after its bounded attempt budget. Match client,
  selected server and IAID; the daemon receive path selects its configured IA.
  Zero timers receive delayed renewal/rebinding times, zero-validity prefixes
  withdraw, and Solicit carries the configured prefix-length hint.
- Discard an unusable IA/prefix at its option boundary while retaining independent
  DNS data. The relevant rules are RFC 9915 sections 14.2, 16 and 21.21–21.22:
  <https://www.rfc-editor.org/rfc/rfc9915.html>.
- RA parsing now obeys declared IPv6 length, source/hop/code/checksum and option
  framing requirements. Ethernet padding is excluded; an invalid independent PIO
  does not erase valid route/DNS options. See RFC 4861 section 6.1.2:
  <https://www.rfc-editor.org/rfc/rfc4861.html#section-6.1.2>.
- Share DHCPv4 Ethernet/IP/UDP framing checks between Linux packet capture and
  FreeBSD BPF. Reject short/truncated/fragmented lengths rather than replacing an
  invalid IP total length with capture length or slicing past it.
- DHCP workers own cancellation for stop commands and process signals, with
  bounded HTTP shutdown. Remove the unused config-update success stub and reject
  reload of immutable supervisor-supplied settings. RA observer rejects unknown
  commands instead of acknowledging them as executed.
- Full local execution completed after the metadata-fixture repair. That attempt
  had 98 passing packages, 16 failing packages and 13 without tests. The failures
  exposed the lost DB-format explanation and the retained derived-host plan;
  repair the error boundary and make the rollback test require the complete plan.
  Make trace-contract tests explicitly sample spans, independent of the host's
  sampler setting (the baseline reproduced that environment-dependent failure).
  Selection/status unit fixtures now own link observations instead of requiring
  a host iproute2 command. None of these changes establishes real forwarding.

### Open implementation work at the earlier checkpoint

These are source findings, not merely VM-test omissions:

- Review DHCPv4 renewal/rebinding/expiry and command ownership as a complete
  exchange. Frame bounds and worker cancellation do not establish that exchange.
- The minimal PD model selects one prefix in its configured IA. Multiple-prefix
  updates/revocations, information-request reliability, and complete server status
  handling still require a coherent implementation and independent-peer checks.
- Continue semantic review of the pending inventory, including config preparation,
  remaining observation-map consumers, recovery/pending operations, web console,
  scripts and packaging. Do not label the project fully rewritten or compliant
  while these are open.

### Verification of the further implementation

- PASS: expanded `make check-routerd15-focused`, including full protocol/worker,
  resource-query and framework packages, plus command/chain selection, apply,
  schema-error, dry-run rollback and withdrawal regressions; `go vet ./...`.
- PASS: generated configuration/control/OpenAPI schema consistency.
- PASS: Race Detector for 15 core/worker packages and selected real command/chain
  regressions (2 additional packages). These are local fixtures, not live peers.
- PASS: project Linux daemon/provider builds and static linking checks.
- PASS: FreeBSD daemon/provider cross-builds and test compilation via
  `make check-freebsd-cross-compile`. `/bin/true` runs no FreeBSD test bodies;
  this is compilation evidence only.
- NOT PASS: whole native suite. After the identified logic/fixture corrections,
  complete cmd/routerd and chain reruns still fail on Unix sockets and unavailable
  host link/Netlink observations. The installer also requires missing ShellCheck.
  Keep the unrestricted full test and real-peer gates; do not add blanket skips.

The final report must still say that the repository-wide rewrite and semantic
review are incomplete. The known open implementation work above is not resolved
by successful compilation, race checks or this audit document.

## State-variable write contract

Replace Set/Unset/Forget results with `(Value, error)` and make Delete return an
error. SQLite read-only/closed/failed mutations cannot return an applied value.
The evented adapter reports unavailable persistence instead of fabricating a
value. Memory stores keep the same consuming contract with explicitly transient
writes; remove the last obsolete Save methods from the affected fixtures.

Propagate those errors through every production variable-write caller: the PD
observation recorder fails its apply operation, and ingress drain/undrain fails
its control request when persistence fails. Ingress drain observation now depends
on Reader and evaluates expiry without deleting operator intent as a read side
effect. Invalid persisted JSON also fails status listing instead of becoming a
successful synthetic error-status map.

This resolves the previously listed variable-write error finding. It does not
close the remaining protocol, ownership, preparation or inventory review items.

State-write contract checks: PASS for the expanded focused Makefile gate and
`go vet ./...`, all native package/test compilation, Race Detector on state,
ingress drain/controller and web console, and the PD write-failure regression
through the real command producer. Read-only/closed SQLite and corrupted JSON
fixtures exercise actual persistence failures; they do not qualify peer traffic.

## Failure and recovery implementation, 2026-10-29

The following extends the earlier checkpoint. It is still a partial semantic
audit of the inventory above. Parallel implementation and reciprocal source
review covered the actual paths below, including their callers and failure
effects; this is not evidence that the remaining inventory has been reviewed.

- DHCPv4: one client owns discovery, selection, request, INIT-REBOOT, T1 renewal,
  T2 rebinding, expiry, NAK, release and explicit restart. The daemon's once and
  persistent modes use that client. Raw Linux/FreeBSD paths validate IPv4 and
  nonzero UDP checksums, permitting IPv4's optional zero UDP checksum. The lease
  controller journals owned address, route and DNS operations before effects,
  retries failed cleanup and does not adopt preexisting matching resources.
- DHCPv6: multiple IA prefixes with separate validity, omitted-prefix retention,
  explicit revocation, valid selected-prefix projection and a new snapshot
  contract. Information requests, transient sends, NoBinding and bounded release
  share the client owner. Late replies cannot revive an expired exchange.
- Partial configuration mutation takes the mutation gate before reading and
  merging the canonical configuration. Concurrent apply/delete operations retain
  earlier committed changes; dry-run does not acquire the mutation gate.
- When conditions use error-returning persistence reads. SQL/closed-store errors
  and invalid object-status JSON stop the operation rather than becoming a fresh
  unknown observation or a false condition that triggers teardown. Propagation
  includes the actual apply, route, VXLAN and VRRP callers.
- nftables acknowledgements bind to the desired content and expire. Invalidate
  before effects, acknowledge only successful effects, and retry failed apply or
  withdrawal even if the desired file already exists. The MTU and policy-route
  consumers use the same contract and propagate receipt errors.
- NAT withdraws unavailable translations and omitted families. Typed ingress
  observations drive backend selection and scoped drop/reject rules. PF uses a
  single runtime anchor for translations and availability filtering; the root
  firewall no longer duplicates static translations. Inconsistent PF controller
  flags fail before effects. Firewall status writers preserve ingress-owned
  observations. PF retains existing states; actual state behavior needs a peer
  check. Unsupported independent multi-backend PF sourceHash/random is an error,
  rather than silently selecting the first backend.
- ICMP probes use the configured socket steering and correlate peer, fresh ID,
  sequence and payload. Cancellation interrupts an in-flight socket operation.
- Final BGP removal belongs to the same actor that owns watch/FIB writes.
  Persist a pending-removal operation before acknowledged supervisor shutdown,
  clear explicitly owned FIB routes (including retained routes), then retire
  restart state and complete the operation. Failed or uncertain completion keeps
  the fence through retry/restart. The service-unit and BGP-only callers share
  that actor and honor service-manager/dry-run restrictions.
- DNS reload returns an acknowledgement for the worker's applied config digest.
  Failed/missing acknowledgements remain errors and retry with unchanged desired
  files; preparation does not publish zone success. Query-log replacement is
  staged with the runtime and failed staging retains the previous runtime.
  Cache identity includes question class, flags and EDNS parameters; cache hits
  age record TTLs, preserve EDNS flags, and echo the current query. Retired pools
  cancel their queries and close idle HTTP connections. Runtime files are written
  atomically. See RFC 2181 section 5 and RFC 2308 section 5 for cache lifetimes.
- Web console responses snapshot the current declaration once. Frontend refreshes
  use the current view and reload configuration. Configuration diff uses linear
  space and cancellation; generated patches are checked by actual patch application.
  Installer compatibility rewriting is removed; failed restarts/recovery return
  a failing exit code.

The canonical-commit tests use sandbox controller effects, while still committing
the actual canonical YAML and generation. IPv6 host-policy fixtures own all
artifact paths and command observations, including the existing nft command
boundary. These tests do not execute host firewall changes.

### Findings remaining at checkpoint e9e317f

These were source findings, separate from missing VM evidence. The next pass
below addresses the named handover/preparation/PD/cancellation paths; it does not
complete the remaining inventory.

- Mobility snapshot collection could publish handover-release ownership events
  before the old local BGP withdrawal succeeds. Move the release decision into
  the typed plan and publish only after the responsible withdrawal, preserving
  failure/restart/federation semantics. The subsequent pass below rewrites it.
- Static SAM/dynamic preparation redecoded, cloned and validated parts of
  the configuration on observation ticks. The once-per-configuration rule is not
  yet satisfied throughout the chain; see the subsequent pass below.
- PD state had internal encoded-string transfers and duplicate projections
  in generic current status. The new protocol snapshot does not resolve every
  controller/store representation; see the subsequent pass below.
- Most mobility bodies, event/action/federation paths, CLI commands, UI enrichment,
  scripts and operator documentation have only been inventoried or partially
  read. Neighbor/VPN/log query cancellation also needs further work.

Do not describe this branch as fully rewritten, fully audited, or fully compliant
with AGENTS.md. Keep the complete test gate and independent-peer/target-OS gates.
This environment denies AF_UNIX and raw socket operations, and has no established
PVE access. Chromium download returned HTML rather than an archive, so the added
browser regression has not run. Cross-compilation is not target-OS execution.

### Validation of implementation checkpoint e9e317f

The published source is `e9e317f7c6022af29f916c77c4f2b7625c7c9012`.
Local Git and the published GitHub tree were compared before these final checks.
Go 1.25.9 was restored after the executor restart; checks used `GOMAXPROCS=2`
and `GOFLAGS=-p=2`. No GitHub Actions wait was used.

- PASS: expanded `make check-routerd15-focused`, including the new actual DHCP
  effect/removal/eligibility paths, config concurrency, read failures, nft retry,
  PF flag consistency and BGP removal. Its final `go vet ./...` passed.
- PASS: `make check-spec check-schema` for generated specs and all three schemas.
- PASS: targeted Race Detector checks for DHCPv4/v6 producer and controller
  ownership, lifecycle/conditional reads/config mutation, nft/PF recovery,
  ICMP/BGP removal and DNS reload/cache/log/TLS/lifetime paths. The final DHCPv4
  nil-router/unmanaged-history/deadline guards were rechecked after restoration.
- PASS: Linux project daemon/provider builds with `CGO_ENABLED=0` and
  `make check-linux-static`; the built daemon reports commit `e9e317f`.
- PASS: complete `make check-freebsd-cross-compile`, including project
  daemon/provider binaries and all test compilation. `/bin/true` executes no
  FreeBSD test body.
- PASS: frontend typecheck/production build, real patch application for config
  diffs, focused actual-installer restart/config preservation checks, and CI's
  ShellCheck arguments over 19 scripts with official ShellCheck 0.10.0.
- NOT PASS: complete native `go test ./... -timeout 180s`: 102 packages passed,
  14 failed, and 11 had no test files. All 51 top-level failures are AF_UNIX
  restrictions, including delayed startup/ARP tests whose server cannot create
  its Unix socket. Existing Netlink-dependent unit fixtures now own their link
  or FIB observations; required route/effect assertions were retained.
- NOT EXECUTED: real netns shell acceptance suites, independent DHCP/DNS/BGP
  peers, native supervisor/kernel recovery, native FreeBSD execution and browser
  regression. The Go `tests/netns` package checks shell fixtures, not forwarding.

These results establish a substantial implementation checkpoint, not completion
of the repository-wide audit, real communication qualification or release.

## Native ownership and current-observation pass

This pass follows published checkpoint `64d1043`. The changed paths and their
actual callers were reviewed together; the inventory outside this scope remains
pending or partial.

- Static handover planning is read-only. The source withdraws BGP ownership and
  persists a typed native withdrawal request. SAM, IPv4 route and address owners
  invalidate old acknowledgements before effects and acknowledge the exact
  transfer and plan only after their owned effects are withdrawn. The publisher
  rechecks the current persisted request under the same native-effect lock through
  event recording. Pending acquisitions precede kernel mutations and survive
  failed final saves and restart. Failures preserve retryable ownership.
- The target requires the exact group, pool, new operation ID, address, source,
  target and live finite release. Ordinary DHCP/ARP expiry and self-RIB history
  cannot bypass this gate. Five-minute proof renewal reconfirms BGP and current
  intent, rechecks time after waits, and permits legitimate delivery effects
  toward the new remote owner. The barrier certifies routerd-owned effects;
  arbitrary administrator effects are not silently adopted or removed.
- BGP dynamic additions use a version-2 journal and a stable native source
  identifier before AddPath. Lost UUID/final-save and policy-update failures stay
  recoverable through the real control handler and helper restart. Listing fails
  on unknown/corrupt journals. Source-specific replacement/deletion preserves
  other local sources and peer-origin paths in the real GoBGP server. This is
  in-process GoBGP evidence, not independent-peer wire qualification.
- Validated static configuration is prepared once per publication. Declaration
  and preparation are captured together for observation-driven updates and
  rollback. Dynamic admission, expiry, masks, conditional dependencies and
  enrollment claim validity remain live. Actual callbacks bind their status
  writer to the same captured router, including the shared BGP actor, avoiding
  stale owner references after a shape-preserving update.
- EventRule, DerivedEvent and Plugin changes replace their generation. Retired
  event consumers and fired timers complete before replacement is acknowledged;
  cancellation fences delayed publication. DHCPv4's existing actor updates its
  prepared interface and status writer together under its own mutex, preserving
  old owned effects until the corresponding replacement succeeds.
- PD has one typed current observation with protocol-second lifetimes and parsed
  timestamps. The actual daemon-status producer uses the existing transactional
  status/event owner. Generic status writers preserve that section; inspection,
  CLI and LAN consumers use typed reads. Old encoded internal lease transport
  and the unused standalone PD controller are removed. Source changes, release
  and stale snapshots must not revive withdrawn authority.
  Client selectors that never selected an external lifecycle now fail explicitly;
  the supported default remains `routerd-dhcpv6-client`.
- LAN current addresses inherit remaining preferred/valid PD lifetimes in the
  kernel, including same-address renewal. Default native reads distinguish
  unknown, absent, tentative and usable addresses. Pending writes and exact old
  address tombstones precede mutation; failed cleanup retains ownership. Stale
  authority is rechecked after waits. Finite rounding cannot extend the upstream
  deadline, and infinite previous-prefix deadlines cannot overflow native values.
  DS-Lite shares exact LAN-managed /128 sources without a second address writer;
  separately derived sources have their own finite-lifetime ownership journal.
- Ordinary IPv4 routes retain pending and applied identities across inactivity,
  device changes, failed writes/deletes and restart. Failed observations are
  errors. Exact Linux route priority/protocol and supported FreeBSD native
  identity distinguish ownership; unsupported nonzero FreeBSD route metrics
  fail explicitly. Dry-run reports rendering without inventing kernel ownership.
- FreeBSD rendering retains every managed RA interface, applies explicit native
  RA flags after CPE boot defaults, zones the full link-local gateway range and
  avoids static-route label collisions. Tests evaluate the generated rc.conf
  with a real shell; native FreeBSD execution remains a separate gate.
- Noncommittable mutations retain terminal failure history and return failure;
  the runtime does not publish uncommitted declarations. Delete intent and
  confirmed canonical deletion are distinct fields. Transport timeouts report
  unconfirmed outcome, and rollback history does not label the latest failed
  attempt as the current configuration.
- Console commands and SQLite enrichment queries inherit their request/owner
  context. Command stdout is separate from diagnostic stderr, including private
  WireGuard dump data. Successful section collection is explicit; failed/skipped
  sections retain the previous view, while observed-empty sections clear it.
  Metrics use the same contract to clear absent labels/counts. The overview has
  one recurring fetch owner and displays partial collection errors.
- `routerctl firewall test` shares the renderer's static nftables ordering,
  policy and selectors. It reports indeterminate input/runtime cases instead of
  predicting a false verdict with a copied role matrix. A configured DoH TLS
  name now passes operator configuration validation into the worker input; the
  existing real TLS/SNI regression remains part of the local DNS gate.

### Remaining scope

Most event/action/federation paths, Mobility's broader status readers, auxiliary
daemons, scripts, packaging and operator documentation still need semantic review
against their actual callers. Global forwarding sysctls are shared by several
features and are not retired as an address-specific handover cleanup. A single
native receipt must never be presented as proof of arbitrary host-state absence.

Independent peer/client traffic, real netns acceptance, native supervisor/kernel
crash recovery, native FreeBSD behavior and management recovery remain required
target gates. AF_UNIX/raw/Netlink restrictions and absent established PVE access
have not been resolved by adding command fixtures. No production cutover occurs.

### Executed local checks for this pass

Go 1.25.9 on Linux/amd64, with `GOMAXPROCS=2` and `GOFLAGS=-p=2`.
These checks extend the e9e317f evidence; they do not change its historical
results. GitHub Actions completion is not awaited.

- PASS: expanded `make check-routerd15-focused`, including the added recovery
  target and `go vet ./...`. Final ownership/lifetime refinements also have
  focused ordinary and Race Detector checks through their actual callers.
- PASS: generated specs, all three schemas and their website copies, including
  handover ID, typed PD inspection, deletion target and supported PD client.
- PASS: Race Detector regressions for prepared declarations/current actors,
  BGP native identifiers/pending additions, native handover owners/publication,
  SAM acquisition/GARP recovery, ordinary routes, PD atomic source/history,
  LAN/DS-Lite lifetimes and exact withdrawal, event consumer retirement and
  mutation outcomes. Command fixtures do not establish real forwarding.
- PASS: full web console package, frontend typecheck/production build and
  shipped-app refresh checks in real Chromium 153. Partial/failed responses
  retain earlier data, display errors and allow other sections to refresh;
  successful empty observations clear prior rows. Real HTTP/SQLite/OTel
  collection tests cover the corresponding gauge transitions. This supersedes
  the earlier browser-download restriction for this pass only.
- PASS: complete `make check-freebsd-cross-compile`, including all project
  daemon/provider builds and test compilation with `CGO_ENABLED=0`. The
  `/bin/true` runner executes no FreeBSD test body.
- PASS: `make build-daemons build-provider-executors check-linux-static
  ROUTERD_OS=linux`, with `CGO_ENABLED=0` for the project release build paths.
- NOT PASS: complete native `go test -json ./... -timeout 180s`: 102 packages
  passed, 14 failed, and 11 had no test files. The 52 top-level failures require
  AF_UNIX, including delayed-start/ARP tests waiting on a forbidden Unix listener.
  Both non-environmental failures found in the first whole-suite run were fixed:
  PD dependency suppression now preserves its original test premise and actual
  canonical observation, and tunnel teardown uses a current typed route receipt
  with exact native facts. Required assertions and the full gate remain intact.
  The final run uses official ShellCheck 0.10.0; installer static analysis passes.
- NOT EXECUTED: successful production Unix HTTP transport roundtrips, real netns
  acceptance/independent network peers, native kernel/supervisor crash recovery,
  native FreeBSD execution and PVE management/traffic qualification. The browser
  regression above does not replace these gates.

## Second semantic pass, 2026-10-29

The first native-ownership checkpoint is published as `64b29b8`. This additional
pass continues the repository-wide work; it is not a release or a completion
claim. The table records source reading, while the following lists distinguish
implemented paths from the remaining integration work.

### Implemented paths and scope-local evidence

- Provider operations: one database-side process guard, running-only result CAS,
  pending undo, explicit unknown forward/undo results and no age-based requeue.
  Unknown results fence other keys as well. `action resolve` records independently
  verified operator evidence under that same guard. Target/parameters/undo remain
  typed inward and are decoded strictly at SQLite. Completed undo invalidates the
  earlier capture assignment; unresolved transitions are protective, never proof
  of successful ownership. Actual SQLite/process/CLI race fixtures pass.
- Federation: group identity is bound to authenticated input; an event ID binds
  its complete immutable fact. Refresh cannot replace its payload/group/source.
  Arithmetic, body bounds, expiry and cancellation are enforced at their owning
  boundaries. Actual applied/pending SAM receipts prevent self-capture feedback
  after a failed native withdrawal. Loopback/SQLite/native-command-fixture races
  pass; native packet communication remains a separate gate.
- Subscription and exporter lifetime: stop/cancel waits for the actual owner;
  generation replacement cannot outlive an unacknowledged predecessor. A blocked
  exporter retains the actual cluster lease until it exits. Subscription desired
  output and whole-batch deduplication commit together. Archive sink selection
  fails before deleting if any required destination is absent, disabled or would
  filter the archive record. Real process/SQLite/flock/webhook fixtures pass.
- Configuration/API: strict YAML/JSON shapes, catalog-consistent kind/API groups,
  exact metadata and explicit unsupported-selector diagnostics. The migrated required current
  resource reads return errors instead of silently choosing absence/fallback.
  Control requests reject unknown intent/trailing bodies and share the encoded
  size budget with clients. Complete response decoding does not confirm a partial
  response. Plugin credential headers are removed at the IPC boundary, and a slow
  manual plugin cannot overwrite concurrent publication. Focused races pass.
- State: malformed present variables, null native receipts and null/malformed PD
  authority are errors, preserving the source record through failed writes.
  Variable decoding no longer serializes internal status just to reinterpret it.
  Event attribute corruption fails the required event read. Provider journal
  timestamps and shapes are validated at the SQLite boundary. Shared-writer
  event budgeting notices external commits through SQLite data_version.
- Native/runtime boundaries: BFD and static VIP/CARP record pending intent and
  exact ownership before effects; failed cleanup retains receipts. PPP status is
  bound to the current desired supervisor token and public session identity.
  Ingress fallback is bound to the current target. Required source read errors,
  failed native effects and failed result publication remain failures. Native
  BFD/CARP/PPP communication and target FreeBSD behavior remain unqualified.
- Auxiliary workers: generation-bound PPP observations and secret-safe logs;
  exact VMAC parent/type/mode ownership; bounded NFLOG/DPI/ARP packet reads;
  fingerprint transaction identity; explicit firewall verdicts; owned child/socket
  shutdown. Ordinary fixtures do not mutate host networking. Optional libndpi
  builds and native independent-peer behavior are not established here.
- Console/observation: complete snapshot accounting, accurate FreeBSD PF tuples,
  explicit unavailable/empty/declaration states, current bounded redacted diffs,
  request/SSE cancellation, IAID/DUID distinction and no fabricated MAC vendors,
  reachability, byte counts or firewall classifications. Actual Chromium and Go
  HTTP/SQLite regressions pass. The full handwritten UI body pass is complete;
  the routerctl/routerd diagnostic and effect bodies have now also been read.
  The separately recorded defects remain open implementation work.

### Still open at this pass

- Validate the integrated native Bridge and WireGuard ownership changes,
  including boot, peer/member changes, foreign resources, failed cleanup and
  restart. Scope-local fixture/race tests pass; global gates are recorded below.
  Native networking and peer behavior remain separate acceptance gates.
- Mobility core, BGP, command, packaging, release and lab-driver body reviews
  identified the concrete findings linked below. Reading those bodies does not
  establish compliance; the recorded implementation gaps still require repair.
- Required production resourcequery consumers now use error-bearing reads.
  Static exact /64 RA ranges are emitted, but dynamic prefix selection and finite
  lifetime semantics remain open. Prefix/PrefixFrom must match native output.
- Distributed capture assignment retains all configured non-drained members;
  automatic dead-member redistribution is not implemented safely. Existing local
  generations, BGP marker absence and per-host file locks are not cross-node
  fencing authority. Current provider executors also classify ambiguous mutating
  CLI failures as ordinary failures; their complete unknown-outcome contract
  still needs correction before claiming safe cloud retry/recovery.
- Linux VXLANSegment has no confirmed live owner in the actual caller path;
  FreeBSD VXLAN stop receipts still need protection against a same-name foreign
  replacement. These are source implementation findings.
- Packaging/release/lab follow-up includes restart/rollback ordering, safe liveISO
  mount cleanup and firstboot validation ordering, durable mutation launch before
  child effects, pinned admission inputs, exact process/token cleanup and truthful
  measured protocol results. Remaining CLI bodies also expose unchecked duration
  arithmetic, lossy observation reads and mutation before output validation.
- FreeBSD 14.3 forward-zone FirewallRule parity is unfinished. This pass rejects
  unsupported forwarding rules rather than silently ignoring ToZone; supported
  router-input rules remain separately scoped. A complete outgoing/tagging design
  and native qualification are still required for cross-zone parity.
- The integrated local gates below have run after source convergence. Their
  results do not close the remaining implementation or native acceptance gaps.
- Unrestricted Unix/raw/Netlink, PVE, independent peers, provider-cloud mutations,
  physical NICs and native FreeBSD qualification remain unrun in this environment.

The follow-up body review identified further concrete native, BGP, command and
release defects. Their exact repair boundaries are recorded in
[routerd15-open-findings.md](routerd15-open-findings.md). They remain implementation
work even where existing tests pass. Source reading is not a compliance verdict.

### Integrated validation of the second semantic checkpoint

Final source convergence was checked on 2026-10-29 with Go 1.25.9 and
ShellCheck 0.10.0, before committing this checkpoint.

- PASS: `make check-routerd15-focused check-schema check-website-schemas`,
  including recovery/robustness tests and vet. Generated schema content and the
  website copies agree.
- PASS: `make check-freebsd-cross-compile build-daemons
  build-provider-executors check-linux-static`. This compiles FreeBSD tests; it
  does not execute them on FreeBSD. Linux release binaries use CGO_ENABLED=0.
- COMPLETE RUN, NOT PASS: `go test -json ./... -timeout 180s` yielded 103 passing
  packages, 14 failing packages and 9 packages without tests. All 52 failing
  top-level tests require forbidden AF_UNIX listeners, directly or through a
  startup timeout; the two failing subtests are the same restriction. Assertions
  were retained. Ten real integration failures from the preceding run were fixed
  and rerun: subscription lifetime, lifecycle catalog, NAT fixtures, native MTU
  expectations and reviewed golden output. No unexplained failure remains in
  this run, but the complete gate has not passed here.
- PASS: whole-repository test compilation; full golden normal/race tests; actual
  Chromium console regressions; UI typecheck/build; wizard fixture regeneration
  and schema validation. All eight wizard configurations also passed production
  config admission. The full wizard daemon gate is blocked by AF_UNIX.
- PASS: final subscription normal/race repetition (20 runs each), final VRRP/CARP
  race regressions, and the required WG/CARP driver syntax/config render checks.
- NOT RUN: production Unix HTTP roundtrips, isolated kernel/netns communication,
  real provider mutations, native FreeBSD/PVE/independent-peer qualification and
  optional libndpi builds. No GitHub Actions result was awaited.

The focused gate deliberately remains separate from the complete gate. The
concrete open findings above remain work even when their existing tests pass.
