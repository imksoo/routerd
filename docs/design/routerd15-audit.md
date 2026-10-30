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
- Persistence: one schema 1501 shared by status and ownership. Remove old JSON
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
  [RFC 9915](https://www.rfc-editor.org/rfc/rfc9915.html).
- RA parsing now obeys declared IPv6 length, source/hop/code/checksum and option
  framing requirements. Ethernet padding is excluded; an invalid independent PIO
  does not erase valid route/DNS options. See RFC 4861 section 6.1.2:
  [RFC 4861 §6.1.2](https://www.rfc-editor.org/rfc/rfc4861.html#section-6.1.2).
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

### Separately fixed follow-up checkpoints

These checkpoints were tested from separate immutable worktrees with Go 1.25.9
and ShellCheck 0.10.0 where applicable. Fixture builds in worktrees use
`-buildvcs=false`, as release builds do. GitHub Actions was not awaited.

- `d77a848a`: BGP daemon policy and local-path ownership, format-3 recovery and
  typed control boundaries. Full race tests pass for `pkg/bgp`, `pkg/bgpdaemon`,
  `pkg/controller/bgp`, `pkg/controller/mobility` and `pkg/config`; scoped vet
  passes. The daemon race run passes with its five AF_UNIX-listener tests
  excluded because native socket creation is forbidden here. An attempted
  listener test failed with EPERM. Actual loopback GoBGP session tests verify
  export/edit/withdraw and exact-source cleanup. This is not independent-peer
  or kernel FIB qualification.
- `7aa0c71d`: release QA and standalone certification share one durable process
  and cleanup owner. The exact tree passes all **476** offline QA tests in
  183.594 seconds, with nine existing tool/environment skips; the actual
  release-certification offline entry point and ShellCheck on four affected
  shell scripts also pass. No paid resource, native service installation or
  PVE lifecycle was executed.
- `ee1fc40c`: live ISO mount/workdir ownership and failure-preserving firstboot.
  The full `tests/liveiso` package passes under race, including execution of
  production shell bodies for mounted/unowned workdir refusal, failed unmount
  retention, cache validation and validation/render failure before DHCP removal.
  Shell syntax and ShellCheck pass. Package ownership is now preserved by the
  builder and old caches are rejected, but an actual image build, image mode
  inspection and VM boot remain unexecuted.

These are bounded implementation checkpoints. Native address/route/tunnel owners,
RA/LAN lifetime, runtime reload, HTTP/IPsec and observation-counter changes are
being integrated separately. The complete repository gate has not been rerun on
these follow-ups; the second checkpoint's complete-run counts above must not be
presented as current complete-run results.


### Native/runtime integration checkpoint

The fixed source tree is `edf269ad80c1f3d82367651baec95ed081d4583c`,
published as `a93e55a9`. Its local validation commit was `0f186b3`; only commit
metadata changes between those revisions. Checks used Go 1.25.9, ShellCheck
0.10.0, `GOMAXPROCS=2` and `GOFLAGS="-p=2 -buildvcs=false"`.

This checkpoint replaces duplicate static IPv4, ordinary-route, delegated-LAN,
DS-Lite and VXLANSegment writers with typed pending/applied native receipts.
The recorded parent identity remains usable for safe child withdrawal after the
last parent declaration is removed. One RA service owns finite prefix/DNS
lifetimes and withdrawals. Runtime reload, native process acknowledgement,
artifact activation, scoped IPsec receipts and optional traffic counters preserve
failed or unknown observations. Public apply errors retain progress facts without
embedding raw causes. Separate operator references describe native, RA and IPsec
contracts and cutover.

- PASS: `make check-routerd15-focused check-schema check-website-schemas`,
  including recovery/robustness targets and whole-repository vet. The integrated
  systemd removal path now receives its actual BGP receipt store; unrelated
  fixtures explicitly report known absence and reject any BGP receipt write.
- PASS: targeted regressions for DS-Lite shared-LAN receipts, systemd units,
  public apply-failure output, PD source changes, retained FreeBSD rendering and
  installer ShellCheck. The previous generation's late BGP removal is fenced.
- PASS: Linux release daemon/provider builds with `CGO_ENABLED=0` and static
  binary checks; complete FreeBSD/amd64 daemon/provider builds and test
  compilation through `make check-freebsd-cross-compile`. This compiles FreeBSD
  tests and does not execute them on Linux.
- COMPLETE RUN, NOT PASS: `go test -json ./... -timeout=180s` completed all 127
  packages: **104 pass, 14 fail, 9 have no test files**. Of 3,825 top-level tests,
  3,764 pass, 55 fail and six skip. Failures comprise 51 direct AF_UNIX EPERM,
  two direct Netlink EPERM and two Unix-listener prerequisite timeouts inferred
  from their actual call paths and same-run socket failures. The native implicit
  DUID and interface-observation positive assertions remain intact. There are no
  unexplained assertion failures in this run; the complete gate did not pass.
- NOT RUN: unrestricted Unix/native networking, independent peer/client
  qualification, actual FreeBSD/PVE lifecycle and an ISO build/boot. Separate
  pending policy-route, Mobility, VXLANTunnel and SAM changes are not included in
  these counts. No GitHub Actions result was awaited.


### Atomic enrollment checkpoint

Current SAM enrollment admission and revocation commit the claim and durable
nonce ledger in one SQLite transaction. Admission rechecks the current row and
global revocation inside that transaction. Topology GET reads one consistent
snapshot without writing repairs; missing claim/identity digests and old HMAC
payloads without explicit `directMesh=true|false` are rejected. Current schemas
and operator instructions match that contract.

On the isolated `feeb740` plus exact SAM source projection, focused race tests
pass for routerd, Mobility, ControlAPI and routerctl. Full Mobility,
SAM-enrollment and schema-generator race tests pass, as do state race tests,
seven-package vet and FreeBSD/amd64 routerd test compilation. Actual SQLite
trigger failures on both revocation writes, reopen/retry, nonce rotation,
replay rejection and physically read-only GET are exercised. These checks do
not establish independent RR/leaf interoperability or native forwarding.


### Kernel-scoped BGP FIB recovery

Native BGP FIB receipts use a strict SHA-256 key of the current kernel scope;
the payload must match that key and, on FreeBSD, the actual FIB/table. A new
kernel world starts from its own receipt and exclusive native add. Previous
worlds, ambiguous operations and old unscoped rows are retained untouched and
are never used as current ownership evidence. Returning to an old world still
fences its unknown pending operation. A stopped controller generation cannot
remove the next generation's receipt or native effects.

The isolated `feeb740` plus exact scoped source projection passes focused BGP,
removal, generation and systemd tests, including race repetition three times,
four-package vet and FreeBSD/amd64 test compilation. Real SQLite tests cover
world changes, foreign presence, lost acknowledgement, reopen and key/payload
mismatch. Full BGP/state race checks also pass. Actual kernel reboot and packet
forwarding remain unrun; FreeBSD deletion still lacks a full-tuple atomic CAS.


### FreeBSD lifecycle harness ownership

The native lifecycle script now drives routerd serve and its ApplyAPI for
main-config restart, idempotence and empty-desired stop. A small test-only child
launcher records exclusive durable intent and retains the actual unreaped child
handle; disk/request PIDs cannot select a signal target. TERM/KILL completion
requires Wait acknowledgement. Epair deletion validates both endpoints' boot,
jail, index, MAC and private description, then verifies absence. Unacknowledged
cleanup retains work/evidence and cannot publish success. Unsupported targets
exit as not-run before effects. The exact production dnsmasq receipt decoder is
exercised by its offline shell-boundary regression.

All five actual child/shell-admission race tests, scoped vet, FreeBSD test
compilation, shell syntax and ShellCheck pass on the isolated source. This is
preparation for native VM evidence, not execution of BPF/DHCP/PD/DNS traffic in
this environment. A separate primary-source review also found that FreeBSD14.3
IPv4 address add is not atomically exclusive against unrelated writers; the
shared Static/Mobility native contract now records that limitation explicitly.

## Journal transactions and canceled one-shot work

This checkpoint closes reproduced defects in the existing SQLite journal and
one-shot controller paths:

- Event payload accounting now counts every stored column, including both
  `type` and `topic`. Required status/history batches reserve their actual
  bounded payload, rather than guessing that identifiers fit in a fixed allowance.
- Capacity checks, retention pruning and appends commit in one transaction for
  ordinary events and paired status/feature events. Two live SQLite writers
  cannot each reserve the same free slot. An insert failure rolls back pruning,
  status and required history together; cached usage changes only after commit.
- Read-modify-write transactions acquire SQLite writer admission before reading
  their premise. Read-only snapshot transactions retain concurrent-writer access.
  This prevents stale-snapshot upgrade failures when a status transaction and
  a second writer overlap; no test deadline is increased.
- Future-dated private Mobility plan observations are unknown input and cannot
  authorize current native effects. The existing expiry and Pool-invalid boundary
  handles the result; no clock-skew fallback or old-format reader is introduced.
- A canceled one-shot controller returns while another mutation holds the
  exclusive gate, without entering its effect. The earlier blocking lock waited
  indefinitely for that unrelated mutation.

The frozen checkpoint passes the full state, dynamic-config codec and controller
framework packages under the race detector, relevant vet and FreeBSD/amd64
compile-only checks. `make check-routerd15-focused check-schema
check-website-schemas` also passes, including whole-repository vet. Real
two-connection regressions cover concurrent capacity,
writer admission, read-only coexistence and failed paired writes. Linux daemon
and provider-executor builds pass the static-link check. Native packet
forwarding is a separate qualification gate.

## Isolated E2E deployment admission and private generation

The E2E deployment body now rejects a failed unit inventory or stop operation,
then requires inactive/failed state and zero MainPID/ControlPID before replacing
router binaries or configuration. The original body ignored failed stops and
could treat a failed inventory pipeline as an empty successful observation.
Config-generation failure now propagates through the command-substitution caller
instead of returning a deployable output path.

Generated secret-bearing YAML is replaced through a private temporary inode and
atomic rename, including retries from an earlier 0644 output file. Its directories
are private and generated WireGuard key files retain 0600 permissions. A failed
renderer leaves the preceding YAML intact. Router names must be safe output
filenames before key generation.

The frozen four-script checkpoint passes the complete generation and reuse
fixtures, all 11 diagnostics tests, the representative-redundancy offline entry
point, shell syntax and ShellCheck. The stop regression executes the actual
remote function with intercepted systemctl/sudo calls; it performs no host service,
SSH, PVE or cloud mutation. This closes those concrete harness findings, not the
remaining native process/route/evidence qualification work.

## Recovery validation and incremental publication

The recovered, frozen source tree
`fa6be9b07fa07c2c196e468048bda8399687a9b9` was published as `c921af2a`.
Its local validation commit was `5c41e5cc`; all 2474 tracked blobs were verified
before execution. The checks below apply to that tree, not later commits or
unpublished work. Go 1.25.9 was used with `GOMAXPROCS=2` and
`GOFLAGS="-p=2 -buildvcs=false"`.

| Check | Observed result |
| --- | --- |
| Whole-repository `go vet ./...` | Pass |
| Generated schemas and website schema consistency | Pass |
| Linux daemon/provider builds and static-link checks | Pass; all 20 Linux/amd64 binaries were static, built with CGO disabled |
| Complete FreeBSD/amd64 test compilation | Pass; `go test -exec /bin/true ./...` compiles but does not execute FreeBSD tests |
| Complete normal test execution | Failed; 128 packages: 105 passed, 14 failed, nine had no tests. Top-level tests: 3776 passed, 90 failed, six skipped |

Of the original 90 failures, 35 stopped before installer assertions because the
source export lacked repository metadata. After adding independent Git metadata
without changing tracked source, and installing ShellCheck 0.10.0, the complete
installer package rerun had 35 passes and one failure. Its remaining failure
directly reported AF_UNIX permission denial. This rerun corrects the missing
fixture evidence; it is not a new passing whole-repository run.

The other failures comprised 46 direct AF_UNIX permission denials, five secondary
readiness timeouts with that same test's recorded AF_UNIX denial, two direct
Netlink permission denials, and two timeouts whose syscall error was not observed.
The last two tests' source requires a Unix listener, but the attempted syscall
trace was itself denied. They remain unresolved rather than being labeled proven
environment failures. Required native-positive assertions remain intact.

Subsequent changes are published independently after relevant local checks:

- `e5108018`: resolve declared Health interface bindings once and reject
  conflicting declarations; preserve an explicit zero mark.
- `2f41ecc4`: bound RA diagnostic writes and require capture/writer exit
  acknowledgement. Disk I/O no longer holds the observation mutex.
- `c4dc3706`: preserve large upstream DNS UDP responses and honor downstream
  EDNS size/truncation while TCP receives the complete answer. Actual loopback
  exchanges, focused race tests, vet and FreeBSD compilation pass.
- `7bd90e46`: reject a second BGP daemon in the same native world before effects;
  prevent public socket/state paths from replacing its singleton lock. Real
  child-process rejection, focused race tests, vet and FreeBSD compilation pass.
- `80fc3af8`: recheck current IPv4 route intent after the pending-withdrawal write;
  retain the acknowledged native receipt when that intent recovers. Actual SQL
  reopen/failure/caller regressions, focused race tests, vet and FreeBSD
  compilation pass; no native route mutation is qualified by those fixtures.
- `0a4e01fb`: propagate observer cancellation through FreeBSD capture startup,
  pipe reads and child Wait. Actual Linux child-process normal/race checks,
  scoped vet and FreeBSD compilation pass. Observer package tests retain seven
  failures: six direct AF_UNIX denials and one timeout without an observed errno.

The complete gate has not been rerun on these later commits. Native FreeBSD,
unrestricted Unix/Netlink, PVE and independent forwarding/peer qualification
remain unexecuted. No GitHub Actions completion was awaited. Ongoing changes
are preserved on a separate WIP branch, explicitly pending validation; their
presence there does not establish completion or compliance.

## Current implementation validation: 78665041

The final code checkpoint is published as `78665041aaa748a33c2272d7648fb1248b960404`,
tree `766f18d47def78efb868fdd001ebfde0712c5d54`. Its local validation commit is
`5b7ca4d16a0dec534785430c237fc6a6984c93f1`; only Git commit metadata differs.
All 2513 tracked blobs were verified before execution and rehashed afterward,
with zero mismatches and a clean independent Git worktree. Later documentation
updates do not change that tested code. Checks used Go 1.25.9, ShellCheck 0.10.0,
`GOMAXPROCS=2` and `GOFLAGS="-p=2 -buildvcs=false"`.

This checkpoint includes typed current Health identity/freshness, atomic durable
EventRule and DerivedEvent state/output, bounded fair rule processing, acknowledged
capture lifetime, IPv4 withdrawal rechecks, wildcard solicitation receive and DNS
resource/label boundaries. DNS leases arriving during reload wait for table
handover before acknowledgement; a canceled waiting request cannot mutate the
new table. Separate zone resources sharing a DNS apex remain independently
selectable. These changes use their existing production owners and callers.

The complete normal suite was rerun after correcting two Health test fixtures.
The source-error fixture now declares the HealthCheck that it references; the
metrics fixture supplies a fresh typed sample. Existing error/no-effect and
Healthy-gauge assertions were retained, with no production fallback or
freshness relaxation. This complete run includes the later DNS handover test;
its counts are measured, not inferred from an earlier run.

| Check | Result on this source tree |
| --- | --- |
| Whole-repository vet | Pass |
| Generated schemas and website consistency | Pass; all three generated schemas and website copies agree |
| Linux daemon/provider release build and static-link checks | Pass; exactly 20 Linux/amd64 static ELF binaries, CGO disabled |
| Complete FreeBSD/amd64 test compilation | Pass; 128 packages, including 119 test binaries; compile-only, with no native execution |
| Complete normal tests | Failed: 129 packages, 104 pass, 16 fail, nine without tests; 3888 top-level tests pass, 57 fail, six skip |

The 57 failed tests are classified from their actual output:

| Classification | Tests |
| --- | ---: |
| Direct AF_UNIX permission denial | 47 |
| Readiness timeout with AF_UNIX denial recorded by that same test | 5 |
| Direct native interface/Netlink permission denial | 3 |
| Timeout without an observed target errno | 2 |

The last two are ARP ignored-MAC command readiness and ControlAPI transient Unix
startup. Their source requires a Unix server, but their actual target syscall
error was not observed; they remain unresolved. The full gate did not pass.
No additional product-logic assertion failure was observed in this run, which
does not qualify the assertions blocked by missing native capabilities. Existing
required tests and the new actual RA socket/stop tests remain intact.

The installer package records 35 passes and one direct AF_UNIX failure in this
same complete run. All Health fixture assertions and the actual DNS reload
handover regression pass. Relevant feature race tests, fault/reopen checks and
independent source reviews passed before publication. Linux binaries were built
and inspected, not executed as live router daemons. Actual native FreeBSD/PVE,
independent client/peer traffic, forwarding and image boot remain unqualified.
The implementation gaps in [remaining findings](routerd15-open-findings.md)
remain open; these checks are not whole-repository compliance certification.

## Resumed native ownership and DNS recovery checkpoint, 2026-10-30

Resumed from the observed public `8458ab2a`, rather than the earlier handoff's
`973340bc`. The existing 321 changed/new paths were first preserved at WIP
`a66e6739`. Feature work and review corrections were then committed and published
incrementally. A WIP checkpoint is preservation, not a successful acceptance gate.

The final checked code is public commit
`3d247aed92d4f618d76597bb7085ac4879ec5745`, tree
`23fd6ed20fea8cc77fc96b5f142df851254d240c`. The tracked tree was clean at launch;
all 2640 tracked blobs were checked against that tree during and after execution.
Subsequent audit/PR updates are documentation only. Go 1.25.9 was used;
GitHub Actions was not awaited.

Implemented and reviewed paths now include:

- A shared kind-aware native owner for both VXLAN kinds, current HA gates through
  child/native waits, no duplicate boot creators, and the updated real netns
  driver. Omitted FreeBSD DF retains native behavior; explicit DF is unsupported.
- One exact IPv4/IPv6 policy receipt owner, exclusive route acquisition, precise
  selector withdrawal, unknown-presence fences and current Health/source checks.
  Numeric iproute2 observations distinguish empty tables from failed reads.
- Mobility route/address scope, index and virtual-parent receipts with explicit
  pending/ACK transitions and current typed source reads at effect boundaries.
  The four reproduced final-native-read races now have passing regressions.
- Custom RA self-DNS selection from current native/PD validity and finite per-
  address RDNSS deadlines, including DAD and cross-interface identity.
- BGP community selection on independently cloned outbound paths. Static/dynamic
  admission, actual TCP UPDATE/withdrawal and retained local RIB are checked.
  The pinned GoBGP subset retains its license and documents four upstream edits.
- Named DHCP lease/tombstone projection committed atomically in the existing
  SQLite journal (format 1501). Fresh workers read that journal before serving,
  including when the consumed runtime JSON is unchanged. Actual SQLite reopen,
  early loopback queries, client reassignment and IPv6 aliases have regressions.
  DNS query/probe owners join before handoff; staged/retired listener admission
  and wildcard TCP source selection have actual TCP regressions.
- Bounded QA proxy header/write/connect I/O and preserved CONNECT payload bytes.
  Resolver lifetime and concurrent handler admission remain separate open work.

| Check on the fixed code | Observed result |
| --- | --- |
| Whole-repository vet | Pass, including the migrated VXLAN netns driver |
| Generated config/control/OpenAPI and website schema consistency | Pass |
| Linux release binaries and static-link checks | Pass; all 20 binaries, CGO disabled; embedded commit equals the public code checkpoint |
| Complete FreeBSD/amd64 test compilation | Pass; compile-only, no native test execution |
| Complete normal Go suite | 129 packages: 104 pass, 16 fail, nine without tests; 3957 top-level tests pass, 57 fail, six skip |
| Related feature race checks | Pass, including the final combined DNS worker/controller/runtime/state and Mobility owners |
| Release-QA offline suite | 480 cases: 471 pass, nine existing skips, no failures |

All 57 failed Go tests have an observed permission denial: 48 direct AF_UNIX
failures, six Unix-readiness failures with the actual denied listener reported,
and three Netlink/native-interface failures. The two formerly unclassified
timeouts now report their actual AF_UNIX denial. Required positive assertions
remain; no skip was added to hide a blocked assertion. These failures do not
qualify the behavior they could not exercise.

The first integrated pass additionally found an obsolete netns controller caller
and stale render snapshots; both were repaired before the fixed-source checks.
Render changes are the required `script-on-renewal` line in 19 existing snapshots.
Independent review reproduced and then verified the fixes for source changes
during Mobility native waits, stale DNS names on client reassignment, staged-port
old-policy exposure and wildcard TCP source bypass.

Native Linux/FreeBSD forwarding, independent router peers/clients, PVE, systemd
restart, provider mutations and image boot remain unexecuted. The repository-wide
semantic audit and the implementation findings still listed in
[open findings](routerd15-open-findings.md) remain incomplete. This checkpoint is
not a release or a whole-product qualification.

## Canonical activation, probes and native parent checkpoint, 2026-10-30

The resumed branch was read and verified at public `921d6f61`. Completed changes
were published separately; intermediate combined work was also preserved on an
explicit WIP branch. The final checked checkpoint is public
`c5085332aabb6f14707ea1c9c390dbaa651777cd`, tree
`1755f47b2f1f14ce746b9aaee9915d24a9b85d02`; the equivalent local commit is
`a8c35cd595c09565321308b2865324f420551b6c`. All 2672 tracked blobs matched that
tree during and after the final whole-suite/build checks. Later changes are
audit documentation only. Checks used Go 1.25.9, ShellCheck 0.10.0,
`GOMAXPROCS=2` and `GOFLAGS="-p=2 -buildvcs=false"`; GitHub Actions was not awaited.

Implemented and independently reviewed production paths:

- Canonical durable replacement precedes candidate activation. The selected live
  Runner owns HA admission, consumes the existing apply attempt's commit callback
  and runs required controller effects. Generation replacement transfers the
  actual locked lease descriptor; adding HA acquires only once. Rejected writes,
  unconfirmed durability and standby admission cannot activate the candidate.
  Postcommit failures retain new desired intent and report actual active state.
  One control-http owner prepares and publishes listeners before success history.
- Lease/path identity is rechecked after canonical and status persistence waits.
  Lost selected authority fences its generation; loss confined to a new candidate
  preserves the still-owned old generation. Actual admitted HA observations are
  published before When selection; failed status writes suppress effects. Real
  SQLite, LogRetention, file-lock and TCP listener regressions cover these paths,
  including retry gaps, self-mutation, bind failure, retry and self-disable.
- Doctor performs bounded DNS UDP/TCP, configured egress and LAN probes. Current
  SQL/configuration conditions are checked before and after communication, and
  unavailable observations remain explicit. Local static questions use RD=0;
  actual UDP/TCP resolver regressions prove same-revision workers do not export
  nonrecursive misses. Native-local endpoint observation precedes private wire
  traffic. This does not authenticate unrelated/older DNS listeners or qualify
  router forwarding. The raw-ICMP fixture reports its capability limitation.
- Child static IPv4, route, delegated LAN and RA effects consume actual DS-Lite
  receipts/native cookies in addition to VXLAN proof. Pending/foreign parents,
  changed sources and masked declarations cannot authorize effects; recorded
  cleanup survives declaration removal. FreeBSD aliases share the native naming
  rule. PF, runtime NAT, MSS and service exceptions now resolve aliases for the
  explicit target OS through that same owner rule, including omitted names.
  Bridge, WireGuardInterface, VRF and TunnelInterface proof remains open.
- FreeBSD PF ordinary zone forwarding matches actual ingress and egress, keeps
  explicit decisions ahead of role defaults and separates local output. Native
  name bounds and every possible failover hairpin backend are checked. Unsupported
  translated-source conjunctions and unimplemented advanced selectors fail
  explicitly. Native PF parsing/packets and restricted route-to parity are unrun.
- QA proxy DNS runs in an owned, canceled/reaped child; admitted handlers are
  bounded and joined on shutdown. The four-point capture runner uses durable
  gated launch, Linux pidfd/birth/output identity and acknowledged exit before
  evidence copy. Actual child-process/fault regressions cover lost start/stop
  acknowledgements; no real remote packet capture was executed.

| Check | Observed result |
| --- | --- |
| Whole-repository vet and generated/website schemas | Pass |
| Linux release binaries and static-link checks | Pass; 20 binaries, CGO disabled; routerd/routerctl version output confirms the public checked commit |
| FreeBSD release build and complete test compilation | Pass; 20 binaries and 128 packages, compile-only |
| Complete normal Go suite | 129 packages: 104 pass, 16 fail, nine without tests; 4028 top-level pass, 57 fail, seven skip |
| Related race, persistence-fault and actual loopback checks | Pass; includes final combined canonical/HA/HTTP, DNS privacy/source freshness, DS-Lite and PF regressions |
| Release-QA offline suite | 495 cases: 486 pass, nine existing skips, no failures |

All 57 failed Go tests contain an observed permission denial: 48 direct AF_UNIX,
six Unix-readiness failures with the denied listener reported, and three
Netlink/interface failures. There were no additional product assertion failures
in the final run. These observations do not qualify blocked assertions, and the
full normal gate did not pass. Required positive assertions remain intact.

The first whole-suite run found eight stale FreeBSD PF snapshots and a missing
ShellCheck executable on its PATH. The snapshots were independently reviewed:
seven change only the intended PF filtering, and the rate-limit example now
records the documented unsupported-capability error. Their assertions were not
weakened. A final production-path review then reproduced raw DS-Lite aliases in
PF and runtime NAT despite the native owner creating a gif interface. The fix
uses explicit target-OS alias and service-hole producers, including the actual
runtime NAT controller. Independent actual-output regressions changed RED to
GREEN, and two additional snapshots now use the native gif names. Feature races
and independent fault/wire reviews passed at their corresponding checkpoints.
The complete normal suite, vet, schemas, QA, builds and final blob verification
were rerun on the exact published checkpoint above, with the real ShellCheck
executable on PATH.

Native Linux/FreeBSD forwarding, independent router peers/clients, target-storage
crash durability, PVE/systemd, cloud mutations and image boot remain unexecuted.
Userspace identity rechecks do not establish atomic native compare-and-change.
The full semantic audit and the other implementation gaps in
[open findings](routerd15-open-findings.md) remain incomplete. This is not a
release or whole-product qualification.

## On-prem Linux Bridge and process identity checkpoint, 2026-10-30

The implementation checkpoint is public
`ba054d2fd30653b01a8bc63b2c12c57a3af74e5a`, based on public `1630f2a5`.
The Bridge Go checkpoint is `aa9f3cac`; every Go source blob is unchanged in
`ba054d2f`. Later audit updates are documentation only. Checks used Go 1.25.9,
Linux 6.8.0-136-generic, iproute2 6.1.0, `GOMAXPROCS=2` and
`GOFLAGS="-p=2 -buildvcs=false"`. Normal tests ran as an unprivileged user.

Concrete body/caller review followed `bridge.go`, `bridge_ownership.go`,
`bridge_link_linux.go`, `native_parent.go`, the static address/route/delegated LAN
readers, RA transmission and the actual Runner dependency wiring. The initial
regression reproduced one address write on a foreign same-name Bridge. Children
now consume the existing concrete Bridge receipt and native identity. Active
admission requires the current acknowledged/healthy plan; recorded cleanup
retains its exact scope/index/cookie after declaration removal, during recovery
and during parent withdrawal. Receipt and source changes after observation waits
cannot authorize a child. The private receipt is format 2 with a required kernel
scope; old receipts are preserved and rejected without migration or data reset.
FreeBSD's existing boot backend is unchanged and its Bridge child proof is open.

Actual isolated-kernel execution then reproduced a creation bug hidden by the
command fixture: Linux 6.8 did not install the alias supplied to `RTM_NEWLINK`.
The owner now binds an exclusive create acknowledgement to a requested native
index, checks its current pending receipt/scope, installs the cookie on the exact
index/name/kind with an empty prior alias, and acknowledges native readback.
Cookieless present pending effects are neither adopted nor replayed. Removal
and changed plans preserve that uncertainty instead of deleting its receipt.
This sequence does not establish atomic CAS against unrelated native writers.

The opt-in native test uses new user/network/mount namespaces and a new sysfs
mount. Actual Bridge creation, native cookie/address observation, SQLite reopen,
unchanged ownership and exact child/parent withdrawal passed. Only `networkctl`
is excluded. This exercises actual controllers and kernel effects, not the
complete `serve` process, systemd, forwarding or independent peers.

Independent normal-suite work repaired the BGP runtime-directory fixture under
umask 0002, supplied the current typed HealthCheck/probe identity, preserved the
dedicated PPPoE projection owner, and made subscription-expiry scheduling
deterministic. No assertion was weakened to hide a denied product operation.

The full QA rerun exposed a separate process-census boundary: selecting the
innermost `NSpid` could address an unrelated thread instead of the intended
procfs entry and produce `pidfd_open` EINVAL. The supervisor now selects the
current caller's namespace column while retaining pidfd/procfs identity checks.
Unknown identities and EINVAL for the selected native process remain errors.
Boundary regressions first failed and then passed; the complete offline suite
was rerun after the repair.

| Check | Observed result |
| --- | --- |
| Bridge ownership target and related race checks | Pass; native opt-in remains separate from the normal target |
| Isolated Linux native Bridge/address/reopen/withdrawal | Pass; systemd and peer traffic excluded |
| Whole-repository vet and generated/website schemas | Pass at the unchanged Go checkpoint |
| Linux release/static checks | Pass; 20 binaries, CGO disabled |
| FreeBSD release/test compilation | Pass; 20 binaries and 128 packages, compile-only |
| Whole normal Go suite | 4070 top-level pass, 32 fail, seven skip; 117 packages pass, three fail, nine without tests |
| Release-QA offline | 499 cases: 492 pass, seven existing skips, no failures |
| Installer regressions | Pass after the process-census repair |
| Secret scans, formatting and source cleanliness | Pass at the implementation checkpoints |

The complete normal gate remains unqualified. Thirty-one failed tests report
actual permission denial, including the native-operation lock, production
`/run/routerd` or `/var/lib/routerd` paths and raw ICMP. The remaining failure,
`TestSystemdUnitControllerReportsHealthCheckRestartFailureAfterReady`, reports a
missing synchronous restart and is not classified as a permission failure.
Earlier fixed-source runs also failed the EventRule subcase of
`TestEventConsumerChangesReloadActualGeneration`; the final run passed without
an EventRule implementation change. That variation is not claimed as repaired.
Required assertions were not passed by running as root or modifying production
directories. These normal-suite failures and native/peer/platform qualification
remain separate follow-up work. No production deployment or cloud mutation was
executed.


## Packaged Linux service and independent IPv4 client checkpoint

Baseline remains `e3b49dbb69c7de97bab9595884745eb7cbe93d92` (pre-1.5 routerd).
All of its 87 registered resource kinds and 90 example files remain represented;
that inventory is not functional qualification. The final goal is preservation
of every routerd 1.0 function. A home/site router trial is an intermediate gate.

The product checkpoint `6135bb038dcad2805090506cc53b342b0c47391d` was built by
`make dist` with CGO disabled and tested in a dedicated Ubuntu 24.04.4 VM,
Linux 6.8.0-124-generic. The installed archive and its checksum, real systemd
unit and default filesystem paths were used. Initial tests exposed two product
integration defects: the packaged BGP unit lacked its ownership header, and the
service could not inspect or signal dnsmasq after it dropped UID. Regression
tests reproduced these failures before repair. The native capability test fails
at procfs observation without CAP_SYS_PTRACE and at pidfd signaling without
CAP_KILL; the corrected service passes both with exact identity checks intact.
The native test is now an explicit CI gate.

An independent BusyBox DHCP client ran in a LAN network namespace, with a
separate upstream namespace running dnsmasq and an HTTP peer. The executed
checks passed: DHCP address/gateway/DNS options, local and forwarded DNS over
UDP/TCP, the HTTP peer observing the translated source, service restart,
upstream disconnect/reconnect and managed-address/routing withdrawal. The peers
were checked before applying the candidate. The isolated guest needed conntrack
installed before this run; a prior missing-package run was not counted as pass.
Empty-config installation/apply/restart also passed. An earlier empty guest
without loaded conntrack reported Unavailable, which was not called Healthy.

At `4d767077c3e72194ae424de35ea5ffb7fc331206`, subsequent changes are test-only.
`GOMAXPROCS=2 go test -p 2 ./...` exited 0: 120 packages passed and nine had no
tests. Existing system-unit fixtures now use private BGP state paths. EventRule
fixtures enqueue their durable burst before starting consumption and assert
retirement of the old checkpoint; real timer-commit cancellation tests remain.
HTTP publication checks retain a bounded deadline without treating two seconds
as a product latency contract. Schema/example checks and all four website
locales built successfully. Existing documentation-link warnings remain.

These results do not establish IPv6 PD/RA/renewal/expiry, DHCP lease renewal,
WAN DHCP/PPPoE/DS-Lite, firewall isolation, reboot/crash recovery, 1.0 cutback,
FreeBSD feature parity or the remaining 1.0 function inventory. Those remain
required work; neither a limited trial nor passing unit tests closes the revision.
The earlier isolated FreeBSD 14.3 empty apply/restart and PF route-to/NAT44
smokes at `86ffc265` remain narrower native checks, not full FreeBSD qualification.

## Linux delegated IPv6 lifecycle checkpoint

Product `3da8d066e19535d27610d632391807db7968d1fc`, installed from `make dist`
in the dedicated Ubuntu 24.04.4 / Linux 6.8.0-124 VM, passes the independent
Kea 2.4.1 and Linux SLAAC client lifecycle. A real apply first reproduced normal
DAD wait being returned as `RA LAN readiness is unresolved`; the same failure
was reproduced in a local regression before repair. Pending DAD now withholds
positive advertisement without failing apply. DAD failure and unknown ownership
remain errors, with explicit tests.

Executed checks: /60 acquisition; finite PIO and RDNSS; kernel SLAAC and default
route; local/forwarded AAAA over UDP/TCP; peer-observed delegated IPv6 source;
actual Renew/Reply advancing the lease timestamp; systemd restart; upstream-stop
expiry withdrawing the router address and client default route while the daemon
remains active; traffic failure after expiry; empty apply and fixture cleanup.
The final reusable script passed with explicit negative assertions and successful
raw-output deletion. Procedure and boundaries are in
[development](../operations/development.md#linux-pdra-lifecycle-vm-gate), with
`scripts/linux-pd-ra-lifecycle-smoke.sh` and its committed JSON fixture.

At the product checkpoint the complete normal suite again passed: 120 packages,
nine without tests. Schema/examples, website schema copies and all four website
locales passed. The trial's preparation errors (Kea runtime directory, extracted
rdisc6 ownership and DNS forwarder order) were corrected; those failed attempts
were not counted as pass. WAN transit and return routing are fixture scaffolding.
WAN RA/IA_NA, prefix renumbering, DHCPv4 renewal, firewall isolation, reboot/crash,
1.0 cutback, FreeBSD client parity and the remaining 1.0 inventory stay open.

## Linux DHCP renewal and firewall withdrawal checkpoint

Product `ad686135b4a751cd99a0aba3d48553eb853e8f7c`, installed from `make dist`
in the dedicated Ubuntu 24.04.4 / Linux 6.8.0-124 VM, passes the independent
BusyBox DHCP and three-zone firewall lifecycle. The initial trial passed DHCP
renewal, DNS/NAT, isolation, restart and exception removal, then failed because
removing all firewall resources left the native filter active. The controller's
no-firewall early return skipped withdrawal. A separate real-kernel local test
also showed that a comment in a later table block did not set kernel ownership.
Both defects failed locally before repair. Creation now sets the native marker;
withdrawal verifies that marker and deletes the observed handle. Failed inventory
reads/deletes retain retry input, and foreign/unmarked tables are not removed.

The exact public `scripts/linux-dhcp-firewall-lifecycle-smoke.sh` passed with its
committed fixture: expected DHCP address/gateway/DNS, timer-driven renewal after
61 seconds, local/upstream DNS UDP/TCP, peer-observed NAT source, WAN-to-LAN and
LAN-to-management isolation, WAN-to-router DNS isolation, positive management
control, actual service restart, explicit exception add/remove, filter withdrawal
restoring baseline HTTP paths, empty apply, namespace cleanup and IPv4 forwarding
restoration. Denied HTTP requests were absent at the destination and the relevant
nft chain counters increased. Successful raw output was deleted automatically.
The old unmarked filter was manually removed during environment preparation;
that cleanup is not counted as successful product withdrawal.

Validation: normal `go test ./...` passed (120 packages, nine without tests),
focused actual-kernel withdrawal passed in an isolated local netns, schema and
examples passed, website schema copies and four website locales built. The first
normal-suite attempt found 16 Linux golden files needing the creation-time
comment; their diffs were verified to contain only that intended change before
updating and rerunning the suite. English/Japanese development procedures define
the setup, commands, positive/negative verdicts and cleanup.

This qualifies DHCPv4 **server** renewal with an independent client, not a WAN
DHCPv4 client or rebind/expiry. VM reboot, interrupted-operation recovery, 1.0
cutback, IPv6 renumbering/WAN RA/IA_NA and all other unqualified 1.0 functions
remain required. Existing tables without the native marker require explicit
operator resolution; automatic old-format adoption is not provided.

## Independent Linux peer across VM reboot and interrupted apply

The same formal product `ad686135` passed on the dedicated Ubuntu 24.04.4 router
VM (kernel 6.8.0-124) with a second Ubuntu 24.04.4 peer VM (kernel 6.8.0-117).
Guest VLANs 3131–3133 separate LAN, WAN and management traffic on the existing
isolated segment. A fixture boot unit recreates external guest ports only;
routerd restores addresses, routes, DNS/DHCP, NAT and filtering itself.

The router boot ID changed and its service automatically became active in about
40 seconds. The peer boot ID and udhcpc PID stayed unchanged; its original
120-second DHCP lease address continued renewing. Local/upstream DNS UDP/TCP,
peer-observed NAT source, management positive control, WAN/LAN/management
isolation and WAN DNS rejection passed after reboot. No controller errors or
service restart loop were observed. SIGKILL of the main process subsequently
recovered automatically with a new PID and increased NRestarts (about 16 seconds
observed), and the same peer checks passed again.

The interrupted ApplyAPI trial added a new DNS record, observed canonical file
replacement, and killed the exact original process through pidfd while the
client remained in flight. The client returned EOF with an explicitly unconfirmed
outcome. Automatic restart loaded the new record; the independent client resolved
it and passed all traffic/isolation checks without manual product repair. This
observation does not place the kill on a particular side of directory fsync and
does not prove power-loss durability or all interruption points.

Reproduction uses `scripts/linux-router-reboot-fixture.sh`, the existing DHCP
JSON fixture, `scripts/linux-interrupted-apply-smoke.py`, and the English/Japanese
development procedure. The initial peer helper name shadowed Python's `http`
module; it was corrected during preparation before product apply. No new product
code was needed for these successful recovery paths. Cross-version 1.0 cutback,
IPv6 reboot/renumbering, WAN protocol modes and remaining 1.0 parity stay open.

## Linux 1.0 cold cutover and cutback

The two-VM fixture passed `1.0 -> 1.5 -> 1.0 -> 1.5` using formal `make dist`
archives: baseline `e3b49dbb` and repaired candidate `b4965652`. The router used
Ubuntu 24.04.4 / Linux 6.8.0-124-generic; its independent peer used Ubuntu
24.04.4 / Linux 6.8.0-117-generic. Each transition rebooted only the router.
The peer boot identity and BusyBox DHCP PID stayed unchanged throughout.

The first candidate (`ad686135`) failed status qualification after upgrading
from the 1.0 generated systemd unit: the installer preserved its old capability
set, so the native dnsmasq owner could not inspect the helper after UID drop.
Local installer regressions failed first. The installer now regenerates owned
generated units from canonical config through the new binary's read-only render
command before activation. Config-derived access/telemetry remain intact; render
failure restores the previous payload before service recovery starts.

The repaired candidate passed initial cold upgrade, 1.0 recovery from a stopped
binary/config/state/unit recovery set, and restoration of its saved 1.5 state.
Every stage required Healthy/no current controller errors, systemd running,
local/upstream DNS UDP/TCP, peer-observed IPv4 NAT, management positive control,
WAN/LAN/management isolation, and a real DHCP timer renewal after reacquisition.
The same client address was reacquired; this was a planned outage, not hitless
lease continuity. No generated-unit hand repair was used to pass the candidate.

Empty apply withdrew addresses and firewall tables. Fixture shutdown also
stopped routerd through its temporary Requires dependency; cleanup now removes
that dependency and starts the empty product service. That final cleanup tail
was executed and verified: Healthy, zero resources, no controller errors, no
nft tables, forwarding disabled; peer namespaces/processes were removed.
The normal Go gate passed (120 packages, nine without tests), installer tests,
schema/examples, shell checks and four website locales passed. The reusable
English/Japanese development procedure retains the cross-version gate.
IPv6 cutover, WAN protocol modes and the remainder of the 1.0 feature inventory
remain unqualified by this IPv4 fixture.

## Native ownership and FreeBSD lifecycle checkpoint (2026-10-31)

The final scope remains every pre-1.5 routerd 1.0 function. The new static
`routerd15-feature-qualification.json` maps all 87 baseline resource kinds,
all 90 example artifacts (63 YAML files), and non-resource command, API, UI and
distribution scopes. Registration or file presence is not functional parity.
All whole-feature preservation/platform verdicts remain unknown until their
required behavior and recovery have been exercised.

The Linux ownership work is preserved on `work/routerd15-native-20261031`.
The implementation checkpoint `e8537199` and follow-ups `6a7c264e`, `bca3330e`
and `5c677869` distinguish native effects from completed ownership receipts:

- WireGuard child acquisition requires the actual parent cookie, index,
  network scope, completed configuration digest and native configuration. It
  rechecks current source and the actual receipt after native waits. Recorded
  cleanup remains usable after the declaration is removed.
- Native WireGuard and VXLAN creation reproduced the Linux behavior already
  observed for Bridge: a successful create ACK did not install the requested
  alias. The repair binds cookie installation to the exclusive create ACK and
  requested positive index, validates the native object and reads the cookie
  back. An uncertain creation is not adopted or replayed after restart.
- The independent review reproduced a VXLAN withdrawal race through the real
  static-address caller: one address write occurred after the parent marked
  itself withdrawing. `bca3330e` rereads the same actual receipt after the native
  observation and rejects pending, withdrawing or changed identity/plan.

`make check-routerd15-native-parent-linux` passed on Linux 6.8 with Go 1.25.9.
Every mutation was inside new user/network/mount namespaces with their own sysfs.
The target exercised Bridge address acquisition/reopen/withdrawal and native
VXLAN cookie/configuration/withdrawal. A separately configured kernel WireGuard
peer in another network namespace exchanged real encrypted traffic with the
production WireGuard, address and route controllers. Both ends reported a
handshake and nonzero encrypted transmit/receive counters. SQLite reopen kept
the same ownership identity; child/parent withdrawal removed the actual effects
and receipts, and the directly launched peer process exited and was reaped.

Stopping and restarting the owned WireGuard interface preserved its address but
removed its route; an automatic listen port also changed. Reapplying only the
parent restored its administrative state but did not restore traffic. The
production order of parent, address and route controllers restored the missing
route and traffic with the same parent index/cookie. This narrower result does
not establish packaged service reboot, all peer-source integrations or FreeBSD
WireGuard qualification.

Non-resource checks passed with Go 1.25.9 and bounded build concurrency: the
CLI/control API/WebConsole packages, `check-routerd15-activation`, installer and
live-ISO tests, installer working-directory/dependency checks, version embedding
and safe archive-path checks. The series began at `e8537199` and ended at
`c7f89dda`; per-check source manifests record concurrent changes and relevant
rechecks. It is not a clean-commit whole-repository run. Two existing browser
checks also passed with zero skips using Chromium Headless Shell 147.0.7727.15
and Playwright 1.59.1; their Go/static asset/script manifests were unchanged.
They exercise real handlers, SQLite and browser display/response ordering, not
native controller forwarding. `bd8bc7dc` separately repaired the doctor command
that treated any generation record as a usable rollback: it now verifies a
readable saved configuration within the existing bounded history search and
leaves reapplication validation explicit.

Native FreeBSD 14.3 tests executed the BPF receive implementation with aligned
multiple replies and an unrelated frame before a valid reply; both passed.
The existing lifecycle smoke first stopped despite a real DHCPv4 Bound lease:
its assertions expected superseded event names and incorrectly treated a
stream as its last record. The assertions now inspect the current state-change
events over the complete stream. Actual startup then exposed both supervisor
and dnsmasq process inventories rejecting the legitimate `0 [kernel]` entry.
Native regression tests failed before repair and passed after PID 0 was skipped
as a non-daemon entry. The repaired product recovered its pending supervisor
receipt without deleting the database or ownership file manually.

An initial full lifecycle-script exit 0 was rejected by independent inspection.
Two dnsmasq helper descendants remained alive with UDP port 67 after the recorded
main processes exited and the receipts were removed. Bare devfs exposed only
descriptors 0/1/2 under `/dev/fd`; the remaining helpers retained sockets, BPF
descriptors and both pipe ends. This agrees with the helper/descriptor-closing
paths in the [upstream dnsmasq 2.91 source](https://thekelleys.org.uk/dnsmasq/dnsmasq-2.91.tar.xz).
The old script result is not a native-cleanup pass. The script now independently
requires process disappearance after managed withdrawal. The controller checks
the descriptor view before creating a process receipt, and the FreeBSD platform
instructions identify the `fdescfs` prerequisite. The two known leaked fixture
helpers were separately identified and stopped by the operator; this cleanup
does not count as product recovery.

A fresh complete run with `fdescfs` and the DHCP event-relay helper at its
configured path passed, including the strengthened withdrawal check. Independent
post-run `ps`/`pgrep`, `sockstat` and interface inspection found no dnsmasq/helper
process, fixture listening socket or epair pair left behind. The run exercised
real BPF DHCPv4 acquisition, supervised-client crash/restart/declaration removal,
markerless foreign-process preservation, serve-owned dnsmasq peer acquisition,
configuration replacement/idempotence/withdrawal, foreign-artifact preservation,
Kea DHCPv6 prefix acquisition/reacquisition and resolver health/reload/restart.
The resolver slice still lacks independent DNS UDP/TCP queries. This is not
FreeBSD package upgrade/rollback, CARP failover or complete platform parity.


### Integration, independent DNS and CARP follow-up

At clean integration source `f54ceecb96a2e74d29e69827fb57b84c77286e4a`, the
complete normal Go suite exited 0: 120 packages passed, nine had no tests,
4,120 top-level tests passed and ten skipped (6,186 passing entries including
subtests). Schemas, website schema copies and focused vet passed. The explicit
Linux native parent target passed again at this source. The added FreeBSD
entry-point fixtures passed after integration; descriptor preparation now occurs
before the complete native Go suite, while the DHCP relay is prepared only for
the opted-in lifecycle slice. The entry restores only the mount/files it created.

The independent FreeBSD DNS script is saved in `9651257e` and integrated as
`a0aca109`. Using the actual resolver binary, FreeBSD 14.3 and the independent
`drill` 1.8.3 client, initial, reload and restarted replies matched over both UDP
and TCP. Shutdown removed the listening sockets, IPC socket and owned processes.
The resolver binary is bound by SHA-256 in the qualification ledger; its exact
original source revision was not recorded, so this is not a clean-source binary
qualification. Upstream forwarding, DNSSEC, TLS, IPv6 and DHCP-derived sources
remain separate checks.

VRF work at `9735f25e` adds a concrete Linux owner and SQLite pending receipts
while retaining the baseline plan/artifact declarations. Its real Linux check
passed with a distinct peer namespace, table-101 connected routes, unchanged
parent/member identities across SQLite reopen and Down recovery, and child/parent
withdrawal. An independent review reproduced receipt replacement during the last
withdrawal wait causing a native delete before an error. The owner now checks the
actual receipt immediately before Delete; the regression and native test passed.
Multiple VRFs, member-address dependency semantics and PD/RA within VRF remain
unqualified. The compile-only FreeBSD check does not claim a FreeBSD VRF runtime.

The existing CARP VNET fixture initially left both sides MASTER. Packet capture
at its bridge observed no CARP advertisements. The host-facing epair ports had
not been brought up. The corrected fixture enables both ports and tests VIP
traffic from the root network stack, which has no VIP, rather than a CARP peer's
own stack. With the production binary built from `f54ceecb`, the public corrected
script passed election, priority change, unchanged restart, takeover, reacquisition,
withdrawal and foreign address/VHID preservation. Independent requests reached the
VIP before and after takeover, with three replies and no loss in each phase.
Post-run inspection found no fixture jail/interface/socket; the CARP module was
returned to its previously unloaded state. This is a VNET/kernel lifecycle check,
not a three-machine failure-domain or physical-network qualification.


### Native FreeBSD normal-suite follow-up

The complete normal Go suite was actually executed in the dedicated FreeBSD
14.3 VM from fixed source `c9f5a5cc`, with Go 1.25.9, CGO disabled and a local
module proxy. It failed: 107 packages passed, 12 failed, nine had no tests;
3,425 top-level tests passed, 97 failed and 165 skipped. These results must not
be replaced by the earlier cross-compilation or narrow runtime passes.
The initial environment lacked git/bash/ShellCheck and Git checkout metadata.
Other failures expose Linux-specific test assumptions or real FreeBSD behavior;
each remains classified and repaired at its actual boundary, without a blanket
OS skip or a claim that every failure is environmental.

At `a9a6a5b8`, focused regressions passed on Linux and the actual FreeBSD VM.
The default generated leaf transport is now supported GRE on FreeBSD while
Linux retains FOU. FreeBSD's native `daemon -f -S -P` closes the launching
command's descriptors, preserves syslog output and retains the provider pidfile
lease; the actual duplicate-admission/lease test now passes without WaitDelay.
The DUID fixture uses an actual Ethernet interface rather than a Linux-only
loopback name, and the community wire fixture uses a configured loopback address
without changing host networking. The two FreeBSD-specific launcher tests and
six raw BGP community-selection subcases passed on the real OS.

Four declaration-validation regressions were separately repaired by explicitly
selecting their intended Linux target through the existing ValidateForOS API.
All 252 config tests passed on Linux, and the previously failing four tests also
passed in the FreeBSD test binary. FreeBSD's real unsupported-feature rejection
was retained. The whole normal FreeBSD gate remains open until the remaining
observed failures are resolved and the complete fixed-source run passes.

## Linux PD renumbering and recovery after expiry

Product `b4965652`, formally packaged, passed the extended
`linux-pd-ra-lifecycle-smoke.sh` gate at fixture `bcc27b34` on Ubuntu 24.04.4 /
Linux 6.8.0-124-generic with independent Kea 2.4.1 and Linux SLAAC namespaces.
Kea reloaded a pool change from `2001:db8:1500::/60` to
`2001:db8:1501::/60` while routerd and Kea retained their PIDs. An actual Renew
selected the replacement. The new PIO, client source, local/upstream DNS UDP/TCP,
and routed HTTP passed; the old router address disappeared, the old DNS endpoint
stopped responding, and any retained old client address was deprecated.

After product service restart, stopping Kea exhausted the actual lease. The
router address and client default route disappeared and HTTP failed. Restarting
only Kea then restored PD, usable client addressing, DNS and forwarding without
product-state repair. Final empty apply and fixture cleanup passed; independent
inspection found Healthy/zero resources/no errors, no trial namespaces, ports
or peer processes. The original IPv6 forwarding setting was restored.

The first extended run failed its recovery HTTP check after waiting for Bound
and a default route without checking client DAD. A local isolated-kernel
reproduction demonstrated that a route can exist before its usable source.
The fixture now waits for a non-tentative, non-failed, preferred client address;
the entire gate passed on rerun. Failure snapshots are captured before stopping
participants. Successful raw output is removed; the English/Japanese procedure
and fixture retain the regression criteria. Relevant PD/RA/daemon tests, shell
checks and four website locales passed.

The failed-run cleanup separately exposed stale undeclared Pending/Expired
resource rows affecting overall status after empty apply. That successful run
did not fix it; the subsequent owner cleanup is recorded below. This gate
does not qualify overlapping multi-prefix lifetimes, WAN RA/IA_NA, IPv6 reboot,
FreeBSD forwarding or the full routerd 1.0 feature inventory.

## Removed PD and DNS status lifecycle

Baseline `e3b49dbb` and product `b4965652` retained undeclared Pending/Expired
PD, delegated-address and DNS rows after expiry followed by empty apply. A
formerly Bound PD observation also projected Expired later, making a previously
Healthy empty router Degraded. Global status aggregation was not filtered or
weakened. The responsible owners now retire completed state:

- LAN removes an undeclared display row only after its native receipt is gone.
  Pending, ambiguous or failed native withdrawal retains its receipt.
- The client supervisor retires removed PD state after acknowledged child stops
  and event-reader joins. A disabled declaration remains distinct from removal.
- The DNS controller retires removed zone projections. Resolver process status
  is retired by its process owner after shutdown acknowledgement.
- In production Linux serve, that resolver owner is `SystemdUnitController`:
  `SuperviseDNSResolvers` is false. Unit stop, artifact removal and daemon-reload
  must succeed before retiring resolver status. The durable Removed unit result
  lets a failed DB deletion retry after the unit file is gone. Missing/failed
  acknowledgement and `when:false` declarations retain their status.

The initial candidate `6de787cd` passed local Go tests but the extended VM gate
still found DNSResolver Pending after empty apply: it covered the optional child
supervisor rather than the actual systemd owner. `7f36957c` reproduces that owner
path, including failed stop and failed status deletion. `9524e671` corrects it.

Formal `make dist` product `9524e671` passed the extended
`scripts/linux-pd-ra-lifecycle-smoke.sh` on Ubuntu 24.04.4 / Linux
6.8.0-124-generic with independent Kea 2.4.1 and Linux SLAAC namespaces. The run
covered acquisition, actual renewal, pool renumbering, old endpoint withdrawal,
service restart, expiry, upstream-only return and restored DNS/forwarding. It
then expired the lease again and applied an empty configuration. Routerd kept
the same PID throughout expiry, recovery and final removal, with zero automatic
restarts. Final inspection found Healthy, zero resources, no phase issues or
controller errors, and no remaining PD/LAN/DNS rows in SQLite. Trial namespaces,
ports and peer units were removed and the original forwarding setting restored.

Validation: `go test ./...` passed at the final product source (120 packages,
nine without tests), including stop/deletion failure, disabled declaration and
native-receipt retention regressions. Shell syntax/ShellCheck, schema/example
checks, four website locales and formal static Linux distribution checks passed.
The English/Japanese development procedure includes the expiry-before-removal
criterion. Resolved failed-run output and duplicate/older packages are removed;
the procedure, tests and this scoped verdict are retained.

Relevant source/caller review: `cmd/routerd/cmd_serve.go`,
`pkg/controller/chain/chain.go`, `lan_native_receipt.go`, `pd_observation.go`,
`system.go`, `daemon_removed_status.go`, `system_dns_removed_status.go`,
`pkg/controller/dnsresolver/controller.go`, `removed_zone.go`,
`pkg/state/pdlease.go` and `pkg/state/sqlite.go`, with their focused regression
tests. Native FreeBSD removal, WAN RA/IA_NA, overlapping prefixes and the remaining
1.0 function/platform inventory are not qualified by this Linux result.


### Combined native routing and FreeBSD portability follow-up

At clean integration `3e58c93a`, all nine related Linux Go packages passed:
chain, BGP controller/daemon/library, routerd-bgp, routerd, routerctl, config and
installer. The native-parent gate passed Bridge, WireGuard, VRF, VXLAN and
DS-Lite in isolated namespaces. The separate AFTR captured the actual IPv6
encapsulation and inner IPv4 ICMP traffic. Independent FRR 8.4.4 passed both
address families through the production daemon/RPC/controller/SQLite/kernel
path, including forwarding, withdrawal/readvertisement, peer and daemon restart,
SQLite reopen and declaration removal. Related vet passed. This combined check
does not qualify every policy, ECMP/BFD/GR mode, LAN transit or FreeBSD routing.

The real FreeBSD 14.3 VM also passed the corrected BGP import-policy fixture at
`a2ba1bfa`: configured IPv4/IPv6 loopbacks replace unconfigured 127/8 aliases.
Direct-peer preference and IPv4 FIB selection remain tested. Prefixless import
rejection requires a newly received UPDATE and corresponding RR Adj-RIB-In
entry, followed by the expected direct-only global RIB. The fixture now tests
two peers in stages rather than three simultaneous peers; this is not a native
FreeBSD FIB forwarding qualification.

At `4f9015bc`, the complete observe package passed 24 top-level tests on the VM,
without skips. WebConsole passed 117, skipped two and failed none. The fixes
supply test-owned pfctl as well as conntrack, and OS-appropriate empty/failing
neighbor commands. They retain cancellation, output limits, confidential-data
exclusion and fresh/empty/failed metric distinctions; production behavior was
not loosened. Both packages also passed on Linux.

SAM submit fixture `46865adb` passed all 17 selected cases on FreeBSD, retaining
SQL, authorization and signature checks with an existing supported IPIP
transport. Live-ISO fixture `e234bbc3` passed all 29 package tests on the VM;
the Linux mount-information file is supplied at the existing observation
boundary. No live ISO was booted, and no SAM data-plane parity is inferred.
VRRP fixture `8ab6ed09` also passed its complete package on the VM after two
keepalived-specific tests explicitly selected their existing Linux OS boundary.
Diagnostic fixture `8d2c0485` passed its HTTP check on the VM, distinguishing
unknown link-local scope from an actual private UDP/TCP DNS SERVFAIL and SQL
failure. Production classification was retained. The complete normal FreeBSD
gate remains open, including the remaining chain failures. Exact test-binary hashes and scoped
exclusions are retained in the feature-qualification ledger. Whole-function
and whole-platform verdicts remain unknown.


The complete Linux Go suite subsequently passed at clean `52a52b4c`: 120
packages passed, nine had no tests, 4,148 top-level tests passed, 13 skipped and
none failed (6,237 passing entries including subtests). The opt-in namespace
and FRR gates remain separately bound to the executed `3e58c93a` revision.

The FreeBSD PF data-plane script was strengthened at `4dc78020`. Its previous
outbound-capture check did not require replies. Each independent egress VNET
now owns the routed destination; all four three-packet requests must receive
all three replies. Repeated requests from each source must retain one egress,
and both egress peers must be exercised. Capture completion is observed before
stopping tcpdump, rather than treating its buffered final packet as absent.
With the production binary built from clean `52a52b4c` (SHA-256 in the ledger),
all 12 replies arrived; both peers captured six translated requests, no private
source leaked, and each PF state showed 3:3 packets. Independent post-inspection
found no trial jail/interface, restored forwarding=0, empty rules/states and
PF returned to its previously unloaded state. This qualifies rendered PF
source-affinity/NAT traffic in VNET, not controller-owned reconciliation,
IPv6, physical failover or complete firewall parity.


### BFD interoperability, PPPoE shutdown and remaining native FreeBSD gate

At clean integration `d1cfff8a`, the new
`make check-routerd15-bfd-native-linux` gate passed the independent FRR 8.4.4
BFD lifecycle and the existing dual-stack BGP lifecycle with a BFD failure
stage. Stopping only the remote bfdd first left BGP Established; the observed
BFD Down then drove the production IPv4 hard reset, RIB/FIB withdrawal and
failed IPv4 traffic. Unrelated IPv6 BGP/routes/traffic remained usable. BFD
return restored communication, followed by the original withdrawal, peer and
routerd-bgp restart, SQLite reopen and declaration-removal stages.

The actual FRR JSON omits Peer Type. The adapter now joins actual JSON and
ordinary CLI observations by the complete peer tuple and local timers, and
rejects unequal peer sets or unknown observations. Independent Controller and
SQLite regressions caught foreign adoption after a pre-dispatch abort. Only a
pending effect with a persisted successful command acknowledgement and matching
readback may recover ownership; an unacknowledged pending effect remains fenced
across retry and declaration removal. Receipt compare failures retain authority.
These checks do not qualify single-hop/IPv6 BFD, cross-vendor peers or every BGP
policy/BFD combination.

The initial complete native FreeBSD normal suite was rerun at clean
`0aa10541`, with native Go 1.25.9, prerequisite tools, Git metadata and the local
module proxy. It still failed: 118 packages passed, one failed and nine had no
tests; 3,526 top-level tests passed, 34 failed and 139 skipped. All observed
failures were in chain. Its MSS create-race fixture then panicked, so tests after
that point were not observed by this run. These counts are not a complete list
of remaining failures. After repairing the panic, a cross-built chain test
binary ran to the end and exposed further platform-fixture failures; that
invocation lacked native Go PATH/GOROOT for helper-building tests and is not
substituted for a complete native-Go verdict.

At `d78a676b`, an MSS preview no longer reads a live ownership journal or Linux
boot/network identity. A corrupt private journal reproduced the preview failure
on Linux before the fix, and remains byte-for-byte unchanged after preview.
Live application still takes its existing ownership lock, identity checks and
journal recovery path. Pure journal fixtures use explicit fixture identities;
three tests requiring actual Linux boot/network identity remain explicitly
Linux-only. Actual FreeBSD execution passed 20 MSS tests and skipped those
three. The interface-name fixture now uses a configured gif unit valid on both
OS targets. At `857f7953`, the DS-Lite parent/child fixture uses one consistent
OS, runtime name, scope and finite lifetime representation. Its complete
TestDSLite group passed on the actual FreeBSD VM without removing foreign-parent,
lost-confirmation or withdrawal assertions. Linux chain plus DHCPv4-controller
tests also passed at `8a51b1ee`.

A clean-source resolver built at `0aa10541` now separately passed the drill
UDP/TCP initial/reload/restart/withdrawal gate. This closes the earlier binary's
unknown-source limitation for the new scoped result; it does not rewrite the
older binary-only evidence.

The real FreeBSD PPPoE trial with independent mpd5 5.9_19 established IPCP and
bidirectional traffic, but its one-second IPC timeout ended before the existing
three-second native stop grace and five-second interface barrier. The fixture
now permits the actual bounded stop contract. Requiring the outer daemon's
exit status then reproduced a product bug: normal cancellation returned
context canceled/exit 1. Product fix `ee305683` returns success only after
confirmed native cleanup, while unresolved cleanup retains the native identity
and returns its concrete error. The existing SIGINT/WaitDelay child-lifetime
contract is unchanged. Actual private-process tests passed both confirmed and
unconfirmed cleanup on Linux and FreeBSD; three related Linux packages passed.

Fixture `77a0f7e3` with that exact product binary passed acquire, bidirectional
ICMP, stop/start, repeat traffic, final withdrawal, same-name foreign-interface
preservation, and a third connected session terminated by SIGTERM. The connected
daemon exited 0 in two seconds and its native interface disappeared. The runner
then reaped its children and checked no private process/socket, jail or trial
interface remained before publishing success. Independent post-inspection
confirmed only the original vtnet0/lo0 and no jail. Runtime files now stay in the
fixture's private directory. This test used a prepared disposable VM: ng_ether
was already loaded, because its upstream unload contract requires removal of
all Ethernet interfaces. The runner preserves pre-existing modules and restores
only its newly loaded modules; it does not detach the management interface.
See [FreeBSD ng_ether source](https://github.com/freebsd/freebsd-src/blob/releng/14.3/sys/netgraph/ng_ether.c).
PAP/IPv4 helper lifecycle is qualified here, not CHAP, Linux PPPoE, packaged
routerd reconciliation, LAN transit, arbitrary carrier interoperability or the
complete 1.0 PPPoE function set.

### WAN DHCPv4 first apply and independent lease lifecycle

The formal static Linux package `31d5f4d4`, with fixture `670992a3`, passed
`scripts/linux-wan-dhcpv4-lifecycle-smoke.sh` in an isolated Ubuntu 24.04.4 VM
(kernel 6.8.0-124-generic). Its independent Kea 2.4.1 server, DNS/HTTP upstream
and LAN client used separate namespaces inside that VM. This is a real-kernel,
independent-implementation gate, not a separate-machine or physical WAN trial.

The gate starts from empty configuration and applies the product configuration
through routerctl. A leased /24 address, metric-4242 default route and actual
systemd-resolved per-link DNS/domain appear. The LAN client obtains UDP/TCP DNS
answers from the learned upstream and reaches an off-link HTTP destination;
the independent HTTP peer sees the leased WAN address after NAT. T1 renewal
advances the lease epoch. Kea then stops for the next T1 attempt and returns
before T2; capture must contain the same transaction/MAC's unicast Renew,
broadcast Rebind with ciaddr and matching ACK. The exchange and post-rebind
traffic passed. Stopping Kea again causes real 90-second expiry: address,
default route, learned DNS/domain and forwarding disappear. Returning only
Kea restores all four and LAN DNS/NAT without restarting the product.

Empty apply finishes Healthy with zero declared resources, phase issues or
controller errors. Independent post-inspection found only original interfaces,
no trial namespaces/nft tables/units, restored IPv4 forwarding=0 and unchanged
IPv6 forwarding=1. The service remained active with no automatic restarts.
SQLite contained zero DHCPv4Client and DNSResolver rows. Two empty DNSForwarder/
DNSUpstream configuration rows remain; they carry no lease/process authority,
and this check does not claim that every historical configuration row is removed.

The initial `4ac07b78` run exposed a product failure: the DHCPv4 lease actor read
its socket before the supervisor started the helper, causing first apply to
return runtime-apply failure. Test-first commit `ba81d2cd` reproduced it both in
the controller and actual one-shot caller. `31d5f4d4` permits Pending only for
ENOENT without a prior lease, deadline, applied or pending effect. Existing
worker loss, other I/O errors, expiry withdrawal and ownership-read failures
remain failures. A newly available helper replaces the pending observation.

Two fixture errors were corrected before the final pass: Kea DHCPv4 does not
accept DHCPv6's data-directory setting (same-binary local parser RED/GREEN),
and stopping tcpdump immediately after the helper's ACK can discard the final
kernel-buffered exchange. An isolated local netns reproduced zero saved packets
on immediate stop and one saved packet after observing it. The procedure now
waits for the correlated exchange in the saved capture before stopping capture;
it does not infer wire success from lease status. The lease state names use the
actual case-sensitive Bound/Renewing contract.

At `31d5f4d4`, the full Linux `go test -json ./...` gate passed 120 packages and
4,152 top-level tests, with nine no-test packages, 13 skipped tests and zero
failures (no cached package verdicts). Focused controller/one-shot safety cases,
Python capture tests, shell checks, schema/examples and formal static distribution
checks passed. The relevant source review covered `pkg/dhcpv4client/client.go`,
`snapshot.go`, `cmd/routerd-dhcpv4-client/main.go`,
`pkg/controller/dhcpv4client/controller.go`, `pkg/controller/chain/chain.go`,
`startup_supervisor_order_test.go`, `runtime_apply.go`, `pkg/api/specs.go`,
`pkg/controller/dnsresolver/controller.go` and their focused tests.

Reproduction prerequisites and pass criteria are in the
[English](https://routerd.net/docs/operations/development#linux-wan-dhcpv4-lifecycle-vm-gate)
and [Japanese](https://routerd.net/ja/docs/operations/development) development procedures.
Successful raw captures are deleted automatically. Resolved failed-run captures,
outputs and superseded duplicate packages are removed after the final pass.
The ledger binds the package hash and revisions. FreeBSD WAN DHCP, reboot/NAK,
server identity changes, multi-WAN failover and complete 1.0 parity remain open.

### Observe-only WAN RA host effects

At `7acdc692`, `IPv6RAAddress.managed: false` still generated an
`accept_ra=2` runtime write/persistent sysctl file and a networkd adoption
drop-in/reconfigure. Test-first checkpoint `3469b567` reproduces both effects
through `SysctlController.Reconcile` and `NetworkAdoptionController.Reconcile`
with isolated temporary files and injected commands; it does not alter host
networking. The same missing managed check exists in the 1.0 baseline.

The focused correction gates both derived host settings on the existing
default-true managed field. The reproducer passes for observe-only, omitted/true
management, and an independent PD owner on the same interface. Related sysctl
and network-adoption tests, host dependency tests and schema checks passed.
The broader render check failed in the existing
`TestFreeBSDTailscaleRCDTimeoutsWholeStartAndRollsBack/tailscale-up` marker
cleanup assertion, and `make validate-example` failed because its sandbox
server exited with `context deadline exceeded`. These failures remain under
investigation at the first checkpoint. Capturing the previously discarded rc.d
output at `17b303ab` identified the stop-not-confirmed branch, which correctly
retains ownership. A normal whole-Go run on the persistent test directory failed
in two packages: two ARP tests timed out while opening a new SQLite database,
and the Tailscale rollback fixture could not confirm stop. Concurrent host
observations showed high I/O pressure; this is not a reason to weaken deadlines
or remove retained ownership. The unchanged baseline passed the focused
Tailscale check and sandbox startup, and example validation subsequently passed.

At `17b303ab`, the same test deadlines and production code passed the whole Go
suite with test temporary files on an existing executable tmpfs:
`TMPDIR=/tmp/r15 GOTMPDIR=/tmp go test -p 2 -json ./...` (`/tmp/r15` was a temporary
symlink to that tmpfs). There were 120 passing packages, nine with no tests,
4,161 passing top-level tests, 14 skips and no failures; 46 package verdicts
were cached. The ARP failure reproducer passed in 0.03 seconds there. This is
functional unit/control-plane verification, not persistent-media durability or
proof that the original storage condition is repaired. Four-locale website
build passed with existing unrelated localized-link warnings. Successful raw
outputs and the temporary test area are removed after recording these limits.

The change prevents new observe-only effects; it does not restore settings from
an earlier managed configuration or qualify WAN SLAAC/IA_NA interoperability.


#### Persistent-directory follow-up

With I/O pressure reduced, the same three previously failing top-level tests
and the RA observe-only test passed on the development host's default persistent
`/tmp`, without changing deadlines or product code. At clean `593cb42b`, pinned
CGO-disabled Linux test binaries then passed those four tests as an unprivileged
user in an isolated Ubuntu 24.04.4 VM (kernel 6.8.0-124-generic, ext4 `/tmp`).
The two ARP tests completed in 0.09 and 1.06 seconds, RA host-effect fixtures in
0.04 seconds, and the rc.d timeout/rollback fixture in 2.05 seconds. This checks
actual SQLite files and controller/renderer fixtures; network commands remain
injected and rc.d behavior is a shell fixture on Linux, not native FreeBSD or
WAN RA interoperability. The installed router stayed healthy with empty intent.
This narrower rerun does not replace a complete persistent-directory suite or
prove operation under sustained storage pressure. Test binaries and successful
raw outputs are removed; hashes and the source are recorded in the ledger.

### FOU and GUE candidate native gate

Candidate source/test `5be3b91e` was tested separately from the shared proposal,
using a pinned static Go test binary in an isolated Linux VM. FOU passed actual
controller/SQLite ownership, separately configured iproute2-peer echo traffic,
unchanged reconciliation without another apply, source-status revocation,
parent/child withdrawal, fresh-token reacquisition and final receipt removal.
The source-status transition is injected through the canonical store, not a
real DHCP expiry. Both peers use the same Linux kernel in different network
namespaces; this is not different-device interoperability.

GUE failed during controller acquisition with `numerical result out of range`.
The separately configured peer's `ip fou add port 5556 gue` succeeded. The
candidate's `addTunnelNativeFOU` calls netlink v1.3.1 `FouAdd`, which serializes
`Protocol=0` as an explicit `FOU_ATTR_IPPROTO` attribute. The kernel's
[FOU attribute policy](https://kernel.googlesource.com/pub/scm/linux/kernel/git/stable/linux.git/+/0506158ac7363a70f0deb49f71d26ccb57e55990/net/ipv4/fou_nl.c)
requires a supplied IPPROTO value to be at least one. GUE ADD must omit that
attribute while direct FOU retains protocol 4. The corrected candidate result
is recorded below; the original failure remains bound to its tested revision.

Environment certification must precede the test: load guest `ipip`, `ip_gre`
and `fou` modules, verify an isolated kernel listener/link create-read-delete,
then run the pinned test with guest-root capability and separate network/mount
namespaces. `unshare -Urnm` fails before controller execution because FOU's
`GENL_ADMIN_PERM` needs initial-user-namespace capability. The working wrapper
uses `unshare -nm`, a private mount tree and a fresh sysfs mount; the test checks
that host, router and peer network namespaces differ. The peer acknowledged
exit and was reaped after both the failed setup and actual gate. No trial link,
named namespace or FOU listener remained in the guest root namespace. Default
module-created tunnel devices remained down. This candidate gate is not a claim
that the pending TunnelInterface implementation is integrated or fully qualified.


#### Corrected GUE candidate

At clean candidate `fedafe49fc05f5e6fd61823572e7d7f87e1c8edf`,
`addTunnelNativeFOU` uses context-bounded `ip -4 fou add port <port> gue`
for GUE, leaving direct FOU on its protocol-4 netlink path. The pinned Linux
Go 1.25.9, CGO-disabled test harness passed both FOU and GUE lifecycle subtests
on the same certified Ubuntu 24.04.4 / Linux 6.8.0-124-generic VM (2.75 seconds).
Each independent kernel peer received the two encapsulated test packets.
Acquisition, unchanged reconciliation, injected source revocation, child/parent
withdrawal, fresh-token reacquisition with traffic, final receipt removal and
peer exit/reap all passed. The local `TestTunnelInterface*` and
`TestTunnelNativeReview*` controller fixtures also passed before the native run.
The owner's separately supplied test binary at the same revision also passed
both native subtests (2.70 seconds); both binary hashes are in the ledger.

Reproduce from the pinned candidate source after environment certification:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o chain.test ./pkg/controller/chain
# Copy this test harness to the dedicated VM and verify its SHA-256.
# Run as guest root; keep the guest host network namespace untouched.
export ROUTERD_RUN_NETNS=1
export ROUTERD_HOST_NETNS=$(readlink /proc/self/ns/net)
unshare -nm sh -c 'mount --make-rprivate / && mount -t sysfs sysfs /sys && exec "$@"' sh \
  /absolute/path/chain.test \
  -test.run '^TestTunnelNativeLinuxPeerLifecycle$/(fou|gue)$' \
  -test.count=1 -test.v -test.timeout=150s
```

This is a controller test harness, not an installed product upgrade. The
installed `31d5f4d4` router remained Healthy with no controller errors; no trial
namespace, listener or tunnel remained outside the isolated test. The source
revocation is still injected and both peers share a Linux kernel. Full daemon
integration, actual DHCP expiry for this tunnel, other platforms and all
TunnelInterface functionality remain separate gates. Successful binaries and
raw logs can be removed after recording the source, hash and verdict.
