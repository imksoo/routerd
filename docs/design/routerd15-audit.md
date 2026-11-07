# routerd 1.5 repository-wide rewrite audit

## Linux name-backed local-service redirection, 2026-11-04

Product source `6dc12c92` (documentation HEAD `84672341`) passed the extended
`tests/netns/local-service-redirect.sh` on Linux 6.8.0-136-generic. Independent
dnsmasq and dig peers exercised both address families and TCP/UDP. The production
IPAddressSet controller resolved A/AAAA through a private resolver mount, stored
status in SQLite and refreshed real nftables sets after the configured interval.
Changing the independent DNS answers moved interception from the old addresses
to the new addresses without a NAT-table rebuild or status repair. Static
destination/port/interface selection, interface changes and empty withdrawal
also passed. Successful temporary files and namespaces were removed automatically.

This fills the earlier name-refresh gap in the local-service redirect check;
it does not qualify DNS outage/expiry policy, daemon scheduling, NTP interception
or the complete deployed-router combination. The reusable command and criteria
are in `tests/netns/README.md`. Test files and SQLite used executable tmpfs to
avoid observed host disk stalls; this is not persistent-media durability evidence.

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
WAN DHCP/PPPoE/DS-Lite, firewall isolation, reboot/crash recovery,
FreeBSD feature parity or the remaining 1.0 function inventory. Those remain
required work; cross-version 1.0 cutback is excluded from acceptance.
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
At that checkpoint WAN RA/IA_NA, prefix renumbering, DHCPv4 renewal, firewall
isolation, reboot/crash, FreeBSD client parity and the remaining 1.0 inventory
stayed open.

The lifecycle gate was rerun at product revision
`1392082fd634ebd26ca0a06b0edc6315411b7a60`, using the installed static package
`v20261101.3` on Ubuntu 24.04.4 / Linux 6.8.0-117-generic. Independent Kea
renewal and pool renumbering, LAN SLAAC/default route, IPv6 DNS UDP/TCP, forwarded
HTTP, old-prefix withdrawal, systemd restart, real expiry, Kea-only recovery and
second-expiry empty apply all passed. Final status was Healthy with zero resources
or controller errors; temporary peers/namespaces/output were removed and the
original forwarding value was restored. Exact binary, procedure and fixture
hashes are recorded as `linux-pd-ra-lifecycle-1392082f` in the qualification
ledger. WAN RA discovery, IA_NA, overlapping multi-prefix behavior, FreeBSD and
the remaining 1.0 inventory remain open.

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
DHCPv4 client or rebind/expiry. VM reboot, interrupted-operation recovery,
IPv6 renumbering/WAN RA/IA_NA and all other unqualified 1.0 functions remain
required. Existing tables without the native marker require explicit
operator resolution; automatic old-format adoption is not provided.

### Current-source clean-install rerun, 2026-11-02

Product `1f9fc3190455d61988865aa8fef0233dcb338af2` was built with `make dist`
and installed in a dedicated Ubuntu 24.04 VM (Linux 6.8.0-124-generic) from a
prepared, validated routerd 1.5 configuration and fresh state. The first
invocation exposed a test setup defect: it started systemd before creating the
canonical config, so the expected clean-install startup failed. The smoke
procedure now installs and validates the prepared config before service start,
and persists the empty 1.5 config during cleanup. The corrected procedure
`scripts/linux-dhcp-firewall-lifecycle-smoke.sh` passed: DHCP acquisition and
timer renewal, local/upstream DNS over UDP/TCP, peer-observed NAT, verified
baseline reachability, three-zone firewall isolation and counters, management
positive control, exception add/remove, service restart, firewall withdrawal,
and empty-config withdrawal. No raw logs were retained; the package and test
VM were returned to a clean state. This run does not qualify host reboot, WAN
DHCP/PPPoE/DS-Lite, IPv6 behavior, or complete routerd 1.0 parity.

## Linux integrated home-router clean-install gate, 2026-11-02

Product revision `999dfd4a188892cb5bfaf4c3eae599d95aa755e3` was built with
`make dist` as `1.5-dev-999dfd4`; archive SHA-256
`9cef7ed88a4223a3648718aa767efc07404d09d4d07cff9d62437c39bcaffa6e`.
The prepared fixture `tests/fixtures/linux-home-router-lifecycle.json` has
SHA-256 `d5531ac800fb7ee5fc3983fd58bfe22ce53165f2e9ae285ed365f997622894bc`.
It was installed through the actual systemd service in a dedicated Ubuntu
24.04.4 PVE guest (Linux 6.8.0-124-generic). The guest started without routerd
configuration or state; the 1.5 fixture was validated before install. No 1.0
configuration or database was read or imported.

The [clean-install procedure](../operations/development.md#linux-home-router-clean-install-lifecycle-gate)
passed WAN DHCPv4 address/metric-qualified route/DNS acquisition and renewal,
LAN DHCPv4 acquisition and renewal, DHCPv6 IA_NA, DHCPv6-PD renewal and
renumbering, real expiry and automatic reacquisition, RA/SLAAC, local and
upstream DNS A/AAAA over UDP/TCP from IPv4 and IPv6 clients, peer-observed
IPv4 NAT and delegated IPv6 sources, unsolicited WAN-to-LAN denial, systemd
restart, IPv4/IPv6 forwarding stop and recovery across upstream loss, and
empty-config withdrawal. Package uninstall completed. Postflight found no
routerd service/configuration/state, test namespaces/interfaces, or nftables
tables; the management route and systemd-resolved remained active. Raw output
and the temporary package staging were removed.

This is an independent-peer check using isolated client/upstream namespaces
inside the guest. It does not qualify physical ISP behavior, WAN IPv6 RA
discovery, PPPoE/DS-Lite, overlapping multi-prefix forwarding, host reboot or
power-loss recovery, FreeBSD runtime, or the remaining routerd 1.0 feature
inventory. Those whole-feature and platform verdicts remain unknown.

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
code was needed for these successful recovery paths. IPv6 reboot/renumbering,
WAN protocol modes and remaining 1.0 parity stay open. Cross-version 1.0 cutback
is outside the acceptance scope.

This pass remains bound to product revision `13b7a5b9`. Review of the source
changes through `1f9fc319` found only PD-expiry and DHCPv4-relay changes after
that pass; neither path is present in this gate's fixture. I have not rerun the
same recovery campaign and do not claim a new revision's reboot pass.

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

### Current-source WAN DHCPv4 rerun, 2026-11-02

The same complete gate passed again with the formal package built from
`1f9fc3190455d61988865aa8fef0233dcb338af2` (archive SHA-256
`6b1d36140e2fcc6e6b3a7bd54509f701f361b86c796ac7d5283ee5bf405f8184`), installed
cleanly with a prepared empty 1.5 config and fresh state on Ubuntu 24.04.4 /
Linux 6.8.0-124. It passed DHCP acquisition, actual T1 Renew and captured T2
Rebind/ACK, metric-qualified route and learned DNS, LAN DNS/NAT traffic, real
lease-expiry withdrawal, Kea-only recovery without product restart, and empty
apply. Kea 2.4.1 was unpacked into a temporary runtime root from Ubuntu packages;
the VM received no Kea package installation. The test and routerd were removed
afterward and raw output was not retained. This remains IPv4-only coverage; it
does not combine WAN DHCPv4 with IPv6 PD/RA or qualify an ISP link.

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


### Linux WAN RA/SLAAC product lifecycle

The formal `make dist` package at `a00a4826d5028319e9fafc5dbd523ea2f5bf690f`
passed an installed-daemon WAN RA gate on an isolated Ubuntu 24.04.4 VM,
Linux 6.8.0-124-generic, with independent dnsmasq 2.91 RA-only and HTTP peers.
The fixture created a networkd-managed WAN with RA initially disabled. Fresh
`managed: false` intent left acceptance at zero and created no adoption drop-in.
Changing it to true enabled acceptance, kernel SLAAC and a `proto ra` default;
the off-link HTTP peer observed the learned SLAAC source. No WAN global address
or default route was manually assigned.

SIGSTOP froze the independent RA sender without a zero-lifetime shutdown RA.
The 15-second router lifetime expired: route lookup and HTTP failed. The actual
120-second prefix lifetime then removed the SLAAC address. Resuming only the
peer restored address, default route and HTTP with the same routerd process
and no controller errors. Empty apply and explicit fixture cleanup followed.
That cleanup is not a managed-to-observe-only rollback qualification.

The first attempt incorrectly required the expired default-route row to vanish
from `ip route show` within 25 seconds. It remained with `expires -12sec`.
An unprivileged local veth/nexthop reproduction showed the same negative-lifetime
row while `ip -6 route get` returned `Network is unreachable`.
[Linux v6.8 route selection](https://github.com/torvalds/linux/blob/v6.8/net/ipv6/route.c)
explicitly skips expired entries. Correcting the test to require unusable routing
and failed HTTP, while still requiring natural SLAAC address removal, passed on
the unchanged product. No product behavior or lifetime was relaxed.

The executed fixture SHA-256 is recorded separately from the product archive.
Its public export adds a license and argument-usage message; the gate body is
unchanged. See [the procedure and acceptance criteria](https://routerd.net/docs/operations/development#linux-wan-raslaac-lifecycle-gate)
and `scripts/linux-wan-ra-lifecycle-smoke.sh`. This does not qualify RDNSS/DNSSL,
IA_NA, LAN forwarding, restart/crash recovery, other platforms or all 1.0 features.

The preceding environment-certification helper had a cleanup defect: killing a
background shell-function wrapper did not kill its dnsmasq/Python children.
The three leftover peer processes were identified by exact command line and
namespace and terminated. A local shell reproduction confirmed the cause.
The helper now launches `nsenter` directly as the background process, so kill
and wait address the actual daemon. The product gate already uses direct
`ip netns exec` processes. Named-namespace absence alone is not a cleanup proof;
verify that the identified peer processes have exited as well.


### WAN RA planned restart and SIGKILL recovery

The installed `a00a4826` make-dist package passed the expanded WAN RA fixture
at `2b8ad0683d48393f83e0d103a0ea18867b6b07af` on the same isolated Ubuntu
24.04.4 / Linux 6.8.0-124-generic VM. After actual SLAAC/default-route acquisition,
a planned service restart produced a new Healthy daemon with both declarations,
unchanged canonical configuration and the original RA/HTTP peer processes.
The off-link HTTP probe passed with its expected SLAAC source.

A separate SIGKILL of the main process then recovered through the installed
systemd `Restart=always` policy. Exactly one automatic restart occurred; no
manual start or configuration reapply was used. The new daemon again reported
Healthy with two declared resources and no controller errors, and the HTTP
probe passed. After those recovery checks, the same daemon passed the original
real RA/SLAAC lifetime expiry and peer-only recovery gate, followed by empty
apply and fixture cleanup.

This checks service recovery while kernel networking remains present. It does
not prove host reboot, kernel-state loss, storage failure recovery or continuity
of already established application sessions. The source package is unchanged;
the new commit extends the test and its documented acceptance criteria. The
ledger separately binds package revision/hash and executed fixture revision/hash.


### WAN RA/SLAAC lifecycle recheck at 1392082f, 2026-11-01

The installed static `make dist` package `v20261101.3`, built from product
revision `1392082fd634ebd26ca0a06b0edc6315411b7a60`, passed the same gate on
the isolated Ubuntu 24.04.4 / Linux 6.8.0-117-generic VM. The product binary
SHA-256 was `7138296c801131d97c9b3b0b5c1880d35c37dc44a6d781b1c23ca454f9897180`;
the executed fixture SHA-256 was
`6e53c0dc1cea2d9928a76d678783f16c702cfc4cef8863d58c44a3693cd6d12b`.

Fresh observe-only intent left RA acceptance disabled, with no adopted
networkd drop-in or default route. Managed intent enabled kernel SLAAC and a
`proto ra` default route; an independent off-link HTTP peer observed the
learned SLAAC source. A planned systemd restart preserved intent and traffic.
SIGKILL recovered through exactly one automatic systemd restart, again with
traffic. Freezing the RA sender led to natural route and address lifetime
expiry and failed traffic; resuming only the peer restored traffic with the
same routerd PID and no controller errors. Empty apply and fixture cleanup
passed. No 1.0 configuration, database or installed system was imported or
upgraded in this test.

This is a scoped WAN `IPv6RAAddress` and Linux service-recovery result, not
complete IPv6 or routerd 1.0 qualification. RDNSS/DNSSL, DHCPv6 IA_NA, LAN
forwarding, host reboot/storage loss, other platforms, and automatic restoration
of host settings on intent removal remain outside this check. The procedure is
`scripts/linux-wan-ra-lifecycle-smoke.sh --disposable-vm`.


### Integrated native Go and expanded peer checks, 2026-10-31

The clean combined source `f294bb23e42f6d3a552d340779a3af1dca88f147`
passed the complete ordinary Go suite on FreeBSD 14.3 amd64 with the native
Go 1.25.9 toolchain, CGO disabled, two package/build workers and the pinned
local module proxy. All 119 test-bearing packages passed: 3,949 top-level
passes, 206 explicit skips and no failures (199.412 seconds including runner
checks). All test events, including subtests, were 5,928 passes and 250 skips; nine packages had no
tests. Source Git status was clean before and after execution. The first
transfer retained the Linux file owner and Git refused to run before any test;
correcting the private checkout's ownership resolved that setup failure without
adding a global safe-directory exception.

This combines the actual-OS LAN, mobility, RA, prepared resolver callback,
WireGuard and VXLAN fixture repairs. The previous complete `7acdc692` native
run had 43 top-level failures in `pkg/controller/chain`; it reached the end
without the earlier MSS preview panic. The fixed-source result replaces those
failures for this ordinary-test scope. It does not qualify every corresponding
kernel operation or independent peer.

The earlier clean Linux `7acdc692` whole suite passed 120 packages with 4,160
top-level passes and 14 skips. The combined Linux `f294bb23` run completed with
119 passing packages and one failing BGP package: 4,160 top-level passes,
one failure and 14 skips. The raw BGP peer connected while its asynchronously
created passive FSM was still Idle, which closes incoming connections. An
unchanged-source 25-run repetition reproduced one failure. Test-only
`64f4a47923e539ec79bb31258482481d4e7d89b4` waits for the actual Active state,
within the existing deadline, before dialing the static peer. Dynamic peers,
UPDATE/withdrawal and local-RIB assertions remain unchanged. All six modes
passed 50 repetitions on Linux and 50 on actual FreeBSD; the complete Linux
BGP package also passed. This focused repair is not a whole-suite pass at a
newer integrated revision. The FreeBSD test binary SHA-256 was
`26a28065423cd891f021d59d6a451e02c25254679b5facc20952f796606d99f0`.

#### Single-hop and multihop BFD

Clean `6aab35b502542637a570275f08570c9384a149ff` passed
`sh scripts/linux-bfd-frr-smoke.sh` with both existing BFD configuration forms:
a declared interface for single-hop and a local address for multihop. Each
uses independent FRR processes and isolated network namespaces. Up, peer loss,
Down, peer return, administrative shutdown, local restart, SQLite reopen,
foreign-peer preservation and withdrawal passed in both forms. Single-hop FRR
requires its fixture's real Zebra interface inventory; the first fixture lacked
that inventory and reported a nonexistent interface. Supplying isolated Zebra
processes resolved that setup problem, and both exited normally and were reaped.
The same gate also passed actual BFD-driven IPv4 BGP reset, RIB/FIB withdrawal
and traffic failure while IPv6 remained usable, recovery, and the full existing
BGP lifecycle. This is same-kernel interoperability, not a physical failure-domain
or every BFD option qualification.

The pinned artifacts were BFD test
`5725c2d1624517f3f83a4a5c63660a15d3633647ffc047117dfaa8f817555310`,
BGP test `628dbc280a68447f17c068af95fab5834337768cc651833af4d40c463850563b`,
and production routerd-bgp
`d599a378667e4c56ef9e327378f447d58b248506f4101b8333e31100c227f60e`.

#### FreeBSD PPPoE authentication matrix

The actual `ee305683342033706ed3b9d234acd13b65ddd748` client binary
(SHA-256 `22dca768e4debc5a1e3b1d0bb33fae39a214cf0662087c19cab96649b10cd3ba`)
passed the `2b0f019aa6e78fe6ccb2c8e41bd8ca2e927b03ed` runtime script in all
four supported client/independent-mpd5 combinations: pap/pap, chap/chap,
both/pap and both/chap. Each acquired an IPCP address, passed 3/3 replies in
both directions before and after stop/start, preserved a foreign same-name
interface, and terminated a third connected session with exit zero and native
interface removal. Connected termination took two or three seconds. Each final
result was written only after owned process/interface cleanup passed.
Authentication evidence captures only the 26-byte Ethernet/PPPoE/PPP/FSM header
prefix; the protocol assertions distinguish PAP and CHAP without authentication
payloads. Prepared ng_ether and its baseline module set remain a disposable-VM
prerequisite; these results do not claim that preparation restored the original
VM module state or that the full Router resource/installed service is qualified.

#### Linux PPPoE clean install, independent client and recovery, 2026-11-02

Official source `851acc7d8a9d88f4c9d409fbbaa402a20a7474ae` was built with
`make dist`; the executed Linux archive SHA-256 was
`1beef2a215d8ceb5edc28dd70c7994d4e4c21e5da4945701d11e44052d33d36b`.
It was installed from a clean 1.5 configuration and state on a dedicated PVE
candidate VM. No routerd 1.0 configuration or state was read or imported.

An independent PPPoE access-concentrator VM and an independent LAN-client VM
were attached through separate PVE VLANs. The candidate established a PAP
session as `ppp-rd15`, received `203.0.113.2` with peer `203.0.113.1`, and
installed the PPP default route. The client reached the upstream peer through
the candidate's IPv4 forwarding and NAT44 path: 3/3 ICMP replies succeeded.
The rendered `pppd` configuration preserved the selected interface name,
service and access-concentrator names, PAP selection, 1492 MTU/MRU, default
route, persistence, and the configured LCP echo values.

The access-concentrator VM was then powered off without restarting the
candidate. Client traffic failed, and after the configured five unanswered LCP
echo requests `pppd` reported the disconnect; `ppp-rd15` became down and the
PPPoE resource published a failed session event. After starting only the
access-concentrator VM and its PPPoE server, the still-running candidate
recreated `ppp-rd15`, published a new connected event, restored its default
route, and the independent client again received 3/3 replies. The observed
interval follows the explicit LCP configuration and is not an acceptance-time
target.

This qualifies the observed Linux PPPoE PAP/IPv4/NAT44 transit and
upstream-withdrawal/recovery path only. It does not qualify CHAP or dual
authentication, IPv6CP, peer DNS, multi-session or provider interoperability,
host reboot/power-loss recovery, FreeBSD behavior, or complete routerd 1.0
PPPoE parity.

Follow-up source `f8ef5e5970168802d75933564abf3459cee33ab0` was built as
archive SHA-256 `f1773b5e6b1153b343ee5cc0ba1e7be8f95736ac42138a7e459693bada6bed6f`.
The disposable PVE candidate snapshot contained an earlier routerd binary, so
the package operation was an update rather than a clean-install result; its
routerd configuration and state were reset before the test. The rendered
`pppd` configuration contained `-ipv6`, `ppp-rd15` had no IPv6 link-local
address, and the independent client again reached the upstream peer with 3/3
ICMP replies. This recheck qualifies the explicit Linux IPv6CP-disable setting
for this package and configuration only; it does not replace the earlier
clean-install or upstream-recovery evidence.

#### Linux DS-Lite independent AFTR, client and configuration withdrawal, 2026-11-03

The same product source `f8ef5e5970168802d75933564abf3459cee33ab0` and
archive SHA-256 `f1773b5e6b1153b343ee5cc0ba1e7be8f95736ac42138a7e459693bada6bed6f`
were installed in an isolated PVE candidate. The restored candidate snapshot
contained an earlier routerd binary, so this was an update/configuration-state
reset test rather than clean-install evidence.

The candidate's external WAN IPv6 address and the AFTR's external IPv6 address
were distinct addresses on the isolated WAN VLAN. `routerd` created the
candidate `ip6tnl` device `ds-rd15`, assigned its inner IPv4 endpoint,
installed the IPv4 default route, and reported the `DSLiteTunnel` as `Up`.
An independent AFTR VM used only its own Linux `ip6tnl` device and exposed an
IPv4 endpoint behind it. A separate LAN-client VM reached that endpoint through
candidate IPv4 forwarding and NAT44 with 3/3 ICMP replies; the AFTR tunnel
receive counter increased from 47 to 63 packets.

Replacing the candidate configuration with an empty Router configuration and
restarting the service removed `ds-rd15` and the `routerd_nat` table; client
traffic then failed. Restoring the same DS-Lite configuration and restarting
the service recreated the tunnel, restored the default route and NAT44, and
returned 3/3 replies from the independent client. This qualifies the observed
Linux DS-Lite static-AFTR IPv4 transit and configuration withdrawal/recovery
path only. Source-address expiry, other NAT/MSS/firewall bindings, host reboot,
provider interoperability, FreeBSD behavior and complete routerd 1.0 parity
remain unqualified.

#### Linux VRRP virtual address independent client failover and recovery, 2026-11-03

Product source `81bc17680e3200ccda65eaa1f5a6b4190f908668` was packaged as
`routerd v20261027.1620 (81bc176)` with archive SHA-256
`90e10e039d8a4f694a48ef65f0709c03cc7b13f8428d83eb53faf3bf75b9da25`.
Two isolated PVE candidates used separate node addresses on LAN VLAN 3131 and
WAN VLAN 3132. A third isolated VM contained separate client and upstream Linux
network namespaces: the client used only `198.18.40.254` as its default gateway,
while the upstream endpoint used `203.0.113.1`.

The higher-priority candidate reported `VirtualAddress/lan-vip` as `master` and
was the sole owner of `198.18.40.254`; the other candidate reported `backup`.
The independent client reached the upstream endpoint through the virtual address,
IPv4 forwarding and routerd NAT44 with 3/3 ICMP replies. Direct ICMP to each
candidate from the WAN namespace was denied by the configured untrust-zone
policy, while the routed client flow succeeded.

Stopping the initial master promoted the remaining candidate to `master`, moved
the virtual address to it, and preserved the client's unchanged default route;
the client again received 3/3 upstream replies. After the stopped candidate
booted, it reported `backup` with `preempt: false`; the surviving candidate
remained master and the client again received 3/3 replies. This is an
update/configuration-state-reset check because both candidate snapshots contained
an earlier routerd binary. It qualifies the observed Linux IPv4 VRRP election,
client forwarding, primary withdrawal and node recovery path only. Split-brain
and fencing behavior, PVE-host failure, configuration withdrawal, IPv6 virtual
addresses, physical-site failure domains, FreeBSD behavior and complete routerd
1.0 parity remain unqualified.

#### Plugin process and effective-configuration checks

At `e4097d0b966aeabbbe8fb01eb6d43ab42d5adc3c`, the actual subscription
controller, default runner, independent shell executable and SQLite passed
stop/child exit, exit-status-7 failure, retry and durable no-replay checks on
Linux and FreeBSD. Separately built production routerctl processes passed
stdin identity, generation publication, effective-route replacement, allowed
masking, policy-removal rejection, preservation of the prior generation and
expiry after failure, real-time TTL expiry and fallback restoration, and later
generation advancement. The FreeBSD CLI SHA-256 was
`67466729a122032e828961a6d20708adb795c7c2cfa5e66158826bb921538511`;
its two native checks passed in 0.06 and 10.13 seconds. Reproduce the Linux
process gate with `make check-routerd15-plugin-process`; the direct script's
required tools and limitations are documented in
[the process procedure](../operations/plugin-dynamic-process-test.md).
These prove local process/persistence/effective-configuration behavior, not
native route installation or forwarding. DynamicConfigSource and declared
Plugin triggers still have no automatic execution caller in either reviewed
baseline or current source and remain unfinished advertised entry points.

#### Newly observed native boundaries and remaining work

FreeBSD 14.3 `route -n get -host` can print
`route: route has not been found: No error: 0` and exit zero. Native child
acquisition exposed this before the parent-preservation stage. Production
`2237d1ec6e5f1b3a058192d0e636058c9f1d6b14` recognizes the existing narrow
absence messages independently of exit status. Unknown empty output, foreign
routes, BGP protocol markers and replacement-interface protection remain
unchanged; all related route tests passed after the actual-output regression
was first observed failing. The same native trial showed that a FreeBSD P2P
alias needs an explicit destination. Retaining another owner's child addresses
and routing through the actual parent remains an active separate acceptance
gate, not a pass inferred from the ordinary suite.

All full-feature/platform verdicts remain unknown. The complete 1.0 scope,
unimplemented advertised entry points, native forwarding and failure recovery,
remaining platform/service combinations, and release qualification remain in
the completion criteria. No production deployment or main merge is included.


## DHCPv6 IA_NA and combined PD client lifecycle

Product `ede8dbf269148435c9ddc2a3c95bd187e808d863` extends the existing
DHCPv6 exchange owner with typed IA_NA addresses and independently timed IA_NA/
IA_PD leases. Test-first checks cover omitted-association replies, partial offers,
NoBinding, independent withdrawals/expiry, identity rejection and snapshot
restoration. A daemon test reproduced secondary-address expiry remaining on disk;
comparison against the last successfully saved snapshot now persists and publishes
that expiry even when the overall lease/exchange state is unchanged.

The formal static `make dist` helper passed both `address` and `pd-address` modes
against independent Kea 2.4.1 in an isolated Ubuntu 24.04.4 VM with Linux
6.8.0-124-generic. Allocations matched Kea's lease database. Helper restart retained
acquisition timestamps; natural T1 Renew succeeded. SIGSTOP of only the peer
produced Renew, T2 Rebind and actual 45-second expiry in public and saved state.
Peer-only resumption restored acquisition; Release and clean termination passed.
The procedure at `8d3064b5`, archive and executed helper/script hashes are bound in
`linux-dhcpv6-associations-ede8dbf2` in the feature ledger. Reproduce using the
[DHCPv6 client procedure](https://routerd.net/docs/operations/development#linux-dhcpv6-address-and-prefix-client-gate).
Fixture processes/links and successful temporary output were removed. The installed
router configuration was not replaced and its status remained Healthy.

At the product revision, the whole ordinary Linux Go run passed 120 packages and
4,176 top-level tests, with 15 explicit test skips and 67 cached package results.
Targeted protocol/helper/daemon API race tests passed. FreeBSD-targeted protocol
and helper compilation passed; this was not FreeBSD runtime execution. Schema,
example, shell syntax/ShellCheck and four-locale website builds passed. Existing
unrelated translated-link warnings remain.

This is helper protocol qualification. The configured `DHCPv6Address` supervisor
entry point, shared-interface validation, kernel /128 address ownership, DAD/
Decline, forwarding and native FreeBSD gates remain open. In the pinned 1.0
baseline, the reviewed DHCPv6Address entry point represented renderer intent;
that does not prove native runtime support. Neither that limitation nor the
successful helper gate closes full 1.0 preservation requirements.

A follow-up test reproduced repeated disk writes and fabricated binding events
while an internal NoBinding Request retained a publicly Bound sibling.
`87761c0ef4da26620e087f81bb47851908c6ac95` compares against the saved public
state without replacing it with the internal exchange state. The new regression
and targeted race checks pass. The subsequent full ordinary Linux run passed
120 packages and 4,177 top-level tests, with 15 test skips and 118 cached package
results. Targeted FreeBSD cross-compilation also passes. The independent Kea
result above remains pinned to `ede8dbf2`; no newer native result is implied.

Reviewed implementation/consumer paths for this increment include
`pkg/pdclient/{message,address_message,client,address_client,snapshot}.go`,
`cmd/routerd-dhcpv6-client/{main,address}.go`, `pkg/daemonapi/types.go`,
`pkg/controller/chain/{chain,pd_observation,lan_native_receipt,static_address}.go`,
`pkg/config/{validate,validate_wan,listen_ports,index}.go`, `pkg/api/specs.go`,
`pkg/render/netplan.go`, their related protocol/daemon/configuration tests, and
the development runbook. The native controller paths were inspected for the next
integration step, not changed or qualified by this increment.

## Automatic Plugin triggers and shared NTP owner, 2026-10-31

Integration `309af77fe23dc049ff2485c86204caed2ff64a0e` contains the automatic
Plugin owner (`035d13c8`), NTP lifecycle/shared-provider fixes (`59104e2a`),
FreeBSD ntpd fixture (`1c389ab8`), static-address ownership rechecks (`d2efd8f9`)
and the independently reproduced logging archive/startup fixes (`49bd1e7e`).
This is an implementation checkpoint; whole-suite results and per-feature
qualification remain separate observations.

### Automatic local Plugin publication

The existing framework worker now executes prepared Plugin/DynamicConfigSource
trigger unions. Each interval has its own due time. Ordinary events select only
matching named topics and cannot consume interval deadlines. An optional explicit
bootstrap callback separates internal startup from valid operator-declared events
named `routerd.controller.bootstrap` or `routerd.controller.periodic`; unchanged
controllers retain their prior bootstrap behavior.

The production runner publishes through the existing source/generation SQL CAS.
A source TTL caps the original observation lifetime, including manual CLI
publication. Expiry while waiting for SQL cannot commit an already expired new
generation. Cancellation reaps the actual child before completion and cannot
publish late output. Read-only and standby paths neither launch nor consume due
work. Unchanged active content does not generate another change notification.

At `035d13c8`, the Linux smoke entry point passed 41 top-level tests: subscription
1, separate CLI 1, owner 14, framework 21 and actual declared callers 4, with no
failure or skip. Independent review reproduced the event/deadline and reserved
name failures before the fix, then passed the same three cases afterward.

The exact-source FreeBSD 14.3 binaries also ran: owner 14 and actual framework
callers 4 top-level passes (23 passes including subtests), no failure or skip.
The owner binary SHA256 is
`61ee69d5b06dae53d64e5da5172fe2a5150beb102a47b0462045dc1140e93722`;
the caller binary SHA256 is
`02b8c02a5377abd814dc7fc8a21144186161c7d481c31a7711a0a78eba23c216`.
Actual interval repetition, bus publication, SQL generation and cancellation
were exercised. This does not qualify native forwarding from a plugin's output
or every external provider. See [the reusable process gate](../operations/plugin-dynamic-process-test.md).

### NTP fragments, shared provider and real packets

The NTP owner joins acquired fragments to private receipts, exact marker/digest,
provider and path. Interrupted writes/reloads/withdrawals retain retry state;
unacquired or foreign replacements survive. A declared managed server owns the
shared provider, including a Linux timesyncd-requesting client delegated to
chrony. A false server condition keeps active client upstreams while disabling
its downstream service. Public Applied state requires the confirmed fragment.

The final Linux NTP candidate `59104e2a` passed 28 NTP top-level tests (one
separate native opt-in skip), and its whole chain passed 869 (five existing
native opt-in skips). The real isolated chronyd/independent-UDP gate passed three
configurations, including the mixed-client/server case, source selection,
allowed/denied queries, process/SQLite recovery and declaration withdrawal.
Native execution/build inputs were compared with all 13 NTP-related git blobs
at `59104e2a`; the record retains that execution began at `f74b3ab3` plus the
subsequently committed native test. It is not relabeled as a clean-commit build.
Chronyd used `-x` in private user/network/mount namespaces.

A separate FreeBSD 14.3 pair used the identical final fixture and script on both
sides. Baseline `112a1e1b` selected its independent reference and returned a valid
mode-4 reply to the explicitly denied source; the expected silence assertion
failed. Candidate `1c389ab8` passed in 91.46 seconds, including allowed replies,
three silent denied queries per stage, owned process restart/Wait, SQLite reopen,
WhenFalse silence with upstream selection retained, and WhenTrue recovery. The
candidate production source here includes `b8cdb17e`; later shared-provider/token
changes in `59104e2a` require their own exact-source native replay.

Candidate binary SHA256:
`48815b5518eb3395c3cc070fa3b96d0e528b89a67aadf89ba1ed3ed1c478ff1c`.
Both runs used script SHA256
`8b1566a7c7996937ff73b1c42111ab8ab00b8cea709961136c5691ca8953bbe5`
and the same fixture source hash. Earlier attempts failed during fixture setup
(ntpq path, numeric peer matching, disabled-feedback leap alarm, then cold
reference dispersion); they are retained as failed setup attempts. The final
fixture waits for measured reference quality without changing product maxdist,
ACLs or upstream inputs.

Both VNET jails first proved clock-setting and NTP-adjustment privilege denial.
The generated provider fragment was consumed unchanged, with private drift files
and disabled kernel discipline. Independent source review checked FreeBSD 14.3
jail rejection of the fallback clock APIs. No shared clock or rc.d/sysrc setting
was changed. Each run confirmed process exit, jail removal, epair absence and
module restoration before its final result. Independent post-run inspection
found no trial jail, ntpd process or trial interface. Actual clock precision,
packaged shared-service control and physical failure domains remain unqualified.
See [the native NTP gates](../operations/ntp-runtime-test.md).

### Static address receipt replacement

Independent SQLite regressions reproduced replacement during initial native
observation, pending persistence and native re-read, for both acquisition and
withdrawal, with peer change or cancellation. `d2efd8f9` preserves the replacement
receipt and its Applied phase in all 12 cases; no native mutation occurs. The
independent replay also passed all 12. Nil prior ownership is a real expected
state, not permission to overwrite a newly acquired receipt. These userspace
checks do not establish atomic native/SQL CAS against unrelated external writers.

The 50 full feature scopes and 100 platform verdicts remain unknown. These
scoped observations do not reduce the complete routerd 1.0 preservation goal or
authorize production cutover.

### Whole-suite integration and exact-source FreeBSD replay

Whole ordinary Go tests at clean `309af77f` completed on both platforms:

| Platform | Top-level pass | Top-level fail | Top-level skip | Package pass/fail/no-test | Seconds |
| --- | ---: | ---: | ---: | --- | ---: |
| Linux | 4235 | 1 | 23 | 120 / 1 / 9 | 430.216 |
| FreeBSD 14.3 | 4023 | 1 | 212 | 119 / 1 / 9 | 205.906 |

The sole failure on both was the old expected worker list in
`TestContractFrameworkRegistrationAndScheduledSelection`: the actual new Plugin
worker was absent from that test's expected names. Test-only `0c3b60f6` adds the
actual worker to the expectation; the focused contract passed on both Linux and
FreeBSD. These focused results do not relabel the original whole runs as passes.
Source trees remained clean before/after both whole runs.

The final NTP production source, including `59104e2a`, then ran from clean
integration `0c3b60f6` through the same real FreeBSD ntpd gate: PASS in 91.404
seconds, no skip, with guards, packets, restart/reopen/When transitions, owned
process Wait, final cleanup and independent post-run absence confirmed. The
exact-source chain binary SHA256 is
`d90341f932ccbf932d48bcf0c6326070955414fa120c792ff34bc6538ba9fc50`.
This closes the later-source replay caveat of the earlier `1c389ab8` packet
result; shared rc.d/sysrc and actual clock discipline remain separate gates.
The same binary passed the static receipt replacement's 12 cases natively.

The production router binary from `0c3b60f6`, SHA256
`72ffa88851dc88976f20e3e53113fe60c02cbaebad63916839ee0f6742a7e3c1`,
also passed the actual FreeBSD TunnelInterface parent/IPv4StaticAddress/IPv4Route
child gate in 26.623 seconds. All 11 stages passed: IPIP/GRE traffic, repeated
no-op, child acquisition, parent reconciliation preserving child effects,
child-only withdrawal, MTU change, GRE key 42 to zero and restart/rekey,
owned parent cleanup and both foreign-preservation cases. Seven three-packet
checks each received all three replies. Independent post-run checks confirmed
the original interfaces, no trial jails or routerd process, and restored trial
module presence. The fixture still suppresses some cleanup command errors; this
run's overall cleanup conclusion uses those separate native post-checks and does
not qualify its cleanup fault handling.

Standalone NTP previews were separately reproduced publishing Applied despite
zero provider commands, no generated file and no acquired receipt. `b34d87b8`
changes only that unexecuted display to Rendered. Its same SQLite regression and
all normal NTP tests passed; the live packet path above is unchanged.


## DHCPv6 native address ownership and DAD

The Linux native gate passed at product source
`d05524de0f04ef7f77fb1a4051bea35ff729b44a`, including the separately verified
NTP/plugin/system-policy integration. It used a static `make dist` archive on
an isolated Ubuntu 24.04.4 VM with Linux 6.8.0-124-generic. Router and independent
Kea 2.4.1 peer ran in distinct network namespaces; supervisor runtime and state
paths were privately mounted. No installed service configuration was replaced.
Exact archive, executable and script hashes are recorded in
[the qualification ledger](routerd15-feature-qualification.json), entry
`linux-dhcpv6-native-d05524de`.

Reproduce with `scripts/linux-dhcpv6-native-smoke.sh --disposable-vm ARCHIVE
SHA256 KEA_PACKAGE_ROOT` and the
[native DHCPv6 runbook](https://routerd.net/docs/operations/development#linux-native-dhcpv6-address-gate).
Pass requires declaration-to-supervisor acquisition, finite `/128` readback,
DAD completion, three peer packets each way, service stop/restart preserving the
SQLite acquisition, timer-driven renewal, actual 90-second expiry with Kea
stopped, peer-only resumption and automatic recovery, and configuration removal
that withdraws the address before retiring ownership. A conflicting independent
host must trigger server-observed Decline and native withdrawal. A subsequent
combined PD/NA configuration must acquire both associations through its shared
worker and retain native address ownership. All these checks passed; post-run
inspection found no fixture namespaces or Kea process and the installed router
remained Healthy. Successful output and resolved failure output were removed.

This native gate was rerun at product revision
`1392082fd634ebd26ca0a06b0edc6315411b7a60` with formal static package
`v20261101.4`. The current run passed IA_NA acquisition, finite `/128`, DAD,
bidirectional traffic, isolated daemon restart, Renew, expiry/reacquisition,
withdrawal, duplicate-address Decline and combined PD/NA ownership. Its expiry
assertion was corrected to fail explicitly on a surviving address after
ShellCheck identified that a standalone negated pipeline could bypass `set -e`;
`bash -n` and ShellCheck now pass. The installed service stayed unchanged and
Healthy. Archive/script hashes and the scoped result are in ledger entry
`linux-dhcpv6-native-1392082f`; successful temporary output was deleted.

Two failed gates were diagnosed before retrying qualification. The first fixture
used a global peer source while assuming `/128` acquisition also installed an
on-link `/64` return route. A local isolated real-kernel reproduction showed the
missing route, and scoped link-local peer traffic succeeded in both directions.
The corrected fixture certifies that path before starting product qualification;
it does not add a fabricated product route.

The second gate exposed a product bug: Linux deleted a finite address immediately
on DAD failure. Its `RTM_DELADDR` carried `dadfailed tentative dynamic` within
24 ms, so periodic address dumps saw absence and reapplied the same lease. The
new isolated real-kernel regression first failed with no Decline, then passed
in 1.16 seconds after subscribing before address addition and committing the
exact acquisition's failure before sending Decline. A bounded subscription also
honors cancellation and drains its reader at close. This follows actual kernel
notifications rather than treating arbitrary address disappearance as a duplicate.

SQLite retains upstream authority separately from pending/held native ownership.
Expiry cannot extend an address, uncertain writes cannot adopt a present foreign
tuple, and withdrawal rechecks ownership deadlines after observation. Read-only
status projection removes expired applied addresses without deleting cleanup
records. Protocol status and generic garbage collection do not own this journal.
The local deadline, foreign tuple, DAD, restart, no-write and infinite-lifetime
regressions pass. The integrated ordinary suite passed 121 packages and 4,263
top-level tests, with 26 explicit skips. Targeted race tests passed; the privileged
native test ran separately with explicit namespace isolation. FreeBSD chain
cross-compilation passed, which is not FreeBSD runtime qualification.

Reviewed entry points and consumers include
`pkg/controller/chain/{chain,dhcpv6_worker,dhcpv6_address,dhcpv6_address_source,dhcpv6_address_store,ipv6_dad_linux,pd_address_observation,native_parent}.go`,
`pkg/state/{dhcpv6_address,sqlite}.go`, `pkg/pdclient/{client,address_client,decline,snapshot}.go`,
`cmd/routerd-dhcpv6-client/{main,address,decline}.go`,
`pkg/lifecycle/{registry,gc_plan}.go`, `pkg/apply/engine.go`, their related tests,
and the operator runbook. The Linux path is now qualified for the stated fixture.
WAN routing/forwarding composition, other source modes, multiple-prefix/server
behavior and native FreeBSD remain separate gates. The feature preservation and
platform-wide verdicts remain unknown; this result does not close routerd 1.0
functional preservation or authorize a release.

## Linux-first qualification and canceled retention recovery, 2026-10-31

The operator selected Linux implementation and acceptance as the current
priority. Published FreeBSD work remains retained; FreeBSD-only native checks
and new OS-specific work are deferred, not relabeled as completed.

The clean integration at `14b128dd` passed the Linux ordinary Go gate with
4276 top-level passes, 26 skips and no failures (121 packages passed, nine had no
tests; 381.308 seconds). The same revision's FreeBSD run had 4060 top-level
passes, 214 skips and one failure: the canceled archive test immediately read
SQLite while the canceled driver transaction still held its lock. That failed
FreeBSD run remains a failed historical result.

`2e99c7e7` repairs the shared cancellation boundary. A deterministic Linux test
uses real SQLite and pauses only its driver rollback. Before the repair,
retention returned while an independent `BEGIN EXCLUSIVE` still reported
`SQLITE_BUSY`. Reserving `sql.Conn` and closing it after the transaction rollback
joins the driver's actual completion. The same test passed five runs, the
logstore package and eight retention controller tests passed, and the independent
review confirmed that archive, identity, deletion and immediate-reader
conditions were preserved. FreeBSD runtime replay is deferred.

`make check-routerd15-system-native-linux` ran the actual Hostname and Sysctl
controllers in private UTS, network, user and mount namespaces with a private
sysfs. Both native cases passed without skips: hostname acquisition, SQLite
reopen, update and management release; a real network sysctl change, private
persistent fragment and converged second reconcile. The outer hostname,
forwarding value and namespace identities matched before and after. The test
and production source blobs were those of `2e99c7e7`; the new Makefile entry and
development procedure were uncommitted at execution and their hashes were
captured unchanged. This does not qualify package boot, all profile values or
FreeBSD. See the [system policy gate](../operations/development.md#linux-system-policy-native-gate).

### Linux ordinary gate after the shared rollback repair

Clean `b3036dec` passed the full Linux ordinary Go run: 4277 top-level passes,
26 explicit skips, zero failures; 121 packages passed and nine had no tests
(532.167 seconds). Source remained clean before and after. The same revision
passed full `go vet`, spec/config/control/OpenAPI/website schema consistency,
and production builds plus static-link verification for all 20 Linux release
executables. This result does not qualify unexecuted native gates or later source
changes.

The subsequent Linux logging process regression uses two real serve processes,
a private persisted SQLite cursor and an independent HTTP destination. The first
process's acknowledged record was not replayed and a record inserted while it
was stopped was delivered after the second process started. See `e6edf265`.
Linux cancellation race checking also passed separately.

The retained two-WAN gate at product `58bef4e9` and fixture `2ec2460b` passed
independent HTTP forwarding, loss/return, natural Health sample expiry, total
upstream loss, SQLite restart and declaration withdrawal. The
[operator procedure](../operations/development.md#linux-native-health-to-pbr-gate)
and ledger retain its exact package/hash and exclusions. This gate starts its
Health helpers explicitly; it is not a systemd/boot acceptance result.

Further repairs report TrafficFlowLog destination failure as a local Degraded
resource while retaining aggregate observations and threshold notification;
required status-write failures remain errors (`4b97f5a9`). Numeric UID/GID are
preserved when saving/restoring live-image caches (`03618b21`, with an actual tar
regression). Debian/Ubuntu IPsec dependencies now include the real IKE daemon
package, `charon-systemd`, as well as `strongswan-swanctl` (`885888ea`); the ISO
package list matches. Native IKE/ESP, OS daemon startup and the first real ISO
build/boot remain pending isolated Linux VM checks.


### Linux IPsec package, ownership and provider recovery gate

Product `dc14c48673e249ef489016d9ed9eea0208cbf4ce`, fixture `255deff6`,
Ubuntu 24.04.4 / Linux 6.8.0-124-generic, strongSwan
5.9.13-2ubuntu4.24.04.5 were exercised on an assigned disposable VM.
The `make dist` archive SHA256 was
`7c9d28ca0b89c26a4ac32c4ee7ed1b048cf64ad2417a0c87a378da5282aaea08`;
the executed fixture SHA256 was
`61ad446f8f49d3fc9f74a64fd3b0c638ffcca187cbeedd22c1119347f84c6ec1`.
The [provider procedure](https://routerd.net/docs/operations/linux-ipsec-provider-test) records the
reproducible prerequisites, invocation and acceptance criteria.

Passed: actual PackageController installation of the six initially absent
dependencies, real systemd/VICI startup, independent classic-charon peer IKE/ESP
and bidirectional traffic, unchanged-reconcile SPI/PID/invocation retention,
withdrawal and reacquisition while preserving an unrelated connection.
The fixture loaded the unrelated connection before product acquisition;
it did not use global load-conns after product ownership began.

The stronger automatic-SA-recovery gate **did not pass**. After SIGKILL,
systemd restarted the provider once and running routerd restored its VICI
connection and shared key. Ordinary bidirectional traffic still succeeded,
but used the pre-crash kernel SPIs: each counter increased from four to eight
packets. The new provider's SA list was empty. The receipt was acknowledged,
with one connection/key and no pending operation. Consequently the fixture's
new-provider ESTABLISHED assertion failed at `provider-crash-recovery`.
An earlier explicitly initiated SA succeeded; that is not automatic recovery.

This is neither an observed traffic outage nor an established 1.0 regression.
The baseline renderer and current VICI connection both use `start_action=trap`.
Natural kernel-SA expiry, rekey and subsequent peer recovery remain unverified.
Reproduce those lifecycle paths locally and establish the provider-recovery
contract before choosing a product fix; do not add global XFRM flushing or an
unconditional provider restart to satisfy the fixture.

Cleanup passed: child exits were acknowledged, all owned effects were withdrawn,
added packages/AppArmor profiles and the stale provider socket were removed,
module/link/route/rule/package inventories matched, XFRM was empty, and original
binary/config/unit/stopped-state hashes matched. The installed routerd returned
Healthy with no controller errors. This does not qualify all IPsec modes,
platforms, algorithms, certificate authentication or complete 1.0 preservation.

### Linux ordinary gate and native IPsec follow-up

Clean `dc14c486` passed all ordinary Linux Go tests: 4281 top-level passes,
26 explicit skips and zero failures; 121 packages passed and nine had no tests
(445.513 seconds). Full vet and spec/config/control/OpenAPI/website schema checks
passed. Release QA offline ran 499 tests: 492 passed, seven skipped; these are
local/offline tests, not live cloud executions. Formal `make dist` built and
checked all 20 static Linux executables. Its 96,956,121-byte development archive
SHA256 is `7c9d28ca0b89c26a4ac32c4ee7ed1b048cf64ad2417a0c87a378da5282aaea08`.
The later `00e85ba9` merge adds the IPsec script/procedure only; cmd/pkg/internal
and Go dependency files are byte-identical to this ordinary-gate source.

The dedicated Linux IPsec run used production `bab44306`, binary SHA256
`8cb8a3d170ec4f24a81f162a705419e8ed096aef6b47786b1838f14f6871f88d`,
and the executed `1189a939` fixture, SHA256
`db007dffaf09b8db1f0f45d189bbe5a97d7615941f4c024e061495560e5c44e4`.
The actual controller/SQLite/scoped VICI path acquired, converged, withdrew,
reacquired and recovered after a provider restart. Every initial/reacquired/
restarted phase had IKE on both ends, two ESP states per endpoint with four
packets/336 bytes each, and two-of-two bidirectional ping replies. Final receipt,
VICI and XFRM checks, actual child Wait and independent outer-state restoration
passed. The existing router service remained healthy.

The provider was supplied privately by the fixture. This is not package/systemd
startup, other authentication/algorithm modes, LAN transit or whole-feature
qualification. Two distinct network stacks use the same pinned strongSwan
implementation. Initial permission failures occurred before product startup;
operator certification supplied echainiv before the final unchanged-module run.
`8f75fcd4` makes these discovered prerequisites explicit and retains the executed
fixture's historical hash. See the [IPsec procedure](../operations/linux-ipsec-process-test.md).

## Linux live ISO boot and ServiceUnit recovery, 2026-10-31

The first actual Ubuntu live ISO was built from `dc14c486`, SHA256
`60cf131393a5d82750dd4da35ffa131066c18f84bbba51d67b8b11c2d585353f`
(1,401,188,352 bytes). Its booted kernel was 6.8.0-147-generic. The disposable
VM had no network interface or writable disk. Firstboot completed, the daemon
started and the real control API answered, but readiness **failed**: the image
builder had emitted an unowned legacy DNS systemd template. The controller
correctly refused to remove it. An unused conntrack module also produced an
honest Unavailable observation; that observation was not the feedback cause.

The stale ServiceUnit moved through Removing/Error on each reconcile. Its
unconditional self-status subscription re-entered the same worker 10,159 times
in 99.564198 seconds. `bf22e472` removes that self-subscription while preserving
explicit named When dependencies and periodic recovery. The new actual SQLite,
evented-store, bus and Framework tests also exposed a separate 1.0 regression:
ServiceUnit status had lost its system API namespace in the resource-kind lookup.
`919499c4` restores that existing runtime-status mapping, without inventing a
configuration kind. Three actual-path regressions pass, including the real
canonical `ServiceUnit/external-gate.service.status.phase` lookup. Resourcequery
and Framework suites and the two affected packages' vet checks also pass.

A diagnostic replay replaced only routerd/routerctl in the running ISO's RAM
overlay with official static builds from `919499c4`. The original ISO and its
unowned DNS template were retained. Across 15.046025 seconds, service-unit ran
14 times on its one-second periodic error-retry interval, with the same PID and
zero automatic restarts. DNS and configuration hashes were unchanged, and the
API still reported the expected Error. This qualifies feedback suppression,
not the original ISO's readiness or a clean boot of a corrected image. After
capture the daemon was stopped, the diagnostic disc unmounted and the original
configuration disc restored.

The builder now uses the canonical distribution payload, including provider
executors and their libexec manifests, and no longer emits that unowned DNS
template (`27a70036`, `363678de`). A corrected image must still be built and
booted. Separately, real cache restoration lost ping's file capability despite
retaining numeric ownership and modes; unprivileged raw ICMP failed after
restore. That cache is not accepted for subsequent image qualification.


## Corrected live ISO clean boot and independent-client traffic, 2026-11-03

Canonical ISO product source `0ee9777ec922a5aafff8d95bd6d109c563d7d742`,
ISO SHA-256 `fba24c3b714c8779e608de047d952ad45735fb0a302ade9252e6cc1d62b6a569`,
booted in a fresh Ubuntu 24.04 / Linux 6.8.0-147 PVE guest with read-only ISO
and `ROUTERD_CONFIG` media and no writable system disk. The prepared 1.5 fixture
is `tests/fixtures/linux-live-iso-boot.json` at `5e5e9a76`; its SHA-256 is
`0b4e9e42993045b81198b3df1cbf13e4b8053f2c92806a07f7bcc3eee2e1928e`.
An initial preparation error retained a nonexistent raw route device name;
correcting that fixture and cold-booting the same unchanged ISO resolved it.
No runtime binary replacement or manual product-state repair was used.

Firstboot succeeded, bootstrap DHCP was removed, the canonical config matched
the input, and all 19 resources became Healthy. The former foreign DNS template
was absent. All 24 installed binary/plugin artifacts matched the canonical
release payload. Numeric owner/mode checks preserved root-owned setuid passwd,
shadow group/mode and sticky temporary directories. A fresh v3 rootfs cache
round trip preserved ping's exact capability bytes; its restored binary also
sent raw ICMP as an unprivileged user in an isolated network namespace.
The maintained cache test at `04a11b37` explicitly checks `security.capability`
when run as root; its native writer/reader check passed.

A separate continuously running Ubuntu peer VM passed DHCP acquisition and
same-address natural renewal with unchanged boot ID/DHCP PID; local/upstream DNS
A over UDP/TCP; peer-observed IPv4 NAT; management-to-LAN positive control; and
WAN-to-LAN, LAN-to-management and WAN-to-router DNS denial. A WAN peer loopback
endpoint outside the connected WAN subnet additionally proved default-route
transit and NAT. Native deny counters were nonzero. Routerd retained its PID
and zero automatic restarts, and the guest reported systemd running.

Empty intent reached Healthy with zero resources, removed the native filter
and withdrew all three managed IPv4 addresses. Peer cleanup completed; both
disposable VMs, disks and their ISO/config/snippet media were removed.
This is the clean-boot/IPv4 scope of the
[maintained procedure](../how-to/pve-live-iso-nocloud.md#independent-client-boot-check),
not IPv6 image qualification, NoCloud/provider URL bootstrap, persistent storage
or whole routerd 1.0 parity. The earlier failed ISO observations remain historical;
the previously missing corrected-image clean boot and v3 capability restoration
now have native results.

## Native-package live ISO checkpoint (2026-11-03)

ISO integration `1ee100ee` installs the shared native `.deb` with apt into the
image; the image and config-drive startup use `/usr/sbin` and `/etc/routerd`.
ISO SHA-256 `7a37b6fca06d489ae4b2c57cb66cc620386dae2c20a97ffebc5444ac9ebca138`
booted on Ubuntu 24.04 / Linux 6.8.0-147 with no writable OS disk and package
`1.5.0~20261103022751+g1ee100eebe1c`. Native package file verification was clean,
there was no `/usr/local/sbin/routerd`, and the reviewed config-drive fixture
reached Healthy with 19 resources. Sticky temporary-directory modes and ping's
file capability were preserved.

A separate continuously running peer passed DHCP acquisition and four natural
renewals of the same address without a boot-ID or DHCP-PID change, DNS A over
UDP/TCP, IPv4 NAT and transit to an endpoint beyond the connected WAN subnet,
management positive control and isolation with nonzero native deny counters.
The router daemon kept its PID with zero automatic restarts. Empty intent reached
Healthy with zero resources and withdrew managed addresses and filtering; actual
apt removal retained configuration/state and removed owned files/helpers. The
zero-resource status omits its zero-valued resourceCount field; inspect it with
the maintained procedure's default-to-zero rule. A pre-apply orphan-artifact
warning was observed; this result does not assert warning-free diagnostics.
The peer fixture was cleaned up and the ISO VM, disks and temporary boot media
were removed. The separate build guest was later removed after the native apt
gate. This result does not qualify IPv6 image behavior, provider-URL bootstrap,
persistent installation or all routerd 1.0 functions.

## Native apt/pkg update checkpoint (2026-11-03)

The [native package plan](./routerd15-plan.md#native-package-installation-baseline-2026-11-03)
records the exact versions and revisions. Final actual lifecycle gates passed on
Ubuntu 24.04 / Linux 6.8.0-124 and FreeBSD 14.3-RELEASE-p16 / pkg 2.8.4, both amd64.
The new daemon executable differed from the prior one. Independent UDP/TCP DNS,
old main/helper process exit, unchanged configuration, a retained value in the
actual SQLite database, native package-file verification and removal passed.
Linux also retained an unrelated prefixed service. FreeBSD's injected stop failure
prevented replacement while the old daemon continued serving DNS.

The FreeBSD update command uses `HANDLE_RC_SCRIPTS=true` and an enabled native
service. Services launch through pkg's native post-hook rc.d handling; a hook's
descendants are reaped by pkg, so merely seeing an early startup log was not
accepted as recovery. Root file ownership, native rc.d mode 0555 and hook behavior
were also checked with native pkg tools. Native hook unit tests include pkg's
POSIX environment; local rc.d tests retain unknown-ownership refusal while
accepting a confirmed normal exit during lock/signal operations. The FreeBSD DNS
worker now has its existing native supervisor as owner (`bbbb9e79`).

Both disposable guests, disks and snippets were deleted; downloaded FreeBSD image
staging was removed. Successful temporary payloads and logs are discarded after
recording the reusable procedures and these scoped results. Release workflow
syntax/actionlint checks passed; the workflow itself was not run to publish a
release. Optional libnDPI native packaging, arm64 runtime and full 1.0 feature
qualification remain separate work. No main merge or production deployment was
performed.


### Linux apt multi-path DS-Lite and explicit-stop checkpoint, 2026-11-03

Product source `fc5d7816237758bcf15f68e320aff505f27b3b60` passed on a clean
Ubuntu 24.04 amd64 VM with Linux 6.8.0-124, real apt/dpkg and systemd. Four
independent kernel AFTR namespaces ran standard-library HTTP receivers; a
separate client namespace exercised the configured source-address hash policy.
The maintained [procedure](https://github.com/imksoo/routerd/blob/8faaafe28c069b69f379e16e42f2a0d877b7e0d5/docs/operations/development.md#linux-multi-path-ds-lite-package-gate)
and `tests/fixtures/linux-multi-dslite.json` define the inputs and assertions.

The first clean package start exposed apt chmod/rename failures in its private
cache under the main service's capability bound. Root UID alone did not suffice.
Test-first `dd1c748d` reproduced both actual filesystem failures locally without
changing packages, services or networking. `559e0349` supplied CAP_FOWNER and
CAP_DAC_OVERRIDE in both vendor and runtime units; the native filesystem checks
passed. `81d84f43` retained existing capability ordering. The subsequent VM start
installed its missing derived packages and reached Healthy with no controller
errors; a functioning tunnel alone was not accepted as a healthy router.

Post-run inspection also observed an explicitly stopped daemon being revived by
an already queued ten-second self-restart. Test-first `e41104e8` reproduced that
actual resurrection using persistent dummy services in the ordinary user's real
systemd manager. `fc5d7816` propagates stop requests to the timer without replaying
restart requests. Native checks passed persistent stop, exactly one intended
restart, and scheduling while the notify parent was still activating. The .deb
requires systemd 249 or later, the API version needed by that dependency.

The final campaign used gate `8faaafe2`. Review found that bare shell negation
under errexit did not enforce several absence checks; the gate now rejects
collisions, unexpected startup, old-process survival and remaining tunnels with
explicit failing branches. `1f27a937` reproduced both namespace/interface
collision escapes before fixture creation on a clean guest; the corrected gate
passed both negative cases, shell syntax/lint, and the full fresh-OS campaign.
The ordinary development-host guard test explicitly skips because that host has
an existing state database; the compiled guard test ran before product
installation in the clean guest, rather than counting that skip as a pass.

The final fresh-OS campaign installed package
`1.5.0~20261103065539+g559e03494009` and updated it to
`1.5.0~20261103073215+gfc5d78162377`. It passed:

- 64 independent source addresses, two new connections each, stable per-source
  selection and traffic through all three primary AFTRs; receivers observed the
  NAT-translated inner source `192.0.0.2`.
- One primary peer down selecting only the other two; all three down selecting
  the fourth AFTR; peer restoration selecting the primary pool again. Only
  independent peer links changed; product state was not repaired manually.
- Real systemd restart and apt update, unchanged configuration, prior main-process
  exit, native file verification, Healthy steady states and restored forwarding.
- Empty configuration withdrawing the tunnels and forwarded path. A subsequent
  local-only Telemetry intent created a pending main-unit handoff; explicit stop
  cancelled its timer and remained inactive beyond the handoff delay. This did
  not qualify collector delivery.
- Post-run readback: installed binary identifies `fc5d7816`, dpkg verification is
  clean, main is inactive, no fixture namespaces/interfaces or routerd nft tables
  remain, and the temporary successful capture/log directory is absent.

The full ordinary Go suite, generated schemas, all examples and four-locale
website build passed at the final product revision; existing unrelated website
link warnings remain. Root filesystem and non-root user-manager checks above
were separate opt-in native checks. All disposable Linux guests, disks, the retained pristine snapshot and staging
are removed after recording these scoped results.
One qualified current .deb is retained for the next authorized Linux campaign;
prior packages, duplicated payloads and successful raw logs are discarded.

This does not qualify delegated/RA outer-source expiry, real ISP AFTRs, existing
conntrack-flow continuity, router reboot, BGP, HA/lease/NAT-session synchronization,
or the complete deployed 110-resource router. The prepared full 1.5 configuration
validates, but validation is not its native replacement verdict. Linux remains
first; FreeBSD work is deferred and nDPI is explicitly excluded there by the user.


### Linux packaged DS-Lite existing-flow checkpoint, 2026-11-03

Product `ee50d67423fb9e49911fc4b42f9bc633c78a14b2` and maintained gate
`509d061333bc0f7a930055a01be1e14a1a4295a8` passed on a fresh dedicated
Ubuntu 24.04 amd64 VM, Linux 6.8.0-124, with actual apt/dpkg/systemd, four
independent kernel AFTRs and a separate LAN namespace. The package updated from
`1.5.0~20261103073215+gfc5d78162377` to
`1.5.0~20261103081632+gee50d67423fb`; the new .deb SHA-256 is
`b8edaff8cb47db612fe433a87ab9801f998790a640f6e7fa9ea8c0ebcdddf052`.
The driver SHA-256 is
`3c464e06ab915f7290d1297e4213b2db07dabfe720f2bde43d6993b806bb27b7`;
the fixture remains `56b46ce5feb3f89861794932089a396570965cd2260fa474f162e3f8312b072c`.

`2c69b2ac` first reproduced a defect: a surviving group member admitted the
entire original target list into the conntrack mark restore set. Failed members
therefore remained eligible even though new-flow hashing had withdrawn them.
The real-kernel regression retained failed mark 0x111 on an unchanged live UDP
socket. `ee50d674` copies only available members for restoration and excludes
explicit runtime-unready targets in the renderer. The same native test then
moved to 0x112, retained that healthy mark, moved to fallback 0x113 on total
failure, and kept healthy fallback after primary recovery. Controller tests
cover unhealthy probes, missing interfaces, total loss and desired-spec integrity.

The first extended VM attempt also exposed an incorrect test assumption: it
required every masqueraded flow to keep its original conntrack ID through an
output-interface change. Linux 6.8 explicitly removes that flow and drops the
transition packet in
[nf_nat_inet_fn](https://github.com/torvalds/linux/blob/v6.8/net/netfilter/nf_nat_core.c#L802-L809).
`tests/netns/masquerade-oif.py` reproduced the removal and recovery on the next
send using the same UDP socket, without routerd or manual state repair. IDs may
be reused after deletion, so an observed absence and successful replacement NAT
response matter more than comparing the old/new ID alone. The corrected gate
preserves IDs and paths for unaffected traffic and permits at most one client
retransmission for affected tuples. Bounded concurrent clients avoid expiring
healthy entries merely while serial clients await the transition drop.

The final fresh-OS campaign passed all previous new-TCP install/update, affinity,
partial/total failure, recovery, restart, withdrawal and explicit-stop checks.
After update it additionally passed 64 fixed UDP tuples through real DS-Lite/NAT:

- Initially all three primary AFTRs received translated traffic, with no resend.
- Partial primary loss moved 27 affected tuples to the surviving pair, each with
  one resend. Healthy tuples retained their conntrack IDs and AFTRs without resend.
- Total primary loss moved all 64 tuples to fallback, each with one resend.
- Primary recovery restored new TCP distribution while all 64 healthy fallback
  tuples retained their IDs and AFTR without resend.

No product conntrack flush or manual repair manufactured recovery. Independent
AFTR receivers verified the translated source. UDP peer response is certified
before qualification. Packet capture startup now fails on error rather than
silently losing diagnostics when tcpdump drops privileges in a private directory.
Final readback found product ee50d674, clean dpkg verification, inactive main
with MainPID 0, no fixture namespaces/interfaces or routerd nft tables, and no
successful raw output directory. The earlier failed VM and disks were deleted;
the successful guest and its disks were also removed after final readback.
Only one qualified current .deb is retained for the next Linux campaign; the old
package, duplicated payload/binaries and temporary failure/success output are removed.

The full ordinary Go suite (122 passing packages), schemas, all examples,
format/diff checks and four-locale website build passed. Native mark and
masquerade reference tests ran separately on development Linux 6.8.0-136 and the
6.8.0-124 guest. Shell syntax/lint and installation tests passed. The runbooks in
English/Japanese carry commands and pass criteria; raw success logs are temporary.

This is not delegated/RA outer-source expiry, real ISP AFTR or remote TCP-session
continuity qualification. Dynamic delegated sources, BGP/HA, lease/session
synchronization and the full deployed-router replacement remain active Linux
work. FreeBSD does not gate that work; its nDPI exclusion remains in effect.

### Linux packaged delegated DS-Lite lifecycle, 2026-11-03

Product and gate source `197bd70f7f49e2d81c30bcf629a3a444b90b7c8f` passed the
maintained `scripts/linux-dslite-pd-smoke.sh` procedure on a fresh dedicated
Ubuntu 24.04 amd64 VM, Linux 6.8.0-124, with real apt/dpkg/systemd and independent
Kea 2.4.1. The active proposal is PR #1365; the 1.0 baseline remains origin/main
`e3b49dbb69c7de97bab9595884745eb7cbe93d92`. No main merge, release or production
cutover was performed.

The test follows the deployed source ownership path: one delegated /60 supplies
four WAN /128 declarations, each shared by its DS-Lite tunnel, and a delegated
LAN /64. Four independent kernel AFTR namespaces and a separate IPv4 client
exercise the source-hash policy, with peer-observed NAT sources. AFTR addresses
are fixed IPv6 values, matching the deployed selection mode; DNS-based AFTR
discovery is outside this result. Existing qualification of new/existing-flow
failover remains separate.

The actual installed package is
`1.5.0~20261103094404+g197bd70f7f49`, SHA-256
`c2ee14430b84d393c51726186c58208c0e533a67729369e71ca70d781f712b41`.
The maintained driver SHA-256 is
`a2ba25e6db4bc3dd788129f0455510c3cd22bdf8e59008df7d97615e81a6e308`;
`tests/fixtures/linux-dslite-pd.json` is
`6b9b169f81b6789a07f577aa7e8ce263d06d96dd84a3b80b4790f12ff91b1921`.
The complete remote gate recorded exit zero, followed by separate native
readback. The [EN/JA procedure](../operations/development.md#linux-delegated-source-ds-lite-package-gate)
contains prerequisites, commands and pass criteria.

| Requirement | Executed result |
| --- | --- |
| Acquisition and usable native authority | Real Kea /60 exchange; four finite /128 lifetimes and kernel tunnel local endpoints matched; LAN /64 present |
| Stable forwarded traffic | All 64 IPv4 sources made two connections per stage, retained source affinity and reached all three primary AFTRs; receivers observed `192.0.0.2` |
| Natural renewal | Helper lease transaction timestamp advanced with an independent Kea RENEW observation; finite sources and forwarding remained usable |
| Independent prefix replacement | Kea pool and AFTR return endpoints changed; four sources/tunnels moved to the new prefix, revoked old addresses disappeared and forwarding recovered; routerd and Kea PIDs stayed unchanged |
| Daemon restart | Actual systemd restart changed the main PID and restored the new-prefix source/tunnel/forwarding state |
| Natural expiry | Only Kea stopped; all four WAN sources/tunnels and delegated LAN address disappeared at lease expiry; client forwarding failed while routerd remained active |
| Peer-only return | Restarting only Kea restored the sources, tunnels and 64-source forwarding; no product-state repair or manual restart manufactured recovery |
| Declaration withdrawal | Empty apply removed sources/tunnels and converged to Healthy with zero resources, zero phase issues and no current controller errors |
| Native final state | Correct binary revision and clean dpkg verification; empty canonical configuration, inactive main/MainPID 0, no owned helpers, fixture interfaces/namespaces, routerd nft tables or successful raw-output directory |

This campaign found two product defects after successful forwarding and expiry:

- Native DS-Lite withdrawal unconditionally recreated a removed declaration's
  public row as Disabled/Withdrawn. `0f67b3de` reproduced both retained and
  already-pruned rows. `67b8e838` deletes public state only after native absence,
  keeps the owner receipt until deletion succeeds, and preserves a declaration
  retained but conditionally inactive. A failed deletion remains retryable after
  SQLite close/reopen. Native ownership and foreign-object checks are unchanged.
- The link observer updated current Interface rows but never retired removed
  observations. `adb8576f` reproduced a stale Down/InterfaceNotFound row making
  an empty router Degraded. `197bd70f` passes the full declaration into the
  observer and deletes only removed Interface observations of the owning API.
  Tests preserve conditionally inactive declarations, other APIs/kinds and retry
  input on enumeration/deletion errors. Observation cleanup makes no native
  interface changes; the native gate confirms the final aggregate state.

Environment preparation also corrected a Python receiver filename shadowing the
standard library and Kea startup before link-local DAD completed. Both were
reproduced locally; qualification starts only after independent peer readiness.
The gate observes completion of an already-scheduled initial main-unit handoff
before asserting steady-state PID preservation during renumbering. A local
user-manager timer/service check verified that observation; arbitrary fixed
settling sleeps do not substitute for it. Optional JSON omission of a numeric
zero resource count is handled according to the existing control API shape.

Files read for the implementation/call-path review include
`pkg/api/specs.go`, `pkg/controller/chain/phase2.go`, `dslite_endpoint.go`,
`dslite_native.go`, `link.go`, `chain.go`, `prepared_router.go`,
`runtime_outcome.go`, `pd_observation.go`, `pd_address_lifetime.go`,
`lan_native_receipt.go`, `system.go`, `pkg/controlapi/types.go` and
`pkg/state/sqlite.go`, plus their related tests. The prior multi-DS-Lite and PD/RA
procedures and private deployed input were checked before defining this gate;
private topology and credentials were not committed.

The full Linux Go suite at the final product revision recorded exit zero:
122 passing packages and 14 without test files. Schema/example checks, format/diff
checks and shell syntax/lint passed. Four-locale website verification is recorded
with the final result publication; existing unrelated link warnings remain.
All disposable guests/disks and temporary diagnostic/build staging are removed
after recording the scoped result. One current qualified .deb is retained for
reuse, rather than rebuilding it for unchanged product code.

This does not qualify actual ISP interoperability, custom/non-shared or RA-based
tunnel source modes, forwarded IPv6/SLAAC or IA_NA service behavior in this gate,
BGP/HA/when-role transitions, lease/session synchronization or host reboot with
this source combination. Existing results for those other client paths remain
revision-bound. These are the next Linux integration concerns; they do not
restore a FreeBSD prerequisite or declare the full deployed-router replacement
ready. FreeBSD remains deferred and its nDPI exclusion remains in effect.

### Linux packaged HA pair, initial handover, 2026-11-03

Scoped checkpoint, not complete HA or deployed-router qualification. Product
`83414e60824d0e097ca3b102668c8470c873153c`, formal package
`1.5.0~20261103102340+g83414e60824d` (SHA256
`19446957240f92a55d06710586fe3684685722dcffadd77d99f297e8d36127e1`),
two dedicated Ubuntu 24.04 amd64 / Linux 6.8.0-124 guests with real systemd,
keepalived, conntrackd, rsync/OpenSSH, busybox DHCP and independent FRR.
The synthetic [pair fixture](../../tests/fixtures/linux-ha-pair.json) at
`41640dc3` has SHA256
`87a8bd94a9fe606f9de55358556e149106ab767c87778711eb6fb312c886ae04`.
Its [procedure and remaining acceptance](../operations/linux-ha-pair-qualification.md)
are the reproducible record; private VM identities and credentials stay private.

| Executed path | Result and limit |
| --- | --- |
| SQLite synchronization checkpoint race | Actual local rsync previously reported Synced while losing a lease committed before synchronization. RED `abe522f1`; fix `83414e60` snapshots the read-only database with committed WAL contents before transfer. Real checkpoint/receiver reads, writer close, original receiver path, shared snapshot, failure/cancellation and cleanup checks pass. |
| Native DHCP and sticky lease replication | Actual client acquired the offered gateway/DNS; source and standby SQLite queries found its hostname and lease after successful sync. This is DHCPv4 plus the shared sticky database, not PD/v6 lease qualification. |
| BGP and forwarding | Both independent FRR sessions established; peer received the permitted LAN prefix. Both kernels installed the allowed learned prefix; an unpermitted advertised prefix stayed absent. TCP/UDP application traffic traversed the learned route with the shared SNAT source. |
| Exclusive election and VMACs | Active node published the gateway and both VMACs; backup retained its VMAC links DOWN and did not publish the gateway. An initial test incorrectly required absent backup links; a local real-kernel fixture confirmed the correct DOWN contract before continuation. |
| Actual active-VM power loss | Backup automatically promoted and published the gateway/VMACs. Original TCP socket and UDP source port resumed with the shared SNAT source; client and standby main PIDs remained unchanged. The independent server accepted exactly one TCP connection; no manual VIP or conntrack commit occurred. |
| Interruption observation | Maximum application-response gap was **26.902 seconds**, between consecutive numbered responses; this is recovery with interruption, not hitless failover. Cause was not established by the later successful-flow capture. |
| Active-node return | Not qualified. The test Ethernet scaffolding was not persistent across guest reboot, so the returned guest lacked its external LAN/WAN links. Ended this campaign and returned to environment certification; do not count this as a product pass or repair the environment inside qualification. |
| Local checks | Full Go suite exit 0: 122 passing packages / 14 without tests; schema/examples, gofmt/diff and four-locale website at the snapshot-fix checkpoint pass. |

The source snapshot protects against concurrent source writes/checkpoints; it
does not fence receiver writers or atomically group all lease files. Remaining
work includes the corrected cold-boot environment, non-preemptive return and
reverse synchronization, active daemon restart/withdrawal, PD/v6 state and the
shared-PD/four-AFTR combination with this HA path. The current promotion hook also
uses the deployed `dslite-sessions` instance name; arbitrary sync resource names
are not qualified. All full-feature and full-replacement verdicts remain unknown.

### Linux packaged HA lifecycle follow-up, 2026-11-03

**Not qualified.** Product `b698cb1df10520fe60a65910e8b99202866a68c8`,
package `1.5.0~20261103110542+gb698cb1df105`, SHA256
`fcf1775ffdc18dc7fc9d38737a2f988ae747b7544d8cafa083ffcf02acd42c54`.
The two Linux guests were recertified after fixing the fixture's persistent
LAN/WAN setup and performing an actual guest cold boot with clean product state.
The same public pair fixture at `41640dc3` was used. Native observations refined
the earlier socket-continuity result; recovering a socket is insufficient to
qualify all packets or absence of competing ownership.

| Executed path | Observed result |
| --- | --- |
| Packaged update | Both guests updated from the previous formal candidate through apt; package integrity and executed product revision matched. |
| DHCP/DNS/BGP and sticky replication | Real acquisition, independent FRR sessions, allowed/denied FIB prefixes, traffic and receiver SQLite reads passed again. |
| Power-loss recovery | Original TCP socket and UDP source port recovered without a client or standby-daemon restart. Response gap was 26.870 seconds. |
| Packets during that gap | **Failed.** Before recovery the standby emitted the client's original source address. Successful later traffic did not make this interval correct. Both lab and production lacked bridge-netfilter hooks, ruling out the initially suspected bridge-filtering difference. |
| Applicability to the first deployed target | The minimal fixture removes every NAT rule on backup. Both inspected deployed configurations retain two unconditional SNAT rules. The zero-rule condition remains a real regression scope, but is not imposed as a prerequisite on the first target that does not use it. Next target testing must preserve the deployed standby NAT condition. |
| Returned active guest | Final role was backup and the latest renewed sticky lease synchronized back. **Failed transient ownership:** packet capture saw that guest announce the shared WAN address before settling as backup. Persisted master state reached startup consumers before live VRRP observation. |
| Complete declaration withdrawal | The conntrackd/VMAC fixes stopped and removed their native helpers, files and links on both guests. Full runtime status still failed: removed BGP observations remained Reconverging, and an old lease-sync failure also survived in current issues. FIB withdrawal was not separately read back in this campaign. |
| Local verification of the follow-up fixes | `d8c021c7` retires removed conntrackd instances with durable retries. Actual unprivileged systemd verified a retry after unit unlink; isolated kernel checks verified owned VMAC deletion and foreign-link preservation at `b698cb1d`. Full Go suite at that checkpoint: 122 passing packages, 14 without tests, exit 0; schema/examples and four-locale website passed. |

The returned-node authority failure has a local RED at `2e9b15e8` and an initial
fix at `479f24d7`: `Runner.Start` refreshes native VRRP role observations before
building any controller generation or evaluating role-gated effects. Focused
checks cover inactive/backup roles, preserving a live master on daemon restart,
foreign configuration and cancellation. Native return with that fix is pending.
The zero-rule NAT experiment also reproduced source leakage locally; retaining
only empty base chains was insufficient, while retaining a conntrack expression
preserved the existing binding. No speculative production NAT change was made.

Both disposable guests and all their virtual disks were removed after the run;
the base image remained and both production guests were read back as running.
Only bounded current packet inputs for the unresolved regressions remain private.
Do not mark a whole HA feature, full deployed replacement or release as passed.

### Linux packaged HA lifecycle with declared unicast source, 2026-11-03

**Scoped PASS: the 20-resource IPv4 pair lifecycle, not full deployed-router
qualification.** Product `1a96b8c0383062a06e7e31faea1dbbf2d19375db`, formal apt
package `1.5.0~20261103132525+g1a96b8c03830`, SHA256
`1daade5c3749ccd6713caee1eb255cc18baf9e85d7c46ccc23d5768287c1d6c5`.
Two dedicated Ubuntu 24.04 / Linux 6.8.0-147 guests; real systemd, keepalived
2.2.8, conntrackd, rsync/OpenSSH and independent FRR/application/client namespaces.
Both guests were cold-booted during certification with clean product inputs,
no old volatile election state and persistent isolated LAN/WAN links. The
[pair fixture](../../tests/fixtures/linux-ha-pair.json) at `9440c932` has SHA256
`6034bd8cc00bc0ccd00526e5518a747195f638e25db07a8398da491a105fbeee`.
Unlike the initial fixture, its standby retains an unconditional SNAT rule,
matching the inspected deployed condition.

The intermediate `479f24d7` retest confirmed cold-return WAN ownership and the
original TCP socket, reverse sticky synchronization and active daemon restart.
Its UDP result was invalid: changing the independent server to a wildcard UDP
bind changed its reply source. A local kernel reproduction established that
fixture error; the corrected server uses explicitly bound destination sockets
and certifies reply identity before product startup. Subsequent injection requires
both TCP and UDP success; no result from that invalid UDP path is counted here.

A later `7f7b5779` start exposed a real two-master failure. Both directions carried
correct VRRP advertisements, while the standby raw receive socket was bound to
the shared VIP and its received-advertisement count remained zero. Its current
transmit source had already changed to the node address. Isolated Linux 6.8.0-136
and real keepalived reproduced the retained receive bind after VIP withdrawal.
`2d1fc893` fixes this failure as a regression requirement; `1a96b8c0` renders an
explicit source from the unique unconditional node IPv4 address. The corrected
native fixture fails against the previous renderer and passes the fix. Its
independent peer uses the same zero-VIP advertisement format as graceful VRRP;
the component test owns VIP withdrawal and omits VMAC notify commands. The
packaged gate below executes the full generated hooks.

| Acceptance path | Executed result |
| --- | --- |
| Formal package and exclusive startup | Apt update and `dpkg -V` passed on both guests. Generated configuration and actual raw sockets selected each node's own unicast address. Active owned the gateway/VMACs; backup links were DOWN without competing shared IPv4 addresses. |
| Real clients, DNS and BGP | DHCP supplied the configured gateway/DNS and sticky identity. Both independent FRR sessions established; allowed learned routes entered both kernel FIBs, a forbidden prefix stayed absent, and application traffic traversed the learned route. |
| Synchronization and source reads | Source and receiving SQLite queries confirmed the actual client hostname/lease. The standby external cache contained the specific established TCP and assured UDP tuples before injection. |
| Active guest power loss | The original TCP connection and UDP source port resumed automatically. Client and surviving main PIDs stayed unchanged; the peer accepted exactly one TCP connection. Maximum combined successful-response gap was 10.121 seconds, not hitless operation. |
| Handover packet source | All 115 captured outgoing application TCP/UDP packets retained the shared SNAT source. Independent new flows also exercised the unconditional node-specific SNAT path. This does not qualify the separate zero-NAT-rule standby case. |
| Promoted-node lease renewal | The same independent DHCP client renewed through the promoted node without a client restart or manual product repair. |
| Non-preemptive return | The returned guest remained backup; its WAN capture contained two physical-MAC ARP frames and zero shared-WAN claims. Final shared LAN/WAN IPv4 ownership stayed on the active node. The latest renewed SQLite timestamp synchronized back exactly, and original TCP/UDP traffic continued. |
| Active main restart | Main PID changed while keepalived, BGP daemon and client PIDs stayed unchanged. The original TCP/UDP sockets and learned-route forwarding continued; live master ownership was retained. Both routers reported Healthy with 20 resources and no current errors after return. |
| Complete withdrawal and explicit stop | Both reported Healthy, zero resources/issues/current errors. All fixture host/shared IPv4 addresses, both VMACs, managed keepalived/conntrackd configuration/unit files, learned FIB prefixes and the owned NAT table were absent. No owned helper remained, package integrity passed, and both main PIDs stayed zero without pending deferred restart units. |

Withdrawal status retirement has RED `f04eaf62`, fixes `497f40c3` / `7f7b5779`:
BGP observations retire after acknowledged process/FIB/systemd removal; removed
lease-sync declarations retire after the old generation stops. Failed stop or
status deletion remains retryable, `when`-disabled declarations stay visible,
and dry-run acknowledgements cannot authorize live status deletion. Parent
cancellation no longer fabricates a current sync failure; a real transfer timeout
still does. These checks and native withdrawal passed, rather than filtering old
errors out of the aggregate health result.

Local verification: full Linux Go suite exit zero, 122 passing packages / 14
without tests, at `a5a9aaa6` with the same product code; generated schema/example
checks, formatting/diff checks and the four-locale website at the product
checkpoint passed. The Linux golden output changed only by the two explicit
source directives; its example name does not imply a FreeBSD native test. The
new result publication receives a separate website build.

Reviewed paths include `pkg/render/vrrp.go`, `pkg/controller/vrrp/backend.go`,
`controller.go`, `initial_roles.go`, `pkg/controller/chain/chain.go`, `system.go`,
`system_dns_removed_status.go`, `filesync.go`, `pkg/controller/bgp/controller.go`,
`cmd/routerd-vrrp-vmac/main.go`, `removal.go` and the related focused/native tests.
The maintained [procedure](../operations/linux-ha-pair-qualification.md) records
the native regression command, prerequisites and acceptance. Private access and
infrastructure details remain private; successful raw packet logs are disposable.

This does not qualify shared PD/four-AFTR HA, PD/v6 lease synchronization,
translated-port collisions across HA, IPv6 client continuity, both-direction VM
power loss, arbitrary session-sync names or the complete 109-resource deployed
configuration. The next integrated campaign places its independent peers outside
both router failure domains and reuses the current formal candidate. Full feature
and full replacement verdicts remain unknown; FreeBSD remains deferred.

## Linux shared-PD HA startup follow-up, 2026-11-03

The `70cade06` formal apt candidate has not qualified the combined 70-resource
HA/PD/four-AFTR input. Independent Kea, radvd, routed AFTRs, FRR and application
peers were prepared outside both router failure domains. Certification caught
and locally reproduced an IPv6-overlay transport interaction with the tested
host-traffic policy; exact guest transport-peer routing exceptions restored
multicast, verified from both temporary VMACs before restarting qualification.
The fixture's activation predicate was also corrected to the deployed policy
`datapathState: Ready`, rather than the invalid `phase: Ready`.

The recertified attempt found keepalived MASTER while routerd's election marker
remained BACKUP, preventing PD acquisition and gateway publication. Native logs
show cache import and a conntrackd unit restart in the same promotion interval;
the original notification's exact failing operation was not captured, so its
cause is not asserted. Local test `ae168692` demonstrates that an interrupted
activation loses its retry authority, including a cache-import interruption.
The repair records `activating` before preparation, permits the controller to
retry that pending election and publishes master authority only after successful
cache/VMAC/RA preparation. A later BACKUP cancels stale activation repair.
Focused helper/controller tests pass; native confirmation is still pending.

The run ended without client fault injection. Withdrawal removed native helpers,
VMACs and learned FIB, but stale `Disabled/WhenFalse` DS-Lite statuses prevented
an empty Healthy verdict. This is an unresolved withdrawal defect, not a passing
result. Both router main services are stopped. The three dedicated guests are
being reused for the next integrated campaign; no production cutover occurred.

The `854648e8` repair candidate passed all 122 Go packages (14 no-test packages),
`make check-schema`, `make validate-example` and formal `make dist-deb`.
Package `1.5.0~20261103153846+g854648e8c5ef` has SHA256
`26fc1d71fec0e1126eda729136bbf99fdf4dd83eff625d2d0373514e2be0e4f4`;
both Ubuntu guests updated through apt and passed package integrity checks.
The native follow-up reached exclusive MASTER/BACKUP, real PD acquisition and
published gateway, passing the previously stalled startup path. Both nodes then
withdrew to Healthy with no current errors, owned helpers/VMACs/FIB, or declared
benchmark IPv4 addresses. This confirms the scoped startup/withdrawal repairs;
it is not integrated forwarding/fault qualification.

That follow-up also exposed a mistake in the prepared test input: separate
`IPv4StaticAddress` declarations duplicated the DS-Lite owner's inner `/32`.
The deployed older declarations use `/29` and distinct standard-range sources,
so the synthetic `/32` duplication must not be claimed as a deployed failure.
The prepared 1.5 configuration now has one inner-endpoint owner per tunnel and
explicit SNAT to its standard `192.0.0.2`; no old-configuration compatibility
machinery is added. The public input is 66 resources and both full private
candidates validate with 105 resources, preserving four tunnels and SNAT paths.
Existing client sockets and either-direction integrated power-loss qualification
remain unexecuted for this input.

## Linux integrated IPv4 forwarding and IPv6 default-route defect, 2026-11-03

At product `854648e8`, the corrected 66-resource input reached real DHCPv4,
SLAAC and stateless DHCPv6 DNS/domain acquisition, local DNS and IPv4/IPv6
listener forwarding of A/AAAA answers through the independent IPv6 upstream.
The first client attempt used an AppArmor-denied temporary hook; the valid probe
used stock permitted paths without disabling OS protection. DNS source selection
was corrected in input `9649bc8a` through ordinary configuration application,
so the earlier listener's configured NXDOMAIN is not a resolver defect.

Initial application observations contain 72 IPv4 source/port-collision flows
through the three primary routed AFTRs (20/31/21 flows) plus one TCP flow through
the learned BGP prefix. The native IPv6 TCP socket failed before fault injection.
The client's progress file stopped updating after that worker failed because
executor shutdown waited for the other workers; later elapsed time must not be
counted as measured success. The probe now cancels sibling loops on error and
bounds server reads. No integrated power-loss verdict is claimed.

Kernel route lookup sent the forwarded IPv6 client toward the management default
(metric 100), while the usable WAN VMAC RA default remained at metric 1024.
The helper queried routes with a `dev` selector, whose real iproute2 output omits
that same field; its device-aware parsers silently skipped route promotion.
Native local FIB test `8b787835` fails before the repair and passes after querying
all default routes and letting the existing parser select the VMAC. Focused
helper/controller tests pass. This component result is not native application
or failover qualification of the repaired candidate.

The integrated attempt ended and both router configurations withdrew to Healthy,
without current errors or owned helper/VMAC/FIB state. Subsequent native work must
use a newly built formal package containing the route-query repair. The complete
105-resource replacement verdict and both-direction HA session tests remain open.

The formal `624f885e` follow-up reached all 74 application flows, including the
original native IPv6 TCP socket, and actual sticky/PD replication on the standby.
Before any power fault, IPv6 stopped after the first upstream RA lifetime. The
copied metric-50 route remained expired while fresh RAs renewed metric 1024;
the helper's existing-route shortcut never renewed the copy. Fault injection was
not performed, and both configurations withdrew to Healthy and stopped.

`d605f312` adds a real-radvd/local-kernel regression. The final fixture fails
against the prior product via a Go overlay and passes with the repair: configure
`ra_defrtr_metric=50` on the VMAC, leaving native RA route ownership and lifetime
with the kernel. Repeated activation, live RA refresh beyond the first lifetime,
explicit withdrawal and natural expiry preserve the independent management
route. The obsolete copied-route builder and its static-route-only test are
removed. Focused helper/controller tests pass; the repaired formal package and
integrated power-loss paths are not yet qualified.

At `565de4b2`, kernel-owned RA preference remained healthy, but the first shared
PD renewal coincided with a DS-Lite health failure and an application TCP reset
before any power fault. The run ended and both configurations withdrew Healthy.
Local RED `4d3c527f` reproduces a still-valid shared source becoming
Pending/LocalIPv6NotReady when its lease extends. RED `737b40f7` also demonstrates
that the address actor publishes Pending/AddressApplying and an unconfirmed new
expiry during an existing held address's refresh, disabling Applied dependencies.

The repair reuses the existing held-acquisition predicate used by RA consumers.
A confirmed address covered by the current lease remains Applied with its old
confirmed lifetime until refresh succeeds; the DS-Lite borrower accepts that
shorter live acquisition while checking the actual native scope/index/address.
The in-flight lease recheck permits positive same-prefix renewal but rejects
shortened authority, identity changes and stale leases. Tests retain the native
tunnel token/index, reject expired or overlong acquisition authority, and verify
Applied dependencies never observe an unconfirmed extension. Focused DS-Lite,
LAN and held-RA tests pass; native confirmation of the repaired candidate and
both-direction power-loss qualification remain pending.

### Linux combined HA admission follow-up (2026-11-03)

Product `04ff8deb` passed the real same-prefix renewal with the original 74
application flows and unchanged four tunnel indices; the maximum observed
response gap at that stage was 0.520 seconds. All Go packages, schema/examples,
the formal `.deb` and four-locale website build passed. The subsequent power
loss did **not** qualify: BGP and native IPv6 resumed, while DS-Lite connections
retained their NAT tuples but acquired the legacy route's conntrack mark. A
bounded packet capture confirmed `192.0.0.2` packets leaving on the legacy LAN.

The policy counted a role predicate accepting both MASTER and BACKUP as a
master-only readiness gate. RED `317e78f6` uses the deployed predicate in the
existing convergence test. The fix requires MASTER exclusively, keeping
`datapathState: Converging` while only that fallback is ready. Local policy
regressions pass; integrated promotion remains unqualified until rerun. The
cold-return/withdrawal cleanup also encountered an RA receipt from the prior
boot before opening an unavailable link-local socket. RED `a0d3da44` reproduces
this through the actual SQLite journal and RA service. The service now retires
receipts from another kernel scope before opening sockets or sending withdrawals;
the old receipt cannot operate on a shared identity now owned by the active peer.
Local RA and route-policy regressions pass. Post-handover native withdrawal is
still unqualified; a stopped daemon is not a successful withdrawal result.

### Linux combined packaged HA result at a1d8ef19 (2026-11-03)

The complete lifecycle remains **unqualified**. On three dedicated Ubuntu 24.04
guests (two Linux 6.8.0-147 routers and an independent 6.8.0-124 peer/client),
formal package `1.5.0~20261103173704+ga1d8ef19cd4c` passed apt update, native
file verification, full local Go (122 packages; 14 without tests), schema and
example checks, and the four-locale website build. Both prepared 105-resource
full target configurations also passed validation; the actual native input was
the 66-resource integrated fixture, not those full configurations.

Observed subpaths:

- Real DHCPv4, SLAAC, stateless DHCPv6, local/forwarded A/AAAA through IPv4/IPv6
  DNS listeners; 72 colliding-port DS-Lite flows through three primary AFTRs,
  BGP TCP and native IPv6 TCP.
- A genuine same-prefix Kea renewal with all original 74 flows, ten single TCP
  accepts, no translated-tuple changes and unchanged four tunnel indices.
- Source/receiver sticky SQLite contents and PD identity, plus all 73 IPv4
  external conntrack entries including their original AFTR routing marks.
- A power loss and automatic B promotion: all original 74 flows resumed with no
  TCP reconnects or tuple changes and unchanged independent peer/client and
  surviving-main processes. Maximum observed response gap **106.696 seconds**;
  this is connection survival, not hitless or short-interruption failover.
- The same DHCP client/lease renewed on B; local and forwarded DNS still worked.
- A returned as non-preemptive backup while the original flows continued, but
  the latest sticky lease did not replicate back. B reported rsync exit 30 /
  60-second I/O timeout; A's actual SQLite request timestamp remained old.
  The transport cause is unresolved. The available return capture observes
  A-to-independent-peer overlay traffic, not the direct A/B SSH conversation.
  Do not blame the file-copy implementation or test transport without that
  missing observation. Main restart and reverse power loss were not executed.
- After ending the run, both routers applied an empty configuration, reached
  Healthy with no current errors, removed native helpers/VMACs/configuration
  and learned FIB effects, and were explicitly stopped. This includes the
  old-boot RA withdrawal path fixed above.

Next isolate reverse synchronization after cold return, obtaining both ends of
the direct SSH/TCP transaction before proposing a repair. Do not repeat the
whole campaign to guess at the cause. Public procedures and these scoped results
replace permanent raw logs; private access/topology details remain private.

### Correction to combined HA synchronization input (2026-11-03)

The authorized diagnostic rerun on product `a1d8ef19` failed during initial
A-to-B sticky synchronization, before power loss or cold return. Both endpoint
captures show a successful SSH handshake followed by the server source changing
from its node LAN address to its legacy-subnet address and a TCP reset. The
requested post-return phase therefore did not execute. Withdrawal on A had an
unconfirmed API timeout; B withdrew successfully. Both daemons and all three
VMs were stopped after collection.

Comparison with **both original deployed inputs and both prepared 105-resource
inputs** found that their legacy LAN SNAT already excludes all private
synchronization targets. The synthetic fixture omitted these exclusions when
mapping the private topology to benchmark addresses. This was a fixture
preparation error. The previously observed synchronization failures do not prove
a defect in the deployed peer path. RED `e601b7a2` pins peer exclusion and keeps
the public Internet application eligible. The input now carries the original
RFC1918 exclusions and the corresponding synthetic LAN/legacy/WAN subnets.
Both prepared full inputs already contain the required private exclusions.
The corrected input passed the bounded native synchronization diagnostic in
143.04 seconds on the same formal product package. Initial actual sticky
SQLite/PD contents, power loss, promotion, same-client renewal, cold return,
latest reverse SQLite/PD contents and all three synchronization statuses passed.
Both empty configurations reached Healthy and withdrew native effects.


### Preserving replicated marks during HA admission (2026-11-03)

The following 74-flow campaign passed initial forwarding, actual same-prefix
PD renewal with unchanged tunnel indices and original sockets, sticky/PD copy,
and all 73 external-cache IPv4 tuples with their original AFTR marks. A power
loss then exposed another admission failure. UDP translated identities changed,
and original TCP flows failed to resume. Later return, restart and reverse
power-loss phases were not executed. Both empty configurations subsequently
withdrew successfully and all three dedicated VMs were stopped.

Native state showed replicated primary marks rewritten to fallback mark `0x113`
while the promoted node was still preparing its gateway. The WAN and LAN shared
MACs become active before the deferred VIP, so withholding the VIP alone does
not prevent existing clients from forwarding packets into intermediate policy
rules. `applyDefaultRoutePolicies` supplied only currently healthy marks to
`NftablesEgressRoutePolicyDefaultMarks`, whose unmatched nonzero-mark rule then
rewrote imported connections during local path preparation.

RED `32369d6d` fixes the contract at the controller-to-renderer boundary. While
MASTER has an explicitly unconfirmed VIP publication, retain the declared
connection marks. New flows still use the available selected candidate. After
publication is confirmed, ordinary unhealthy-path reassignment resumes. Product correction `84b5d854` passes the focused regression and prior datapath
convergence tests. An explicit isolated real-kernel test also preserves the
original UDP conntrack mark during pending admission, then reassigns a failed
member after admission and retains a healthy fallback on recovery. The formal package and full local checks passed. The subsequent native lifecycle
passed in 354.90 seconds through both-direction power loss, both cold returns,
latest real lease/PD replication and active-main restarts on both sides. All
74 original flows survived with ten single TCP accepts and zero translated
identity changes. Maximum observed response gap was 53.209 seconds. Returned
nodes emitted zero shared MAC/IP/ARP claims. Both prepared 105-resource inputs
validated at this revision. Complete withdrawal failed as described below, so
the complete lifecycle and full replacement verdict remain unqualified.


### Deferred gateway removal after keepalived stop (2026-11-03)

The final empty-configuration check after the `84b5d854` lifecycle found the
active router's deferred LAN VIP still present. Its base addresses, helpers,
VMACs and generated configuration had been removed, and status incorrectly
appeared Healthy. The other router withdrew completely. Both daemons and all
three VMs were stopped after collecting the remaining address and statuses.

`removeConfiguredVMACs` parsed the owned generated hook but discarded its
deferred-VIP options, relying on keepalived's transition callback to delete the
VIP. Stopping keepalived does not guarantee that callback finishes the deletion.
RED `f55a4808` reproduces the retained VIP in an isolated real Linux kernel and
pins withdrawal order, retry after absent VMACs, foreign protection and failure
propagation. The correction validates all retained VMAC identities, explicitly
withdraws each configured deferred gateway, then revalidates and deletes the
links. A failure retains the generated configuration for retry. Focused ordinary
and isolated kernel checks now pass. The formal package at `4cb2451b` passed all Go/schema/example/website checks.
A focused three-VM run confirmed that the master could restart with its VIP
published, then empty configuration removed the VIP, VMACs, helpers, configuration
and learned FIB on both routers. The complete empty-health check failed on A
because an undeclared DS-Lite Pending status remained. B reached Healthy.
The successful communication results above retain their narrower scope.


### Retiring withdrawn pending DS-Lite status (2026-11-03)

The focused `4cb2451b` withdrawal retained only `DSLiteTunnel/ds-a` status
Pending/SourceOrDependencyChanged after all native effects had disappeared.
The endpoint/native tunnel journals were already clear. The existing dormant
status cleanup handled Disabled declarations but skipped Pending records left
by an interrupted source/dependency check. RED `47bf1c7b` reproduces this and
other unactivated Pending reasons. After native cleanup, an undeclared Pending
record with no retained tunnel or endpoint receipt is now retired through the
same checked deletion path. Declared resources, native receipts, dry-run and
read/delete errors retain their existing protections. Focused tests and the complete Go suite (122 packages passed, 14 without tests),
schema/examples, four-locale website and formal package creation passed. Product
`6dc12c92` was installed through apt on both dedicated routers with package
integrity verification. The focused native check passed in 87.93 seconds through
confirmed MASTER VIP, active-main restart, complete empty configuration and
empty-state daemon reopen/repeated withdrawal on both routers. Each ended
Healthy with no current errors/issues and no native VIP/VMAC/helper/config/FIB
effects. All three VMs were then stopped. The private cleanup wait now allows
45 seconds, covering the observed 15-second and default 30-second controller
periods instead of assuming every status is synchronous within 15 seconds.
The full 74-flow lifecycle remains revision-bound to `84b5d854`; later changes
are limited to removal and withdrawn-status cleanup and have focused native
retesting. Full 105-resource native replacement qualification remains open.


## 2026-11-04 combined Linux services checkpoint

Formal matching base/native-addon packages at `77f05a09` passed a 76-resource
combined run on two dedicated Ubuntu 24.04 routers with a separate independent
peer VM. The original 74 flows progressed through the DS-Lite paths with ten
single TCP accepts. Chained local/upstream DNS and forced external-destination
DNS over UDP/TCP returned the expected answers. An independent TLS server served
a forwarded client; the actual traffic-flow API recorded its SNI with native
libnDPI provenance. Direct and redirected NTP each returned three correlated,
synchronized replies at router stratum 3 (independent peer stratum 8).

Both routers then applied an empty configuration and became Healthy with no
resource errors, helper processes, VMACs, learned routes or owned NAT table.
This verifies the DPI-helper withdrawal fix `4efea2b8` and removed HealthCheck
status fix `77f05a09`. All three guests were stopped afterward and retained for
the remaining combined tests; no new guest was created. This is not the full
105-resource replacement verdict or another power-loss campaign.

The initial redirected DNS input had omitted the deployed node-address listener;
the input was corrected before this run. The initial SNI assertion queried
firewall events even though accept-event sampling was disabled; the actual
traffic-flow API proved native detection. Neither was a product defect.
A separate observed defect mislabeled native DNS names as TLS SNI. Its focused
native-library regression reproduces the erroneous DNS result before the fix;
that subsequent fix is not included in the `77f05a09` VM result.

## DHCPv6 server renewal across the packaged main-service restart

2026-11-06: product `a6d4e3bb45376ade0d5119c435bd47e97c965764`, formal matching
base/addon `1.5.0~20261106191737+ga6d4e3bb`, passed on a dedicated Ubuntu Linux VM.
The normal installed systemd routerd service owned dnsmasq; independent dhcpcd ran
in a separate network namespace with private `/run` and `/var/lib` mounts.

The stateful server supplied a two-minute IA_NA lease from its configured pool.
The client completed DAD, obtained a default route from routerd RA and fetched a
fixed IPv6 HTTP payload. An actual `DHCPRENEW` was observed before restarting
routerd. After restarting **only routerd**, a later natural `DHCPRENEW` succeeded;
the client PID and address were unchanged and the IPv6 HTTP payload still matched.
Empty configuration became Healthy with no managed helper or active dnsmasq before
the client, HTTP endpoint, namespace/veth and temporary files were removed. The VM
was then stopped. No product changes or repairs were needed for this gate.

To repeat in an approved dedicated Linux guest:

1. Verify the formal package revision/file integrity, empty canonical config and
   absent fixture links. Stop the empty main service. Create an isolated veth with
   a client namespace, wait for link-local/global DAD, and give only the SUT link a
   fixture IPv6 /64 address. Keep the client's global address unconfigured.
2. Validate and apply an external Interface plus stateful DHCPv6Server resource,
   `defaultRoute: true`, `logDHCP: true`, a /64 pool and `leaseTime: 2m`. Use normal
   installed systemd service ownership, not a controller-filtered shadow daemon.
3. Start independent `dhcpcd -d -6 -B --waitip=6 -t 30` with `ia_na 42` in its own
   mount/network namespaces. Within 60 seconds verify an address inside the pool,
   completed DAD, `proto ra` default route and an IPv6 HTTP payload from the SUT.
4. Record the client PID/address. Within 100 seconds require a dnsmasq
   `DHCPRENEW(<interface>)` event and successful HTTP; initial REQUEST/REPLY alone
   do not prove renewal. Record the current RENEW count, restart routerd, and within
   another 100 seconds require that count to increase with the same PID/address
   and payload. Do not restart the client or inject a lease/address.
5. Apply empty config and require Healthy/no helpers/dnsmasq inactive before
   removing fixture processes/network/files. Stop the guest. If a stage fails,
   retain the relevant bounded status and client/server errors for local diagnosis.

This closes the native full-service restart/renewal slice. It does not prove lease
expiry, prefix replacement, separate routed-LAN transit, host reboot, or FreeBSD.
Earlier qualification remains revision-scoped; complete 1.0 parity is not claimed.

## Bounded LogSink syslog delivery and packaged receiver recovery

2026-11-06, product `cfdc0a3f925677e8c33f20655ba1759687d4e5aa`, formal matching
base/addon `1.5.0~20261106193134+gcfdc0a3f`: a dedicated Ubuntu guest passed actual
TCP LogSink delivery to an independent Python receiver. The receiver checked the
syslog priority, configured tag and startup/operation messages. Stopping the
receiver did not prevent log-level operations or Healthy status responses within
eight seconds. Restarting it restored delivery from the same daemon/LogSink.
Empty configuration and daemon shutdown passed before receiver/file cleanup.

The preceding local test `a65258db` reproduced an unbounded write to a TCP receiver
that accepted but never read. The fix gives each serialized event a shared
five-second connection/write/reconnect deadline, closes a failed connection, and
returns its error. `pkg/eventlog` passed its real TCP deadline/recovery and
concurrent close tests; chain log-retention/archive tests passed. Archive delivery
failure remains an error, so retention cannot delete the original rows on that
basis. Full Go passed (122 packages, 15 without tests), schemas/examples and the
four website locales passed at the product revision.

Repeat the package check only in an approved dedicated guest:

1. Verify the formal package version, file integrity, empty canonical configuration
   and unused receiver port. Stop the empty daemon. Run a separate bounded TCP
   receiver (600-second lifetime, 1 MiB output cap).
2. Before normal systemd service startup, configure a syslog LogSink pointing to
   that receiver, facility `daemon` and a distinctive tag. Validate the config.
   Require Healthy status and the received `<30>` startup record within 60 seconds.
3. Run `routerctl log-level debug`, requiring its operation record at the receiver.
   Stop the receiver. Bound `routerctl log-level info` and `routerctl get status`
   individually with an eight-second timeout; require Healthy status.
4. Restart the receiver and require a new debug-operation record within 30 seconds
   without restarting routerd. Restore the default log level, apply empty config,
   stop the daemon, then stop the receiver and remove temporary files.

This package gate covers startup-configured LogSink and receiver loss/recovery.
The non-reading receiver deadline is proved by the local real-TCP test, not this
native gate. Dynamic LogSink destination replacement, remote collector products,
FreeBSD and complete 1.0 parity are not established by this result.

## Live LogSink destination activation and removal

2026-11-06, product `da9e17131359c12409ca85305e5f12f19aeab201`, formal base/addon
`1.5.0~20261106194131+gda9e1713`, passed on a dedicated Ubuntu guest with the normal
systemd daemon and an independent HTTP receiver with two destination paths.
All stages retained the same main PID: first LogSink addition delivered to path
one; dry-run and commit-only candidates for path two retained path one; normal
apply switched to path two; empty apply stopped delivery; re-addition resumed
path one. The receiver counted only the exact log-level operation event and
acknowledged each request after recording its count. Empty withdrawal, service
stop, receiver shutdown and temporary-file cleanup passed.

The local process/API test at `fbbd7a94` reproduced the stale destination after a
successful apply. The fix reconfigures the existing Logger only from a confirmed
active runtime snapshot, joins current deliveries and closes the old sinks.
Local destination replacement/removal and focused apply/commit tests passed.
The full Go suite passed (122 packages, 15 without tests), schemas/examples and
four website locales passed at the product revision.

To repeat: start a normal packaged daemon with empty config and record its PID;
start a bounded independent HTTP receiver that counts `log level override changed`
events separately by path. Prepare two otherwise identical webhook LogSink
configs. Apply the sequence above, calling `routerctl log-level info` after each
step and comparing exact counters and the unchanged PID. Bound each apply to 90
seconds and each log-level request to eight seconds. Dry-run uses `POST
/api/control.routerd.net/v1alpha1/apply` on the Unix socket with an `ApplyRequest`
containing `dryRun: true`; commit-only uses `routerctl apply --no-reconcile`.
Require Healthy empty withdrawal before removing the receiver and stopping the VM.

An initial fixture incorrectly supplied `--dry-run` to `routerctl apply`, which
has no such flag. It stopped before that request reached the daemon. The fixture
was corrected to use the actual control API and the remaining checks resumed
against the existing PID/counters, without restarting or repairing the product.
This result does not establish every sink type, concurrent delivery/activation
failure, FreeBSD, or full routerd 1.0 parity.


## WireGuard live identity rotation and connected health

At product revision `b25b264cdb4822e6e0f3b193c91e6dad7ef8fdeb`, the formal
matching base/addon package `1.5.0~20261106195738+gb25b264c` passed a dedicated
Ubuntu guest test through the normal systemd daemon and configuration API.
An independent kernel WireGuard peer lived in a separate network namespace.
The guest required `wireguard-tools` (a package suggestion) and a loaded
`nf_conntrack` module before the empty-daemon health preflight. This result
therefore does not prove automatic optional dependency installation or module
loading after a clean boot.

Reproduce with an isolated underlay veth pair, a WireGuardInterface with an
explicit listen port, one WireGuardPeer, and owned IPv4 address/route resources:

1. Generate temporary keys without printing them; configure the independent peer
   with its own key, the router public key and matching AllowedIPs/endpoints.
2. Apply the router configuration through `routerctl apply --timeout 90s --replace`.
   Generate bounded traffic in both directions **before** waiting for Healthy;
   an idle peer legitimately has no handshake. Require nonzero handshake times,
   successful packets and no current controller error.
3. Save the daemon PID and native link index/ownership alias. Apply a new router
   private key and update the independent peer's trusted public key. Require new
   bidirectional communication, Healthy status and unchanged PID/index/alias.
4. Restart the normal daemon and require bidirectional communication and Healthy
   status again, without repairing product state.
5. Apply an empty configuration. Require Healthy, absent owned link/route and no
   helper process before stopping the daemon. Remove the independent namespace,
   veth and generated key/configuration files, then stop the disposable guest.

The first exchange exposed a product defect: the peer controller published
`Connected`, but `statusPhaseRank` and `canonicalOverallPhase` omitted this
successful phase. The full daemon reported it as a resource issue despite real
bidirectional traffic. Test commit `70747dea` reproduced the aggregate status and
issue-list error; `b25b264c` fixes both normalization paths. The corrected packaged
run passed all five steps. The existing local real-kernel peer test was extended
in `1f069e18` to verify key rotation, child ownership and cleanup.

Preparation attempts also exposed fixture mistakes: a missing peer tool,
health-before-handshake ordering, and an incomplete temporary-file cleanup that
caused an unnecessary repeat. These were corrected before the final run; no
failed attempt is counted as successful qualification. Full Go tests (122 tested
packages, 15 without tests), schema/example checks and four website locales passed
at the product revision. This establishes local identity rotation and daemon
restart on Linux, not all peer-source integrations, PSK rotation, guest reboot,
FreeBSD or complete routerd 1.0 parity.


## NAT44 source translation through the runtime controller

Product `b318d75b83ee6bbfea3dd310767b465045930c2a` connects the existing
`outboundInterface/sourceCIDRs/translation` configuration to the Linux NAT44
controller. Previously the validator and static renderer accepted this form,
but the runtime controller looked only for the other egress fields and retained
`Pending / EgressPolicyMissing`, generating no translation rule. Regression
`f14655ea` reproduces the missing rule/status through the actual controller.
The fix passes the typed translation to the existing source-NAT renderer.

`sudo ./tests/netns/nat44-source-snat.sh` passed in a dedicated Ubuntu VM using
that controller, real nftables and independent HTTP clients/server in network
namespaces. The validator was the formal distribution payload for the same
revision; a separately compiled test driver invokes the production controller.
The normal routerd daemon stayed stopped. The test verified:

- Existing source/destination selection, destination exclusion and unaffected
  traffic from a different LAN.
- Referenced SNAT address replacement on new connections.
- Translation-shaped address SNAT with the actual TCP source port inside
  `40000-40010`, as observed by the independent HTTP server.
- Empty-config withdrawal, NAT table absence and original source restoration.

Both temporary network namespaces and staged binaries/configurations were
removed, then the guest was stopped. Optional prebuilt test inputs are documented
in the development runbook; test keys or private topology are not needed.
This result qualifies the actual Linux controller/kernel path, not full-daemon
reload/reboot, arbitrary address pools, all port mappings or FreeBSD. In
particular, the existing pool renderer joins address strings with hyphens;
multiple/noncontiguous addresses still require investigation rather than a
blanket pool-support claim.


## Dnsmasq pre-exec startup observation

The full Go suite after the NAT translation change failed in
`TestDnsmasqNoDHCPControllerWithdrawsOwnedProcess` with a native identity mismatch.
A successful isolated rerun was not accepted as resolution. Temporary diagnostics
and a bounded direct-start probe observed the child's PID/birth/namespace with
its parent's test executable and argv before the expected image became visible.
The same observation also reproduced in `TestDnsmasqNativeOwnerLifecycle`.

`waitStartedDnsmasq` previously rejected the first nonempty mismatching process
immediately. It now continues observation within its existing one-second deadline,
acknowledging only the expected executable, arguments and namespace. Child exit
and deadline expiration still fail; persistent mismatches are never adopted.
No new retry loop, extra startup deadline or relaxed process identity was added.
The deterministic launcher regression in `1bfe0900` fails before this change;
its companion test retains rejection of a persistently different executable.
Raw executable/argv diagnostics were removed after investigation.


### Packaged NAT source translation lifecycle

The formal matching base/addon `1.5.0~20261106201835+g176f42fb` subsequently
passed through the normal systemd daemon in a dedicated Ubuntu guest. The
configuration contained two external Interface resources, an explicit runtime
IPv4 forwarding Sysctl and one translation-shaped NAT44Rule. Independent LAN
client and WAN HTTP-server namespaces observed the actual source tuple.

The server observed the configured SNAT address and TCP port range after initial
API apply. Changing the address through the API changed new connections while
the daemon PID stayed unchanged. Restarting the main service retained the desired
translation. Removing only NAT preserved forwarding and restored the original
client source, with the owned nftables table absent. Empty configuration then
returned Healthy; the daemon, HTTP server and namespace fixtures were stopped,
temporary files were removed, the fixture's original forwarding setting was
restored and the guest was stopped. No product-state repair occurred.

This result is pinned to `176f42fb3395d2667bea600dce24d3dba839711e` and extends
the controller-only result above to normal API activation and daemon restart.
It does not qualify arbitrary pools, every mapping mode, guest reboot or FreeBSD.
The earlier conntrack-observer status fix is present in the package, but native
conntrack-source loss/recovery was not injected in this campaign.


### Explicit NAT source address pools

The pool renderer previously sorted addresses and joined them with hyphens.
Two addresses could become a reversed lexical range; three addresses generated
invalid nft syntax, and treating a sparse list as a range could select addresses
not declared by the operator. The native syntax regression in `f421b2fa`
reproduced the rejection. Revision `2ee9f2a2` renders an explicit indexed
`numgen inc` map, retaining only the declared addresses. Single-address pools
remain ordinary address translations. Conntrack retains existing flow mappings.

Reproduce syntax validation with `TestNATPoolNativeSyntax` in an isolated user
and network namespace from the repository root:

```sh
parent_netns=$(readlink /proc/self/ns/net)
ROUTERD_HOST_NETNS="$parent_netns" ROUTERD_RUN_NETNS=1 \
  unshare -Urn go test ./pkg/render -run '^TestNATPoolNativeSyntax$' -count=1 -v
```

Both two- and three-address
cases pass real `nft -c` with auto, preserve and range port mappings. Run
`sudo ./tests/netns/nat44-source-snat.sh` in a disposable Linux guest for the
actual controller/kernel path: six independent HTTP connections must collectively
use all three noncontiguous declared addresses, no other address, and ports
40000–40010. Unmatched traffic and empty-config withdrawal must remain correct.
This command passed with the formal distribution validator and matching
production-controller test driver at `2ee9f2a2`.

The formal matching base/addon `1.5.0~20261106202522+g2ee9f2a2` also passed
through the normal systemd daemon after apt installation in a dedicated Ubuntu
guest. Use two external Interface resources and an explicit forwarding Sysctl,
an independent LAN HTTP client and a WAN server reporting its observed source
tuple. Apply a single-address translation, then replace it with a three-address
noncontiguous pool through the API. Assert all three source addresses and the
port range on six new connections while the daemon PID remains unchanged.
Restart the daemon and repeat the six-connection check. Remove NAT and assert
the original client address and absent owned NAT table. All checks passed.
Empty configuration returned Healthy, the daemon and peer were stopped,
namespace/files were removed, the fixture forwarding value was restored, and
the guest was stopped. No manual product-state repair was needed.

The packet test qualifies TCP range mapping and normal daemon restart. Syntax
validation alone does not qualify preserve/auto packet semantics, every
protocol, guest reboot, FreeBSD or complete routerd 1.0 parity.


### Packaged DerivedEvent to EventRule lifecycle

`TestServeDerivedEventFeedsRuleAcrossApplyAndRestart` starts the actual serve
entry point in separate sandboxed processes with only event owners enabled,
a private control socket and real SQLite files. It applies a DerivedEvent with
a literal configuration predicate and `emitInitial: true`, feeding asserted
and retracted topics into an `any_of` EventRule. It checks both committed output
events and the durable rule fire count. Initial assertion, changed definition,
process restart and a subsequent assertion produce counts 1, 2, 2 and 3.
Run from the repository root:

```sh
go test ./cmd/routerd -run '^TestServeDerivedEventFeedsRuleAcrossApplyAndRestart$' -count=1 -v
```

The same sequence passed in a dedicated Ubuntu VM using the unmodified formal
base/addon `1.5.0~20261106202522+g2ee9f2a2` and its normal systemd daemon.
Prerequisites were an empty canonical configuration, functioning management/API
access and the existing conntrack module required by the guest's observer.
Use a unique output topic per run, and the same two resources as the process
test. Apply enabled then disabled predicate definitions through routerctl;
query `get events --topic <output> --limit 20 -o json` and
`get EventRule/<name> -o json`. Require event count and fireCount 1 then 2,
and an unchanged main PID across apply. Restart the service, require count 2,
then reapply enabled and require count 3. Use bounded 30-second observation
windows and 90-second apply deadlines. On failure, compare current config,
resource status and committed events before retrying; do not inject events into
SQLite or reset the checkpoint to make the test pass.

The packaged test passed without product-state repair. Empty withdrawal returned
Healthy, the service was stopped, temporary configurations were removed and the
VM was stopped. This proves actual DerivedEvent publication, EventRule consumption,
API generation changes and durable restart in a normal daemon. The first source was a supported literal configuration predicate.

A second packaged run replaced that predicate with
`HealthCheck/event-probe.status.phase == Healthy`. The real TCP health helper
probed an independent HTTP server on loopback (one-second interval, 500ms timeout,
both thresholds one); no status or event was injected into the database. Wait
for Healthy/Asserted and consumer catch-up, record the output count, then stop
the server. Require Unhealthy/Retracted and exactly one more output. Restart the
main daemon while the server is down and require the same count. Start the server
and require Healthy/Asserted and one more output. At each stage compare the sum
of the two exact asserted/retracted source-topic counts with output count and
EventRule fireCount; the event query takes exact topics, not wildcard patterns.
Allow a 45-second observation window. Bootstrap can publish an initial retraction
before the first successful probe, so use the settled count rather than assuming
one bootstrap event. Observed counts were 2, 3, 3 and 4. Empty Healthy withdrawal,
helper/server shutdown, temporary-file removal and guest stop all passed.

This additional run qualifies actual health observation loss/recovery through
the event chain. Timed operators, hysteresis, retention pressure and downstream
routing effects remain separate qualification requirements.


### UDP response writer overlap and packaged DNS views

The real UDP regression `b11da908` receives the first response while its server
writer has not yet returned. A second writer sharing the socket previously
failed immediately with `DNS UDP response writer is busy`. Product `7012214a`
serializes waiting and sending within the existing two-second write budget;
it preserves the 128-query admission limit. The regression now receives both
packets, and an occupied writer expires without releasing another writer's slot.
The complete resolver package and full Go/schema/examples/website checks passed.
The older one-off listener timeout has no captured writer error and is not
retroactively attributed to this race.

Formal base/addon `1.5.0~20261106204111+g7012214a` passed apt update and integrity
checks in a dedicated Ubuntu VM. In its normal daemon, two listener views served
independent dig clients over UDP and TCP. A separate TCP health target controlled
the public source: stopping and restarting that target withdrew and restored
the source while the private listener retained its answer and the public listener
never disclosed it. With the target healthy, eight independent client workers
each issued sixteen UDP queries, alternating public and private listeners.
All 128 responses matched their expected address, with two-second client timeouts
and no client retries. Main-daemon restart retained view isolation and another
128 queries passed. The deterministic overlap regression establishes the race;
this bounded concurrent run validates the packaged path, not a throughput limit.

Reproduce with the two-listener/health-conditioned source procedure above,
then run the bounded independent-client batch before and after main restart.
Use the standard empty-withdrawal gate afterward: Healthy, no owned helper or
DNS listener, stopped health peer and removed temporary inputs. These cleanup
checks and VM stop passed. Both prepared full deployment configurations validate
with the same formal binary. This result does not rerun the older integrated HA
power-loss campaign or establish whole-project parity.


### Packaged IngressService drain and backend failure

The packaged IngressService procedure in the development runbook passed with
formal base/addon `1.5.0~20261106204111+g7012214a` in a dedicated Ubuntu VM.
Normal daemon reconciliation programmed native NAT from real backend health.
Independent client/backend namespaces verified initial backend1 selection,
drain to backend2, persisted drain after main restart, undrain to backend1,
backend1 process loss and recovery. The original client used one TCP socket
through drain, daemon restart and undrain, receiving backend1 replies without
reconnect while new flows selected backend2. Empty Healthy withdrawal removed
the NAT table and helpers; peers, namespaces, temporary inputs and forwarding
fixture changes were cleaned up, and the guest was stopped. Existing focused
ingress, drain and NAT controller tests also passed. No product change or manual
product-state repair was required. The runbook states the remaining unverified
selection modes, all-backend outage, address sources and guest-reboot scope.


### Static address-set replacement and UDP response authorization

The independent UDP flow-pinhole test exposed retained authorization after a
static IPAddressSet changed from one client to another. Generated rules contained
only the new address, but the kernel named set retained both: flushing table
rules did not replace existing set elements. Test-first revision `51929746`
reproduced this with real nftables for runtime and full-render paths, in both
IPv4 and IPv6. Product `5645fcf8` resets static address sets in the same ruleset
transaction before declaring desired members. DNS-populated sets retain their
controller-owned learned members; the native regression verifies this boundary.

Formal base/addon `1.5.0~20261106205528+g5645fcf8` passed apt update, package
integrity and version checks in a dedicated Ubuntu guest. Running
`tests/netns/nat44-flow-dnat-pinhole.sh` with the formal validator and a test driver
built from that revision passed independent UDP traffic checks: authorized
responses, rejection by source/destination/interface/port, expiry, withdrawal,
and policy re-add. After changing the authorized source, the new client received
its response and the removed client did not. Negative checks also require the
independent peer to observe the outbound request. Temporary namespaces and
staging were removed. This establishes the production controller and real-kernel
path, not normal-daemon configuration delivery, policy-routing traffic, guest
reboot, FreeBSD or full routerd 1.0 parity. The maintained reproduction and
acceptance procedure is in the development runbook.


### Packaged static address-set redirect lifecycle

Formal product `5645fcf8` passed the packaged address-set redirect lifecycle
procedure in a dedicated Ubuntu guest. The normal service received configurations
through routerctl; independent dnsmasq peers returned distinguishable local and
WAN answers. IPv4 and IPv6 clients using UDP and TCP verified first-address
selection, live replacement with the second address while the main PID remained
unchanged, persistence after service restart, and return to WAN answers after
set/redirect withdrawal. Both selected and excluded destinations had to return
the expected answer; timeout was not a passing exclusion. Empty configuration
reached Healthy without owned NAT tables or helpers. The service and DNS peers
stopped, namespaces/veths/files were removed, forwarding values were restored,
and the VM was stopped. No product-state repair or additional product change
was needed. DNS-populated refresh, policy-routing traffic, guest reboot and
whole-project parity remain outside this result.


### Flow-pinhole baseline contract correction

Baseline `e3b49dbb` and the current `pkg/api/specs.go` both define NAT response
pinhole correlation as `localPort`, while FirewallFlowPinhole provides
`remoteAddress` and `localEndpoint`. The previous NAT remaining-check label
incorrectly assigned the firewall mode to NAT. No NAT feature removal is implied
by its absence. `nftFirewallFlowPinholeLearnKey` and
`nftFirewallFlowPinholeAllowKey` implement the firewall alternatives: the remote
mode includes the remote address, whereas the local endpoint mode keys only the
client address/UDP port. Both retain configured inbound address/port selectors.
Existing renderer tests check these expressions, but do not prove actual peer
traffic. The ledger now keeps that independent firewall packet check separate
from the completed NAT response-pinhole and static redirect campaigns. It also
removes already-qualified basic IngressService drain/failover from the remaining
work description without promoting the whole feature to qualified.


### Packaged FirewallFlowPinhole independent traffic

Formal product `5645fcf8` passed the maintained firewall response-pinhole
procedure in a dedicated Ubuntu guest using the normal service. Independent
client/WAN namespaces exchanged UDP without NAT. Replies used a different
source port from the request destination, avoiding ordinary established-flow
acceptance. `remoteAddress` admitted the original remote address and rejected
the second allowed-range address. `localEndpoint` admitted the second address,
rejected an unlisted response port and rejected a response delayed beyond the
five-second learning timeout. Each negative check also verified peer receipt.

The fixture's 30-second SSH restart observation expired. Systemd timestamps
showed stop/start completed in about 34 seconds, with a configured 90-second
stop deadline, a new active process and Healthy status. No second restart was
issued: the remaining checks resumed from the observed completed restart. A
fresh permitted exchange passed, then pinhole withdrawal with zones retained
blocked a new response. The fixture now allows 120 seconds for the restart
command; the complete campaign was not rerun merely to remove the observation
interruption. Empty withdrawal reached Healthy with no owned filter or helpers.
Peers, namespaces and staging were removed, forwarding restored and the VM
stopped. This establishes the two IPv4 firewall correlation modes and tested
port/expiry/lifecycle paths, not every selector, NAT map, reboot or platform.


### Packaged DNS-populated address-set replacement

Formal product `5645fcf8` passed the DNS-populated variation of the packaged
redirect lifecycle. The dedicated Ubuntu service used a temporary service-only
resolver mount pointing at independent WAN dnsmasq. Changing that peer's A/AAAA
answers and sending it HUP replaced the observed IPv4/IPv6 members without
routerd apply, status editing or a main PID change. Independent UDP/TCP queries
confirmed old destinations returned WAN answers and new destinations local
answers. Main restart preserved the new selection; set/redirect withdrawal
restored WAN answers for all destinations. Empty Healthy withdrawal, no owned
helpers/tables, namespace/peer/file cleanup, runtime drop-in removal/systemd
reload, forwarding restoration and guest stop completed. Focused address-set
controller tests also passed. No product change was needed. DNS outage and
NXDOMAIN/empty-answer behavior remain outside this successful-answer refresh
check; it does not establish TTL-expiry withdrawal or full functional parity.


### Packaged WebConsole live browser

Formal product `5645fcf8` passed the packaged browser procedure through a
loopback SSH tunnel to a dedicated Ubuntu guest's normal service, using existing
Chrome and Playwright core 1.59.1. No API response or SQLite state was mocked.
The initial fixture incorrectly expected only the configured title; source
`webconsole/src/main.tsx` appends the selected page, and the observed title was
`Resources - routerd native UI`. After correcting that fixture, browser checks
resumed against the already configured daemon without repeating VM setup.

The real resource table rendered, POST config returned 405/read-only, CLI add
appeared without page reload, current config included it, and CLI withdrawal
was reflected without reload. Service restart and page reload retained the
remaining resource; no page JavaScript errors occurred. Empty configuration
reached Healthy, removed the listener/helpers, and daemon/tunnel/browser/input
cleanup and guest stop completed. This establishes live packaged behavior, not
every UI page or authorization policy. The controlled stale-response and
unknown-observation tests remain separately scoped.


## Packaged timesyncd dependency and synchronization

The timesyncd-only native run on `5645fcf8` exposed a product defect: generic
NTP dependency derivation installed chrony, causing APT to remove the configured
timesyncd provider. `TestNTPPackagesFollowManagedProvider` reproduced the wrong
Ubuntu/Debian package plans in test-first commit `6798dfd8`. Product `59373f17`
derives dependencies from managed providers, including shared-chrony ownership,
defaults and unmanaged declarations; all hostdeps tests then passed.

After restoring and certifying the fixture, formal base/addon package
`1.5.0~20261106212625+g59373f17` passed the maintained timesyncd-only procedure.
The normal daemon kept timesyncd installed, synchronized to each independent
reference address, changed source with the same main PID, and retained the new
source after service restart. Each observation included normal leap, stratum 8
and positive packet count. Empty intent was Healthy and removed the owned
fragment; original chrony was restored, fixture resources removed and VM stopped.
Both private deployment candidates validated with the same formal binary.

`go test ./...`, schema checks, example validation and website build passed
(exit 0); website still emits existing translation-link warnings. This result
does not qualify native DHCP/per-interface/fallback transitions, arbitrary
provider switching, external UTC precision, FreeBSD service behavior or a new
full combined-router run. The local dynamic-source loss/fallback/return test
`92eeaa5a` passes with a fake OS service executor and is not native evidence.


## Packaged DHCP lease DNS lifecycle

Formal product `59373f17` on a dedicated Ubuntu guest passed the maintained
DHCP lease to DNS procedure: independent busybox DHCP acquisition, exact A/PTR
responses over UDP/TCP, main service restart retaining the name, and independent
DHCP RELEASE followed by NXDOMAIN/zero-answer A/PTR over both transports.
No direct lease event injection, SQLite repair or resolver restart was used.
Empty intent was Healthy with no current errors and all helpers removed; client,
namespace/veth and staging were cleaned. Local DNS worker/controller and daemonapi
tests also passed. Natural expiry, DHCPv6 naming and HA are not qualified here.


## Packaged IngressService selection and complete outage

Formal product `59373f17` passed the normal-daemon distribution/outage procedure
on a dedicated Ubuntu guest. For 17 source addresses and four new connections
each, sourceHash kept each source on one backend and reached both independent
servers. Random selection also returned only valid backend-tagged payloads and
reached both servers. With both servers stopped, zero healthy backends converged
and a new connection was actively refused. Backend2-only recovery delivered real
payloads; both-server recovery passed the sourceHash sample again. Empty intent
was Healthy, helpers/NAT absent, peer namespaces/veths/staging removed and original
forwarding restored. Focused ingress controller/renderer tests passed. No product
repair was needed. This does not qualify dynamic address sources, other outage
policies, UDP or affinity across ruleset replacement.


## Packaged IPv6 preferred-source recovery

Product `59373f17` failed source-return recovery: Linux retained the policy
route while clearing its preferred source, and exact observation discarded the
receipt during withdrawal. Return then collided with the orphan route. An
isolated real-kernel reproduction and test-first `cdc1f4e1` established the
failure. Fix `0cf9566b` recognizes the otherwise identical cleared-source route
only through its recorded identity; unowned and pending acquisitions stay fenced.
The real-kernel controller regression passes. A second reproduced failure,
`d0d68488`, left Pending status after successful removal. Fix `e6590007` retires
removed policy status only after effect reconciliation, preserving declarations,
dry-run status and failed withdrawals.

Formal base/addon `1.5.0~20261106215319+ge6590007` passed installed checksum/version
verification and automatically cleared the old Pending status to empty Healthy.
A fresh dedicated-guest run then passed real IPv6 host HTTP through the policy,
source-loss rule/route withdrawal and failed traffic, source return with the same
main PID, service restart with restored HTTP, and empty Healthy with no stale
policy status. Both private 105-resource deployment candidates validated with
this formal binary. This does not qualify transit/hash policy or all WAN modes.

The original fixture used bare shell negation, which is insufficient with
`set -e`; the rerun used explicit failure branches. Final namespace deletion
completed but the immediate veth absence check raced kernel teardown. Subsequent
read-only observation proved link absence; cleanup resumed without repeating
product mutations, verified policy absence and failed HTTP, and removed staging.
The maintained procedure now bounds the veth teardown wait to five seconds.
Do not describe the initial script exit as a complete uninterrupted pass.


## Namespace negative-assertion repair

The VXLAN L2 MSS and VRRP VMAC scripts contained standalone `! command` checks
whose unexpected success did not abort under `set -e`. These now have explicit
failure branches; prior success alone did not prove those negative assertions.
Shell syntax, ShellCheck warning-level checks and diff checks pass.

The corrected VRRP gate passed with formal `e6590007` helper: cold backup and
master transitions preserve delegated globals, enforce link-local absence in
backup, and remove the deferred IPv4 VIP. The corrected VXLAN gate passed with
the current-source test driver through the production PathMTU controller:
invalid nft batch rollback, collision rejection, staged/active crash recovery,
MSS A/B packet observations for IPv4/IPv6, non-TCP transparency, drift repair,
proof-replay protection, failed-list preservation and exact withdrawal. Cleanup
completed. Product code was unchanged.

The first VXLAN attempt in a user namespace failed before controller execution
because the driver could not read the host namespace identity; a direct driver
invocation reproduced the guard rejection. A root-owned private mount/network
namespace with the same prebuilt test driver then completed. This is local
real-kernel/controller evidence, not a new packaged whole-router campaign.

## Linux overlapping VRF peer isolation

The `TestVRFNativeLinuxOverlappingPeerIsolation` addition on the `12656124`
product tree passed in 6.59 seconds using a compiled controller test harness
under `sudo ... unshare -nm`, with a private mount tree and fresh sysfs mount.
Both peers used independent network namespaces on the same Linux kernel; the
actual VRF/address controllers and SQLite store owned the router-side effects.
Two identical destination addresses returned distinct expected UDP payloads.
Each peer's unique address was reachable only through its own VRF. The checks
passed after SQLite reopen and the remaining VRF continued communicating after
withdrawal of the other. Final withdrawal removed owned addresses, links and
receipts. Peer processes were terminated and reaped on test cleanup.

No product fix was needed. The reusable procedure is in
[development](../operations/development.md#linux-overlapping-vrf-isolation).
This adds host-originated overlapping-VRF isolation coverage; it does not prove
transit forwarding, packaged-daemon restart/reboot, delegated IPv6/RA or FreeBSD.

## Packaged Tailscale advertisement withdrawal and exit-node traffic

Product `5feec40ca72529341da6501929894226f0af7fb0`, formal matching base/addon
`1.5.0~20261106221256+g5feec40c`, passed the following scoped checks on two
disposable Ubuntu guests with Tailscale 1.104.5 and independent Headscale 0.28.0:

- Normal-daemon apply and exact independent HTTP payloads through an advertised
  IPv4 subnet and an exit-node destination outside that subnet.
- Removing both advertisements through YAML leaves the node Running/Healthy,
  clears daemon and control-server available routes, removes the client subnet
  route and makes the old subnet payload unavailable.
- Re-advertisement restores both paths; normal routerd restart preserves working
  exit-node traffic without product-state repair or reauthentication.
- Empty configuration becomes Healthy, withdraws the managed unit/route and
  disconnects the node. Fixture services, namespace, veth and current staging
  files were removed, and forwarding settings restored.

The original renderer omitted flags when settings returned to defaults. Actual
Tailscale 1.90.6 CLI validation against a private LocalAPI fixture reproduced
rejection of the change before any preference patch. `--reset` fixes the complete
declared-configuration path; the regression now observes an explicit empty
advertisement patch. No logout or forced reauthentication was added.

The native script was resumed, not an uninterrupted pass: its empty-count
preflight initially mishandled an omitted zero field; after certification, its
Headscale empty-route check raised KeyError for an omitted field. Actual daemon
and provider routes were already empty. Correcting that fixture predicate
confirmed withdrawal. A resume-only missing-variable error occurred after
successful re-advertisement; reading the current node address allowed restart
and cleanup checks to complete. No product repair was used for these resumes.
The full Go gate subsequently passed after regenerating two shared FreeBSD CLI
golden outputs, followed by schema/example checks and all website locales.

[The maintained procedure](../operations/development.md#tailscale-advertisement-removal-and-recovery)
requires both positive payloads and explicit withdrawal checks. This result does
not qualify IPv6 exit forwarding, credential expiry, control-server outage
recovery, production deployment or full baseline parity.

## Tailscale expired credentials and truthful runtime status

Product `db41e03eae9648dc6d068892b3ec361d8a5696c1`, matching formal base/addon
`1.5.0~20261106222627+gdb41e03e`, passed a fresh uninterrupted isolated Linux
run with Tailscale 1.104.5 and independent Headscale 0.28.0. Starting from an
empty Healthy daemon and certified HTTP peer, the normal configuration API
activated subnet traffic. Expiring the test node at the provider caused actual
NeedsLogin and payload failure. Without apply or restart, the controller
reported Pending/AuthenticationRequired, overall status was not Healthy and the
main PID stayed unchanged. Declaring a fresh single-use credential restored
Running/Healthy, removed the stale error/reason and restored the HTTP payload.
Empty withdrawal, owned route/unit removal, fixture cleanup, forwarding
restoration and credential-file removal passed.

The earlier controller regression reproduced Applied for NeedsLogin, Starting
and failed/malformed runtime observations. The fix publishes meaningful Pending
or Error states; recovery clears them. Running with control connectivity absent
remains Running because that observation alone does not prove forwarding loss.
Local tests cover this distinction and machine-approval state; native expiry
does not qualify every observation failure or control-server outage. The
[reusable procedure](../operations/development.md#tailscale-authentication-expiry)
keeps the existing reconciliation interval and uses no product-state repair.

## Tailscale dual-stack exit and independent control recovery

Formal product `db41e03eae9648dc6d068892b3ec361d8a5696c1` passed a fresh
uninterrupted two-guest run after correcting the control-path fixture. The
independent client retrieved exact HTTP payloads over IPv4 and IPv6 exit paths,
including an IPv6 destination outside the connected control LAN and an observed
`tailscale0` route. With Headscale stopped and HTTPS health unreachable from
both guests, 24 new TCP requests (12 per family) succeeded. The router main PID
and Running backend remained unchanged. Recreating the same control server
restored both provider-observed online nodes and payloads without reauthentication.
Normal routerd restart retained IPv6 traffic; empty Healthy withdrawal removed
both destination routes and disconnected the node. Fixture processes, network
namespace, veth and staging were removed and forwarding settings restored.

The earlier run did not qualify recovery. First its restart command addressed
a collected transient unit. After recreating the server, packet capture showed
LAN SYNs with SYN-ACKs sent through the peer's selected exit node instead of the
control LAN. A local isolated route-table reproduction confirmed that selection.
The failed run was fully withdrawn before recertification. The corrected fixture
retains its connected control LAN using the independent client's LAN-access
option and verifies a new control connection after exit selection. No product
change or product-state repair was needed. See the
[reproduction criteria](../operations/development.md#dual-stack-exit-traffic-and-control-server-outage).

This proves bounded control-server outage/recovery with established peer keys,
not full platform parity or unlimited disconnected operation.


## Packaged Collector outage and recovery — db41e03e

The existing 261e6675 combined gate already used independent Collector contrib;
its successful receiver interoperability is not an unexecuted check. This gate
addresses its explicit outage/recovery exclusion with the current formal product,
without claiming to rerun the combined 78/105-resource topology.

On Ubuntu 24.04 / Linux 6.8.0-147, matching package
`1.5.0~20261106222627+gdb41e03e` ran the normal main daemon and managed DNS
resolver. A separate VM ran Collector contrib 0.151.0 and the dig client.
Certification established package integrity, empty Healthy state, validated
collector/listener and actual cross-VM reachability before candidate apply.

The uninterrupted gate passed initial main-daemon logs/metrics/traces, actual
collector stop and connection refusal, declarative DNS A-record replacement,
20 UDP plus 20 TCP exact answers with unchanged MainPID, and Healthy status.
A normal daemon restart with the collector still unavailable restored DNS and
Healthy in 14.41 seconds. Restoring the same collector and changing DNS again
produced all three signals without another daemon restart (received counts:
logs 660, metric records 2, spans 660). Counts identify delivery, not a lossless
transport guarantee. Empty Healthy withdrawal removed the actual DNS listener;
package verification and temporary collector/output/address cleanup passed.
The dedicated guests were stopped; no product-state repair was required.

`go test ./pkg/otel -count=1` also passed. No product code changed in this gate.
The maintained EN/JA observability procedure specifies input identity, peer
setup, bounded waits/counts and teardown. Long outages, buffer exhaustion,
TLS/mTLS, durable remote storage, complete attribution and the full deployed
configuration remain unqualified by this scoped test.

## Current-package combined gate preparation — db41e03e

The current formal packages were installed on both dedicated Linux routers.
The first attempt stopped when an old fixture read status only once across a
managed self-restart. Both empty withdrawals passed. The harness now polls that
socket replacement and reports the actual known retention/standby observations
instead of demanding an unjustified all-Healthy aggregate. The existing explicit
retention-policy regression passed locally.

The corrected 107-resource core lifecycle completed in 529.1 seconds: both DHCP
reservations, conditional DNS/DoH, management admission, DNS/NTP replies, native
TLS classification, telemetry, natural PD renewal, both-direction power loss,
cold return, latest lease synchronization and active-main restarts. All 74
original application flows resumed with unchanged tuples and single TCP accepts;
the largest response gap was 53.853 seconds. Both returning-node captures had
no observed shared MAC/IP/ARP claims.

This is **not a completed latest combined qualification**: a subsequent static
route payload check could not create its peer script because the peer root
filesystem was full. The initial capacity certification was insufficient. The
receiver had roughly 23 MiB of current JSON output on the full root filesystem;
the collector executable was already on tmpfs. Initial headroom was not
measured, so the first space-consuming write is not established. Subsequent
inspection found approximately 121 MiB of repeated rsyslog disk-full errors
(over 215,000 repetitions each of two messages). After recording those reasons
and counts, the disposable peer's redundant syslog stream was cleared and its
logger restarted during certification. Both product withdrawals
then passed with empty Healthy, and the temporary peer fixture was removed.
The revised receiver writes its three bounded output files to the existing
512 MiB tmpfs and checks at least 100 MiB of root headroom before setup. Only
rebuildable APT binary index caches were removed to restore that headroom.
A new certification and uninterrupted final run are required before claiming
this complete gate passed; none of these observations establish a product fix.

## Completed packaged combined lifecycle — 0e186cc0

On 2026-11-07, two dedicated Linux routers with matching formal base/native
nDPI packages `1.5.0~20261107004653+g0e186cc0` passed the complete isolated
107-resource campaign against a separate peer VM. The input preserved the
deployed 105-resource kind combination and added two fixture resources.
Package identity, empty Healthy, peer SSH and authenticated TLS were certified
before applying the configuration. No repair was made during qualification.

Initial and final DNS/NTP/TLS classification, both DHCP reservations,
conditional DNS/DoH, management allow/403, and both main daemons' telemetry
passed. Natural same-prefix PD renewal kept all four tunnel indices. Both
directions of power loss, cold return, actual lease/PD synchronization and
active main-daemon restart preserved the same 74 application flows, original
translated tuples and single TCP accepts. Returning-node captures found no
competing shared MAC/IP/ARP claims. The core lifecycle took 506.2 seconds.

The subsequent three static-route TCP payload checks, Tailscale subnet TCP
connections to both routers, fresh main-daemon logs/metrics/traces and final
74-flow check also passed. Both empty withdrawals reached Healthy and removed
owned helpers, VMACs, learned routes and provider intent. Independent peer
fixtures were cleaned up. The largest response gap was **53.867 seconds**;
this proves connection preservation, not hitless or fast failover.

The revision includes locally reproduced fixes for a transient Pending address
publication after renewal confirmation and zone-refresh cancellation of
unchanged DNS forwarding queries. An earlier campaign failed its final DNS
assertion without retaining the raw response; this successful run does not
retroactively establish that incident's cause. Failure diagnostics now retain
the full DNS response without changing its timeout, attempt count or answer
criteria. Full Go, schema, example and four-locale website checks passed before
packaging; existing website link warnings remain.

This qualifies the stated Linux combination. Production cutover, physical
uplinks, all failure modes, all 1.0 functions and deferred platforms remain
separate requirements. Use the maintained
[HA procedure](../operations/linux-ha-pair-qualification.md); raw temporary
campaign logs are not a prerequisite for future regression tests.

## Linux overlapping VRF client transit

On the `b4fc85f3` tree (product `0e186cc0`), the extended
`TestVRFNativeLinuxOverlappingPeerIsolation` passed in 11.98 seconds without
skips. A compiled controller test ran under `sudo ... unshare -nm` with private
sysfs, using independent server and client namespaces for each VRF. Both
domains had identical router/server addresses and identical client addresses.
IPv4 forwarding and peer return routes were fixture setup confined to these
namespaces. The actual VRF/address controllers owned both router interfaces.

Correct distinct UDP payloads, unique peer reachability and cross-domain denial
passed for router-originated and client-transit traffic. Both paths passed again
after SQLite reopen; red remained reachable after blue withdrawal. Final
withdrawal removed owned addresses, VRFs and receipts; peer processes were
terminated and reaped. No product fix was needed. The maintained
[procedure](../operations/development.md#linux-overlapping-vrf-isolation) now
requires both paths. This qualifies IPv4 UDP transit for this topology, not
packaged-daemon restart/reboot, delegated IPv6/RA, every routing mode or FreeBSD.

Publication `eb43e539` contains the extended test and procedure; `c54d71a9`
updates the plan. Full `go test ./...`, `make check-schema validate-example
check-website-schemas website-build`, formatting and `git diff --check` passed.
The four-locale website build retained documentation-link warnings. Secrets
scans passed and both checkpoint remote SHAs were verified.

## Linux VRF delegated RA peer

The added `TestVRFNativeLinuxDelegatedRA` passed without skips in 5.31 seconds
on tree `3743818e` (product `0e186cc0`). The compiled controller harness ran
under explicit `sudo ... unshare -nm` with private sysfs. A finite PD lease was
fixture input in SQLite; actual VRF, LAN address and RA controllers configured
the member and sent advertisements. An independent Linux namespace acquired
a SLAAC address and RA default route, then successfully pinged the router's
delegated address. Empty withdrawal removed the owned router address, client
RA default route and journal; the peer process was killed and reaped. No product
fix was needed. The maintained procedure and native-parent Make target include
the test. DHCPv6 exchange, IPv6 transit, renumbering, packaged-daemon restart and
FreeBSD remain outside this result.

Test/procedure publication `f28e58fd` was secrets-scanned, pushed and remote
SHA verified. Full `go test ./...`, schema checks, example validation and
four-locale website build passed; existing documentation-link warnings remain.

## Linux VRF IPv6 transit, renumbering and RA source DAD

The extended native gate `TestVRFNativeLinuxDelegatedRA` in `9002a212`
added an independent upstream namespace, IPv6 forwarding and old/new return
routes. It requires client traffic through the VRF, selection of the new SLAAC
source after changing the finite stored PD lease, old-address deprecation,
and complete owned-prefix/default-route/journal withdrawal. PD exchange and
upstream address/routing setup remain fixture inputs.

An initial RA send failed with `sendmsg: invalid argument`. Repeated isolated
runs captured the failing interface: the delegated global address had finished
DAD while the link-local transmit source remained `tentative`. `Listen` selected
that address through `net.Interface.Addrs`, which omits DAD flags. The deterministic
real-kernel test in `5b4fab1f` held link-local DAD pending and failed because
`Listen` admitted that source (0.03 seconds). Successful intervening runs did
not close this failure.

Fix `c7f3eb83` uses Linux native address flags to reject tentative/failed DAD
sources. Tentative readiness publishes `LinkLocalDADPending`; existing controller
reconciliation resumes sending once usable. Required observation errors and
withdrawal without a usable source remain errors. The kernel test now passes
both pending refusal and usable-source admission. The service pending/resume
regression passed. Three isolated transit/renumbering runs on the initial fix
passed in 14.45, 14.71 and 14.29 seconds. No source-readiness retry loop or
manual product repair was added.

This is controller/kernel qualification, not packaged-daemon startup latency:
the test invokes reconciliation directly, whereas the existing owner has
status-event reconciliation and a 30-second periodic check. Formal package
installation, restart/reboot and DHCPv6 exchange in this VRF topology remain
unqualified. The earlier `0e186cc0` full combined-package result retains its
original revision and is not relabeled as evidence for this new runtime fix.

The final published `c7f3eb836d607c8802861ea2efb09580feeba96f` tree was
recompiled and the isolated IPv6 transit/renumbering gate passed in 14.18
seconds without skips. The source-readiness kernel test passed in 0.03 seconds;
the pending/resume focused suite passed in 4.053 seconds.

Full `go test ./...`, `make check-schema validate-example check-website-schemas
website-build`, formatting and diff checks passed on the fix. The four-locale
website retains existing documentation-link warnings. The routeradvertisement
package also cross-compiled for FreeBSD with CGO disabled; no FreeBSD runtime
qualification is claimed. Secrets scans passed and the product remote SHA was
verified.

## Packaged VRF attempt and removed-status recovery

Formal matching base/addon `1.5.0~20261107013516+g143eee96c7dc` were built
and apt-updated on an existing isolated Ubuntu 24.04 router VM. A separate peer
VM hosted Kea 2.4.1 and independent client/server namespaces. The candidate had
11 resources: VRF members, IPv4 addresses, forwarding sysctls, DHCPv6 PD,
delegated LAN/WAN addresses and RA. Fixture setup corrected Kea's validation
namespace and runtime PID/data directories before qualification.

The first apply failed creating VRF with `operation not supported`. The guest
had `CONFIG_NET_VRF=m` but no module file; `modinfo vrf` failed. This was a
missed environment prerequisite, not evidence of a broken netlink command. The
run ended and routerd stopped. During fresh environment preparation, apt
installed the exact running kernel's `linux-modules-extra-6.8.0-147-generic`;
`modprobe vrf` then succeeded. Certification now checks module availability.
No successful restart/reboot qualification is claimed for that attempt.

Normal API withdrawal exposed a product defect: the native VRF was absent, but
`VRF/tenant` retained `Absent/OwnedGenerationWithdrawn`, making the empty router's
health Absent. The two SQLite/controller regressions in `4ec4a69d` reproduced
immediate removal and recovery of a previously withdrawn row. Fix `b181a6c1`
retires only terminal rows absent from both declared configuration and native
ownership. Dry run, retained inactive declarations and ownership receipts are
preserved. The focused VRF suite passed; the actual-kernel overlapping-domain
forwarding/reopen/withdrawal test additionally required both deleted VRF status
rows to disappear and passed in 12.10 seconds.

Fixture networks, units and staging were removed; both used VMs stopped. The
empty canonical configuration and stale terminal row were retained in the
router's database for normal-runtime recovery verification by the new package.
The packaged lifecycle verdict remains incomplete until that follow-up.

For `b181a6c1`, the complete Go suite, schema checks, example validation, website
schema checks and all four website locales passed (existing link warnings).
Formal matching base/addon `1.5.0~20261107015013+gb181a6c1` built successfully.
Packaged VRF lifecycle verification remains pending.

### Packaged VRF follow-up: cold-boot recovery remains broken

The matching `b181a6c1` packages were apt-installed and verified on the isolated
Ubuntu router. Starting with the retained withdrawn status automatically restored
empty Healthy. After environment certification, all 11 resources were Healthy
with no controller errors; independent client IPv4 and IPv6 transit passed at
13.6 seconds. Normal daemon restart passed the same checks at 24.5 seconds.

Router VM power loss and cold boot failed: the VRF controller repeatedly returned
`VRF ownership belongs to another kernel scope`. `VRFController.Reconcile` checks
old receipts before observing absence or considering reboot recovery; the saved
scope includes the old boot ID. Top-level health still reported Healthy from
saved resource rows, but the gate correctly rejected the controller error. This
is not a successful cold-boot qualification. The daemon and independent Kea were
stopped, and existing guests powered off with failure state retained for a local
reproduction and corrected-package recovery check. No database editing or manual
VRF recreation was used to claim recovery.

Environment preparation also exposed two procedure problems: deleted fixture
files returned after immediate hard poweroff, and Kea started during interface
DAD with no listening socket. Preparation/cleanup now flush filesystem changes;
Kea startup waits for a usable link-local address and certification requires its
actual UDP listener. These changes precede product qualification.
