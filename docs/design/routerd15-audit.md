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

| Directory | Production files | Semantic review |
| --- | ---: | --- |
| `cmd/routerctl` | 26 | In progress |
| `cmd/routerd` | 29 | In progress |
| `cmd/routerd-arp-observer` | 4 | Pending |
| `cmd/routerd-bgp` | 1 | In progress |
| `cmd/routerd-dhcp-event-relay` | 1 | Pending |
| `cmd/routerd-dhcp-fingerprint-watcher` | 1 | Pending |
| `cmd/routerd-dhcpv4-client` | 4 | In progress |
| `cmd/routerd-dhcpv6-client` | 3 | In progress |
| `cmd/routerd-dns-resolver` | 9 | In progress |
| `cmd/routerd-dpi-classifier` | 1 | Pending |
| `cmd/routerd-eventd` | 1 | Pending |
| `cmd/routerd-firewall-logger` | 3 | Pending |
| `cmd/routerd-healthcheck` | 1 | In progress |
| `cmd/routerd-ndpi-agent` | 4 | Pending |
| `cmd/routerd-pppoe-client` | 4 | Pending |
| `cmd/routerd-ra-observer` | 4 | In progress |
| `cmd/routerd-schema` | 1 | Pending |
| `cmd/routerd-vrrp-vmac` | 1 | Pending |
| `internal/cmd/specgen` | 1 | Pending |
| `internal/freebsdpcap` | 1 | Pending |
| `internal/hostcmd` | 1 | Pending |
| `internal/mapsort` | 1 | Pending |
| `internal/statusvalue` | 1 | Pending |
| `internal/stringutil` | 1 | Pending |
| `pkg/api` | 13 | In progress |
| `pkg/apply` | 5 | Pending |
| `pkg/bgp` | 1 | Pending |
| `pkg/bgpdaemon` | 2 | In progress |
| `pkg/bus` | 1 | Pending |
| `pkg/config` | 22 | Pending |
| `pkg/conntrack` | 1 | Pending |
| `pkg/conntracktuning` | 1 | Pending |
| `pkg/controlapi` | 5 | Pending |
| `pkg/controller/bfd` | 1 | Pending |
| `pkg/controller/bgp` | 7 | In progress |
| `pkg/controller/chain` | 34 | In progress |
| `pkg/controller/conntrackobserver` | 1 | Pending |
| `pkg/controller/dhcpv4client` | 1 | In progress |
| `pkg/controller/dnsresolver` | 1 | In progress |
| `pkg/controller/eventfederation` | 1 | Pending |
| `pkg/controller/eventsubscription` | 1 | Pending |
| `pkg/controller/firewall` | 1 | In progress |
| `pkg/controller/framework` | 1 | In progress |
| `pkg/controller/ingressservice` | 1 | In progress |
| `pkg/controller/mobility` | 37 | Pending |
| `pkg/controller/mobilityfib` | 1 | Pending |
| `pkg/controller/nat44` | 1 | In progress |
| `pkg/controller/pppoesession` | 1 | Pending |
| `pkg/controller/provideraction` | 1 | Pending |
| `pkg/controller/vrrp` | 2 | In progress |
| `pkg/daemonapi` | 2 | Pending |
| `pkg/derived` | 1 | In progress |
| `pkg/dhcpeventqueue` | 3 | Pending |
| `pkg/dhcpfingerprint` | 1 | Pending |
| `pkg/dhcpv4client` | 2 | In progress |
| `pkg/dnsresolver` | 1 | In progress |
| `pkg/dpi` | 2 | Pending |
| `pkg/dynamicconfig` | 2 | In progress |
| `pkg/dynamicconfig/codec` | 2 | Pending |
| `pkg/egressroute` | 1 | In progress |
| `pkg/eventconsumer` | 1 | Pending |
| `pkg/eventd` | 7 | Pending |
| `pkg/eventfile` | 1 | Pending |
| `pkg/eventlog` | 2 | Pending |
| `pkg/eventrule` | 1 | Pending |
| `pkg/federation` | 2 | Pending |
| `pkg/federationguard` | 1 | Pending |
| `pkg/firewallbackend` | 1 | In progress |
| `pkg/ha` | 1 | Pending |
| `pkg/healthcheck` | 10 | In progress |
| `pkg/hostdeps` | 1 | Pending |
| `pkg/hybrid` | 1 | Pending |
| `pkg/ingressdrain` | 1 | In progress |
| `pkg/inventory` | 1 | Pending |
| `pkg/ipsec` | 1 | Pending |
| `pkg/lan` | 1 | Pending |
| `pkg/lan/dhcpv4server` | 1 | Pending |
| `pkg/lifecycle` | 3 | In progress |
| `pkg/lock` | 2 | Pending |
| `pkg/logstore` | 7 | Pending |
| `pkg/manageddaemon` | 1 | Pending |
| `pkg/mobilityconfig` | 4 | Pending |
| `pkg/netconfigbackend` | 1 | Pending |
| `pkg/nflog` | 2 | Pending |
| `pkg/nftstate` | 1 | In progress |
| `pkg/observabilitypipeline` | 1 | Pending |
| `pkg/observe` | 2 | Pending |
| `pkg/otel` | 3 | Pending |
| `pkg/pdclient` | 4 | In progress |
| `pkg/platform` | 5 | Pending |
| `pkg/plugin` | 6 | In progress |
| `pkg/pppoeclient` | 1 | Pending |
| `pkg/provideraction` | 4 | Pending |
| `pkg/providercontract` | 1 | Pending |
| `pkg/providerinventory` | 2 | Pending |
| `pkg/raobserver` | 1 | In progress |
| `pkg/render` | 32 | In progress |
| `pkg/resource` | 3 | In progress |
| `pkg/resourcequery` | 2 | In progress |
| `pkg/sam` | 1 | Pending |
| `pkg/samenrollment` | 1 | Pending |
| `pkg/servicemgr` | 1 | Pending |
| `pkg/source/daemon` | 1 | Pending |
| `pkg/state` | 12 | In progress |
| `pkg/status` | 2 | Pending |
| `pkg/subprocessio` | 1 | Pending |
| `pkg/sysctlprofile` | 2 | Pending |
| `pkg/tailscale` | 1 | Pending |
| `pkg/version` | 1 | Pending |
| `pkg/vrf` | 1 | Pending |
| `pkg/vxlan` | 1 | Pending |
| `pkg/webconsole` | 25 | In progress |
| `pkg/wireguard` | 1 | Pending |

## Other surfaces

- Web console source and generated assets: refresh, current-config snapshots and
  diff paths reviewed and fixed; remaining UI/enrichment review is pending.
- Shell/Python test and lab tools: pending semantic review.
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

### Remaining implementation and audit work

These remain source findings, separate from missing VM evidence:

- Mobility snapshot collection can publish handover-release ownership events
  before the old local BGP withdrawal succeeds. Move the release decision into
  the typed plan and publish only after the responsible withdrawal, preserving
  failure/restart/federation semantics. This path has not been rewritten here.
- Static SAM/dynamic preparation still redecodes, clones and validates parts of
  the configuration on observation ticks. The once-per-configuration rule is not
  yet satisfied throughout the chain.
- PD state still has internal encoded-string transfers and duplicate projections
  in generic current status. The new protocol snapshot does not resolve every
  controller/store representation.
- Most mobility bodies, event/action/federation paths, CLI commands, UI enrichment,
  scripts and operator documentation have only been inventoried or partially
  read. Neighbor/VPN/log query cancellation also needs further work.

Do not describe this branch as fully rewritten, fully audited, or fully compliant
with AGENTS.md. Keep the complete test gate and independent-peer/target-OS gates.
This environment denies AF_UNIX and raw socket operations, and has no established
PVE access. Chromium download returned HTML rather than an archive, so the added
browser regression has not run. Cross-compilation is not target-OS execution.
