# Cloud SAM rearchitecture goal

## Authority and execution rule

This file is the implementation goal and acceptance contract for the Cloud SAM
rearchitecture requested by the user. It supersedes the earlier small
controller-refactor plan. Do not use cloud infrastructure until every listed
implementation change and local verification are complete. Cloud is a final
integration gate, never a substitute for completing the migration.

## Diagnosis to preserve

The problem at the baseline was not an excessive number of files. Cloud SAM
represented the same facts repeatedly and let later controllers reinterpret
them:

```text
provider inventory / federation event
  -> ownership facts -> ownership decision -> MobilityPool status map
  -> mobilityfib verdict -> synthetic IPv4Route or RemoteAddressClaim
  -> sam.CaptureAction -> OS operation -> RemoteAddressClaim status
```

At the baseline, the BGP Cloud SAM control plane coexisted with the legacy SAM
data plane, and BGP pools were unnecessarily converted back into the legacy
`RemoteAddressClaim` model. The largest redundant path was:

```text
BGP/status -> synthetic RemoteAddressClaim -> CaptureAction -> OS apply
```

Do not remove necessary safety complexity: BGP/provider convergence separation,
startup fencing, holder retention/no-preempt, hold-downs, provider action
fencing and idempotency/journal recovery, split-brain handling, OS-specific
Linux/FreeBSD behavior, and fail-static peer-group synchronization remain
required behavior.

## Implemented component boundaries

- `TransportController`: derive tunnels, endpoint routes, BFD, and BGP peers
  from `SAMTransportProfile`; do not merge it into mobility reconciliation.
- `DiscoveryController`: provider and on-prem ownership observations, plus the
  independent typed ARP-observer bootstrap intent.
- `mobility.Controller`: typed snapshot collection, PoolPlan production, BGP
  and provider-plan effects, status projection, and transition events.  Its
  shell is deliberately limited to orchestration; address policy is in the
  typed planning functions.
- `ownership_resolver`: per-/32 ownership and FIB semantics.
- `bgp_delivery_planner`: BGP advertisements, return paths, and capture
  candidates.
- `mobilityfib`: applies typed `FIBVerdict` values from active dynamic config.
- `provideraction`: independently and safely applies ActionPlans; preserve it
  as the provider mutation boundary.
- `chain/dynamic_effective`: the sole active-plan decoder for
  `MobilityDataplanePlan`; it does not reopen MobilityPool status, BGP RIB, or
  raw Pool configuration and does not create BGP synthetic claims.
- `chain/mobility_dataplane`: applies the typed route/static-address portions
  of that plan directly, with applied-effect ledgers only for safe withdrawal.
- `pkg/sam` and `chain/SAMController`: lower the already-decided capture
  portion and apply the Linux/FreeBSD dataplane behavior.

The runtime is event-driven plus periodic reconcile; do not assume a single
strictly serial controller pipeline.

## Target architecture

Adopt a functional core with an imperative shell:

```go
type PoolRuntimeSnapshot struct {
    Pool                  NormalizedMobilityPool
    Events                []EventRecord
    Ownership             OwnershipFacts
    BGP                   BGPSnapshot
    PlacementObservations PlacementObservations
    Previous              PreviousPoolState
    Provider              ProviderSnapshot
    LivenessMarkerPrefix  string
    TunnelInterfaces      []string
    CaptureGate           *CaptureGateStatus
    Now                   time.Time
}

type PoolPlan struct {
    Placement       PlacementDecision
    Addresses       []AddressDecision
    BGPPaths        []bgpdaemon.AppliedPath
    ProviderActions []dynamicconfig.ActionPlan
    LocalDataplane  dynamicconfig.MobilityDataplanePlan
    FIBVerdicts     []dynamicconfig.FIBVerdict
}
```

`MobilityPoolStatus` is not a `PoolPlan` field. It is projected at the store
boundary from the plan and observed effects, so status cannot flow back into
the pure planning path.

The single direction is:

```text
normalized Spec + facts + previous state
  -> PoolRuntimeSnapshot
  -> pure Reconcile/PoolPlan
  -> BGP effector | provider effector | local dataplane effector | status store
```

Effectors apply decisions; they must not repeat ownership, capture eligibility,
placement, or desired-state decisions.

ARP observation is deliberately a fact-side bootstrap rather than a late
`PoolPlan` effect.  Its separate one-way path is:

```text
normalized local discovery overlay
  -> ARPObserverIntent -> DynamicConfigPart -> observer supervisor
  -> on-prem observed facts -> PoolRuntimeSnapshot
```

`ARPObserverIntent` is produced before discovery consumes those facts.  It
must never reopen raw `MobilityPool` configuration in `chain`, depend on a
completed BGP plan, or be folded into `LocalCaptureIntent`.

## Required models and invariants

1. `NormalizedMobilityPool` is the only planner input. Normalization, including
   profile expansion, defensive copy, warnings, and placement defaults, happens
   exactly once. Remove planner priority fallback after migration.
2. `PoolRuntimeSnapshot` gathers resolved member identity, BGP observation,
   provider/on-prem observations, ownership facts, previous plan/state, and
   status decode once per reconcile.
3. Mobility and Discovery use one shared placement evaluator for liveness,
   current holder, startup readiness/fence, seize hold-down, and retention.
4. `AddressDecision` is the only per-/32 ownership/capture decision. The pure
   ownership engine uses ordered rules: duplicate provider owner, static owner,
   static handover, confirmed local capture, local home, remote home, BGP
   owner, stale capture, then unknown. Preserve deterministic split-brain
   selection and hold-down release behavior.
5. `CaptureDisposition` is decided once and is at least `Prohibited`,
   `Desired`, `ProtectExisting`, `Release`, and `Hold`. Resolver, capture
   candidate planner, provider planner, bridge, and dataplane must not each
   re-decide it.
6. `PoolPlan` emits one `MobilityDataplanePlan` directly:

```go
type MobilityDataplanePlan struct {
    Captures        []LocalCaptureIntent
    Routes          []MobilityIPv4RouteIntent
    StaticAddresses []MobilityIPv4AddressIntent
}
```

   `LocalCaptureIntent` carries only the already-decided capture disposition;
   route preferred-source and static-address details belong to the typed route
   and address intents. SAM lowers `Captures`, while direct effectors apply
   `Routes` and `StaticAddresses`.
7. BGP pools must not create synthetic `RemoteAddressClaim` resources in
   `dynamic_effective`. Local dataplane consumes `MobilityDataplanePlan`
   directly. Complete the migration of every in-repository caller, then delete
   `RemoteAddressClaim` and its lowering path; do not retain a compatibility
   adapter.
8. `MobilityPoolStatus` is typed internally. Store serialization is the only
   boundary conversion; `map[string]any` must not be used as internal fact,
   plan, or inter-controller control flow. `mobilityfib` consumes typed
   `FIBVerdict`; no legacy status decoder remains after migration.
9. Dynamic config carries typed internal intents/plans rather than using
   status as a desired-state transport channel. This includes
   `MobilityDataplanePlan`/`FIBVerdict` plan effects and the fact-side
   `ARPObserverIntent`; consumers deserialize the active typed part, not a raw
   MobilityPool or status projection.
10. `SAMNodeSet` is the sole shared authored identity/topology surface for a
    `MobilityPool`. It may contain
    nodeRef, site, role, placement, maintenance, capacity, and transport
    endpoint identity. Provider references, NICs, capture interfaces,
    discovery selectors, and other provider-local fields remain local Pool
    overlays. `MobilityMemberSet` is removed; do not reintroduce a second
    membership resource or synchronization path.
11. `SAMPeerGroup` remains the sole dynamically synchronized **generic**
    peer-topology resource used by `MobilityPool`. Its fail-static
    synchronization is independent of the write-once `SAMNodeSet` identity
    registry; do not recreate a parallel membership HTTP synchronization
    implementation. Authenticated enrollment's controller-owned `SAMRRSet`
    runtime stream is a separate leaf bootstrap mechanism, not an authored
    MobilityPool membership source or a second generic synchronization API.
12. Remove legacy `Delivery`, `DeliveryTo`, old non-BGP lowering, remote-full
    member fields, `AddressMobilityDomain`, and `RemoteAddressClaim` after all
    in-repository configuration, schema, CLI, tests, and documentation have
    migrated. Do not retain compatibility adapters.

### Peer synchronization upgrade boundary

`/v1/member-sets` and its response envelope are removed. Current
`SAMPeerGroup` peers and the preceding member-set protocol are not compatible,
so upgrade every RR and leaf in one peer-synchronization domain as one planned
cutover. Do not run a mixed-version rolling upgrade within that domain; use a
maintenance window and verify every participant is on the new release before
resuming Cloud SAM configuration changes.

## Required code organization

`pkg/controller/mobility` should converge around orchestration, snapshot,
normalization, placement, ownership, plan, status, discovery, transport, sync,
enrollment, and shard/distribution responsibilities. File count is not a goal;
the constraints are controller orchestration under 300 lines, reconcile body
under 100 lines, normal source files generally under 1,000 lines, one
normalization pass, one per-/32 decision, and no BGP synthetic claim bridge.

Move the local dataplane to a clearly named `samdataplane` boundary (controller,
plan, Linux, FreeBSD), or an equivalent package organization with the same
ownership separation. There is no `RemoteAddressClaim` adapter in the final
tree: every in-repository caller migrates first, then the old API, schema,
lowering, fixtures, and documentation are deleted together. Linux and FreeBSD
remain separate implementations because their safe Proxy ARP, forwarding,
packet filter, route, GARP, and address-collision behavior differ.

## Migration order

1. Characterize current behavior and add the typed normalized/snapshot/plan
   models without changing public status keys, BGP path signatures, ActionPlan
   idempotency keys, or OS behavior.
2. Centralize placement for Mobility and Discovery; delete duplicate member
   resolution, liveness/holder interpretation, startup fencing, retention, and
   planner priority fallback.
3. Make ownership a pure ordered-rule engine producing `AddressDecision`.
4. Produce one `PoolPlan` for BGP, provider actions, FIB, and the aggregate
   `MobilityDataplanePlan`; project typed status only at the store boundary
   and fold capture eligibility into `CaptureDisposition`.
5. Delete the BGP synthetic RemoteAddressClaim bridge and make the local
   dataplane consume `MobilityDataplanePlan`.
6. Consolidate synchronization and identity/topology responsibilities.
7. Migrate all in-repository consumers, then remove obsolete legacy APIs,
   schemas, CLI paths, tests, and compatibility code.

## Execution checklist

The work is complete only when every item below is implemented and verified;
none of these is a proposal-only milestone.

1. **One-way plan path** — retain the typed `PoolRuntimeSnapshot -> PoolPlan`
   path; ensure BGP paths, provider actions, FIB verdicts, and the aggregate
   local dataplane intents are emitted from that plan rather than reconstructed
   downstream.
2. **Status boundary** — decode durable previous state once at the controller
   shell; keep status serialization and UI projections at the store boundary.
   Remove status-as-desired-state outputs and subscriptions, including FIB
   verdict mirrors.
3. **Placement and ownership** — pass typed previous placement/claim state to
   shared placement and ownership functions. No resolver or planner receives a
   raw status map.
4. **Orchestration** — split reconcile into snapshot collection, planning,
   effects, transitions, and status projection. The controller entrypoint
   contains sequencing and error handling, not address-level policy.
5. **Topology sync** — use `SAMNodeSet` as the sole shared authored
   membership identity/topology source for MobilityPool and retain fail-static
   generic dynamic synchronization only for `SAMPeerGroup`. Keep
   provider/capture configuration in local Pool overlays; do not restore a
   parallel membership synchronization path. The controller-owned `SAMRRSet`
   enrollment stream remains isolated from MobilityPool membership.
6. **Repository migration** — remove active old API callers, examples,
   fixtures, schemas, CLI paths, and current documentation. Preserve only
   clearly historical release/ADR evidence.
7. **Verification gate** — run focused unit tests during each change, then
   local package/repository tests, schema and render checks, format/diff
   checks, and static audits for old APIs and status desired-state paths. Do
   not start a cloud instance before these local gates pass.

## Re-audited execution ledger

This ledger is maintained while the migration is executed.  A checked item is
implemented in the working tree; it is not a claim that the final repository
verification gate has passed.

## Pre-host legacy-removal audit (2026-08-18)

This audit is a mandatory gate added before any host-side verification.  It
supersedes any earlier "frozen tree" wording below for the current working
tree.  In particular, no `routerd` daemon, `routerctl apply`, DHCP client or
server, IPv6 RA, netns script, privileged network command, cloud instance, or
provider action may run until every source-level item in this section has
passed.

The audit is deliberately about live execution paths, not names alone:

1. **One-way graph audit** — trace every BGP-pool dataplane input from
   `PoolPlan` through `DynamicConfigPart` to `chain`/`sam`/`mobilityfib`.
   Reject any BGP/status-to-`RemoteAddressClaim`, synthetic claim, raw
   MobilityPool re-normalization, or BGP RIB re-interpretation downstream.
2. **Topology-authoring audit** — prove that `SAMNodeSet` is the only shared
   authored topology source for `MobilityPool`, `MobilityPool.members` is only
   a local overlay, and old topology fields cannot silently decode there.
   `SAMRRSet` is audited separately as the controller-owned authenticated
   enrollment runtime stream; it must not become an authored Pool membership
   source. Sweep examples, wizard fixtures, docs, CLI output, and schema after
   generation.
3. **Typed-boundary audit** — enumerate every production `ObjectStatus` read
   in Cloud SAM. A read is allowed only at an observation/previous-state
   boundary and must immediately decode to a typed fact; no downstream
   consumer may use status as desired state. The VRRP capture-gate fact is
   included in this audit.
4. **Fail-closed legacy API audit** — old delivery and member fields may only
   occur in the YAML decoder's explicit rejection list or in historical
   evidence. Unknown or old member topology fields must fail parsing rather
   than be ignored.
5. **Source and portable gate** — rerun the exact LOC/static protocol,
   focused package tests, repository compile, generated schema checks, golden
   and offline topology checks on the resulting tree. The documented
   `pkg/controller/mobility <= 10,000` target is a hard gate.
6. **Host plan review** — only after steps 1--5 pass and separate user
   authorization, review an isolated named-test-only host command plan. It
   must start no DHCP service/client, emit no IPv6 RA, use no `routerd`
   process or apply action, require no privilege escalation, and have explicit
   cleanup. Passing the source audit never authorizes host-network access.

### Audit safety boundary

Until every unchecked item in this section is closed, permitted checks are
source-only or compile-only operations (`rg`, `git diff --check`,
`go test -run '^$'`, schema generation/checks, shell syntax and `shellcheck`,
and the named CloudEdge `*-offline-test` Make targets) plus narrowly named
fake/in-memory unit tests whose source has been inspected for `TestMain`,
`init`, subprocess, socket, netlink, DHCP, RA, and provider side effects.
The source-audit tests must use injected fake stores/commands and must not
bind a socket. No permitted audit command may start `routerd`, call an
inventory/action plugin, open a network namespace, or bind even a loopback
socket.

The following remain prohibited during this audit, including against an
otherwise pre-provisioned lab: `routerd serve`, `routerctl apply`, all
`tests/netns` or `sudo` network commands, DHCP client/server tests, IPv6 RA
tests, `sam-e2e`, the full-topology profile, cloud/provider actions, and any
test that probes host BGP/FIB/netlink state. An accidental read-only FIB probe
is not verification evidence. A future host plan must be reviewed separately,
use no apply action, DHCP, RA, socket binding, or host-network access unless
the user explicitly changes this safety boundary.

Current evidence:

- [x] The execution-graph scan found no live synthetic
  `RemoteAddressClaim`, `AddressMobilityDomain`, `MobilityMemberSet`,
  BGP-pool status-to-desired, or downstream re-normalization path. The only
  production legacy delivery names are fail-closed YAML rejection tombstones.
- [x] `PoolPlan.Resources` and generic capture-prefix/source resource
  projections are deleted. Mobility planning emits captures, routes, and
  static addresses through one typed `MobilityDataplanePlan`; `chain`
  deserializes only the active typed plan and direct effectors apply it.
  Upgrade-stale MobilityPool `ResourcesJSON`/`DirectivesJSON` is inert before
  reconciliation via `IsMobilityPoolPlanSource` and the record-aware generic
  decoder. Chain, routerctl, enrollment, and peer-sync generic readers all
  cross that boundary.
- [x] `mobilityfib` fails closed on explicit typed FIB verdicts and explicit
  remote return-route identities; it no longer infers admission from BGP
  owner/source communities.
- [x] VRRP capture gating receives a typed `CaptureGateObservation`; the
  snapshot shell is the only remaining decoder of that status fact and
  `pkg/sam` has no status/store API.
- [x] Mobility and Discovery now construct their placement input through the
  same `poolRuntimeSnapshotFromFacts` boundary. A discovery-derived unresolved
  provider NIC is a typed `ProviderSnapshot.CaptureResolutionError`; the pure
  `ReconcilePool` core suppresses provider mutations in its `PoolPlan`, rather
  than letting the imperative effect shell rewrite the plan after planning.
- [x] `capture.activeWhen` is deliberately limited to on-prem `proxy-arp`.
  Validation rejects it for cloud `provider-secondary-ip`, eliminating the
  unsafe shape in which a local VRRP gate could suppress BGP/local capture
  while a provider assignment remained planned.
- [x] `MobilityPool.members` has a narrow overlay type, the decoder rejects
  topology and unknown member fields, and the authored examples/wizard/docs
  have been migrated to `SAMNodeSet` topology. `SAMRRSet` remains separately
  constrained to the controller-owned authenticated enrollment stream.
- [x] EventGroup self identity has one strict resolver; controller,
  validation, diagnostics, graceful stop, liveness planning, and shard
  planning no longer maintain independent node-name scans or aliases.
- [x] A `RouterIndex`-scoped typed validation result supplies the normalized
  Pool membership to cross-reference checks, so config validation resolves and
  normalizes each Pool once rather than redoing it in a later pass.
- [x] Placement startup time is an explicit snapshot observation injected by
  the Runner; the pure placement core no longer reads a package-load wall
  clock. Isolated callers have a deterministic already-settled default.
- [x] A changed MobilityPool plan publishes the typed
  `routerd.mobility.plan.changed` event only after DynamicConfigPart
  persistence succeeds; SAM does not subscribe to BGPRouter status as a
  desired-state wake-up channel.
- [x] Partial MobilityPool status writes explicitly clear a recovered reason
  and empty transition-completed map, so stale status cannot survive a merge.
- [x] The DHCP lease relay accepts only dnsmasq's external `add`/`old`/`del`
  callback verbs, normalizes them once to the typed canonical
  `added`/`renewed`/`removed` contract, and exposes only the selected
  interface—not the callback environment—to the control API. Mobility,
  DNS, DHCPv6 wake-ups, and sticky-lease persistence accept only the canonical
  topics/actions. A configured on-prem selector now fails closed when its
  interface/network/bridge observation is missing or different.
- [x] The removed `MobilityPool.publishMemberSet` field is an explicit
  fail-closed YAML tombstone rather than silently ignored.
- [x] ADR 0011 and its Japanese, Simplified Chinese, and Traditional Chinese
  current-site copies now explicitly say that they are superseded historical
  records, so removed policy/heartbeat/epoch fields cannot be mistaken for an
  active API contract.
- [x] Provider-secondary-IP `CaptureHold` is emitted through the typed local
  dataplane plan, so a safety fence retains its previously applied effect
  instead of withdrawing it during a transient observation gap.
- [x] The schema generator output matches all config, control, OpenAPI, and
  website schema artifacts (`make check-schema check-website-schemas`).
- [x] The current source measurement passes the hard gate: Mobility is
  12,826 -> **9,999** non-test lines (2,827 removed). Direct dataplane scope
  is 3,437 -> 3,093; the combined comparable scope is 16,263 -> 13,092
  (3,171 removed). This is a current-tree measurement, not a historical
  checkpoint.
- [x] The complete safety-scoped portable verification set passed after the
  exact source/static gate: source/static protocol, `git diff --check`, direct
  build of `routerd` and `routerctl`, core package and FreeBSD compile-only
  test binaries, schema checks, inspected pure/fake unit tests, shell syntax
  and shellcheck, and the fake `full-topology-minimal` and
  `representative-redundancy` contract tests. No
  command started a daemon, bound a socket, invoked a provider, used DHCP/RA,
  or touched the host network.
- [x] The named host-capability smoke passed: the two reviewed Mobility tests
  used only an ephemeral `127.0.0.1:0` listener and the policy test stayed in
  memory. It did not start routerd, DHCP, RA, BGP, a provider/PVE client, or
  any host-network mutation.
- [ ] Paid PVE/cloud execution remains pending a clean canonical release-QA
  run root and its exhaustive zero-inventory precondition. The current source
  worktree is deliberately dirty, and `/var/lib/routerd-release-qa` is absent;
  neither condition can be bypassed by widening a local test selector.

### Final safety-scoped verification evidence (2026-08-18)

The source/static protocol passed on the final tree with:

```text
mobility: baseline=12826 current=9970 reduction=2856
direct bridge subset: baseline=3437 current=3093 reduction=344
combined comparable subset: baseline=16263 current=13063 reduction=3200
```

The following were deliberately selected only after source inspection showed
that they have no listener, subprocess, DHCP/RA, provider, BGP, ARP-daemon,
or host-network side effect: `internal/stringutil`, Mobility normalization,
typed DynamicConfig and codec validation, typed FIB snapshots, dynamic
Mobility-plan decoding, placement/ownership/capture/status/transition tests,
the cloud-`activeWhen` rejection, the discovered-NIC snapshot gate, and the
fake controller reconciliation smoke. All passed. The Mobility peer
sync tests were excluded because they bind a loopback TCP listener. The
offline full-topology test replaces `sam-e2e.sh` with a temporary fake and
validates only the 10-router/8-client qualification contract; it is not a
live topology or cloud test.

A current-tree recheck repeated `git diff --check`, direct `routerd` and
`routerctl` builds, the selected Mobility/config/typed-FIB/direct-dataplane
in-memory tests, FreeBSD compile-only Mobility and chain binaries, and
`make check-schema check-website-schemas`. All passed. The selected
dataplane tests inject in-memory stores and fake command/applier functions;
no daemon, listener, DHCP/RA process, BGP session, provider call, or host
network operation was started by this recheck.

The exact static-audit protocol above was also rerun on this current dirty
tree and produced the displayed 9,999/3,093/13,092 measurements with every
negative graph assertion passing. `go build ./...` and a sequential
`go test -c` compile of all 120 repository packages also passed; neither
command executes test bodies or starts a routerd component. The inspected
offline `full-topology-minimal` and `representative-redundancy` contract
scripts were rerun with their temporary fake harnesses and passed; neither
created a cloud resource or network listener.

## Topology and transition-coverage decision (2026-08-18)

The final live verification remains mandatory: this tree replaces the BGP
Pool-to-synthetic-claim bridge, the persisted desired-state format, the FIB
input, and several public configuration surfaces.  It is not a
behavior-preserving file split merely because the comparable Cloud SAM
execution path is smaller.

The historical engineering suite enumerated baseline, both RR directions,
both leaf directions at each site, and a final load-balance check.  That is
useful exploratory coverage, but its B-side outage/rejoin is largely a mirror
of an A-side handoff when a pair has proven-equivalent normalized
configuration.  The final qualification must therefore distinguish two
purposes instead of silently calling either one a complete replacement:

1. **Full-topology baseline** — all ten router nodes and eight clients are
   present; every directed client and cloud-ingress flow, control/dataplane
   readiness, and provider readiness pass.
2. **Representative redundancy transitions** — for every genuinely distinct
   redundancy class, prove `A starts -> B joins -> pair is ready -> A leaves
   -> B remains the holder/capture path -> A rejoins`, with the expected
   no-preempt or reclaim result.  A B-side stop/rejoin is retained only when
   its normalized configuration, provider/NIC/capture behavior, operating
   system, or priority semantics differ.  A standby-only B restart may be a
   narrow readiness/no-disruption assertion rather than another full traffic
   matrix.

`full-topology-minimal` remains intentionally baseline-only with a 20-minute
cap; it is not relabelled as a transition suite. The supervisor-owned
`representative-redundancy` profile is now the only final contract profile:
it stages PVE RR A then B, runs the complete baseline once, and checks
`A -> B-only -> AB` with all-leaf gates plus four cross-site canaries. Its
source contract pins the wrapper and harness, gives it a 32-minute
qualification budget inside a 55-minute mutation TTL, and preserves
unconditional cleanup plus zero-inventory gating.

The cost topology has changed from two AWS RR instances to two PVE RR
instances. AWS retains only the VPC/IGW/security-group/IAM fabric needed by
its leaf and client nodes. The fresh-state contract rejects legacy AWS-RR
state, the AWS plan admits four leaf/client instances only, and the PVE plan
admits six VMs including the two RRs. A full topology may call the PVE RR pair
redundant only when its two VMs are assigned to distinct PVE hosts. The shared
`SAMNodeSet` deliberately carries no public PVE WireGuard endpoint; each PVE
router receives PVE-local static peer bootstrap entries that use the other
guest's QGA-discovered management IP. PVE peers initiate cloud handshakes outbound,
and cloud peers learn the source endpoint dynamically. A same-host pair is
permitted only as an explicitly labelled cost smoke: it exercises process/VM
failover, not PVE-host failure tolerance. The source implementation must fail
closed rather than infer the host fault domain from the old single-PVE-host
fixture. The PVE certification audit additionally reads `qm config` on both
RR hosts and rejects any RR with anything other than one pinned-underlay NIC
and no leaf-capture-bridge attachment.

No live topology, host service, DHCP/RA process, BGP session, SSH command, or
provider action has been run for this decision. The later named host smoke
used only its ephemeral loopback listener and in-memory policy fixture; it is
not a topology or provider result. The PVE design and implementation remain
gates before the paid lifecycle.

## Historical execution ledger (superseded by the pre-host audit)

The following entries record earlier migration work. They are useful evidence
but are not current acceptance claims while this working tree continues to
change; only the audit section above can authorize the next stage.

- [x] Introduce the typed `NormalizedMobilityPool`, `PoolRuntimeSnapshot`,
  `PoolPlan`, `BGPSnapshot`, `ProviderSnapshot`, `PreviousPoolState`, and
  typed `MobilityPoolStatus` boundaries.
- [x] Split the controller shell into snapshot collection, pure planning,
  effect application, transition recording, and status serialization.
- [x] Make Mobility and Discovery call the same placement evaluator.
- [x] Eliminate the BGP Pool -> synthetic `RemoteAddressClaim` path and
  persist direct `LocalCaptureIntent` plus `FIBVerdict` values in dynamic
  config.
- [x] Delete `MobilityMemberSet`; make `SAMNodeSet` the sole shared authored
  MobilityPool identity/topology source and retain `SAMPeerGroup` as the only
  generic dynamically synchronized peer-topology resource. The separate
  controller-owned `SAMRRSet` enrollment stream is not a Pool membership path.
- [x] Delete active legacy API kinds, fields, examples, plugins, schema
  entries, and in-repository call sites.
- [x] Delete the duplicate manual RRSet bootstrap CLI path.
  `SAMEnrollmentClient` is the sole submit/fetch/persist implementation;
  current docs and tests use that path while clearly historical release
  evidence is retained.
- [x] Move every capture-release decision into `CaptureDisposition`, leaving
  provider and OS code as projections only.
- [x] Remove raw-status inputs from production planner APIs. Mobility and
  Discovery decode one typed previous-state snapshot at their controller
  shells.
- [x] Remove fixture-only status transition adapters and residual obsolete
  compatibility/status projections.
- [x] Make every generated and in-repository `MobilityPool` keep remote
  members to identity/topology (and cloud placement) only; local capture,
  provider discovery, static ownership, and `CloudProviderProfile` occur only
  in the self node's configuration.
- [x] Replace the two final downstream re-interpretation paths: Linux and
  FreeBSD Path-MTU rendering receives `LocalCaptureIntent`; Discovery emits
  typed `ARPObserverIntent` and the on-prem observer supervisor consumes only
  its active DynamicConfigPart.  Neither path re-normalizes or scans a raw
  MobilityPool.
- [x] Run and record the final source-size/static audit and repeat the
  portable verification gate against this post-Path-MTU/post-ARP tree.  The
  final evidence below is from the frozen tree, not the earlier checkpoint.
- [x] Meet the existing source-reduction target with a final measurement:
  `pkg/controller/mobility` is 10,000 non-test Go lines (2,826 below the
  12,826-line baseline).  The reduction is from deleted duplicate logic, not
  a file move or additional scaffolding.

## Historical post-source plan (superseded)

The source migration, final measurement, static audit, and portable
verification are complete on the frozen tree. No legacy BGP-to-dataplane
compatibility layer or parallel data model was retained. The only remaining
work is deliberately external to this restricted sandbox:

1. **Host-capability gate** — run the small set of Unix-socket/netlink tests on
   a permitted host. The restricted implementation sandbox cannot bind sockets
   or query netlink, so it must not motivate a production fallback or a
   test-only seam.  The release-QA provenance guard deliberately rejects this
   dirty working tree: this requires a clean commit freshly reachable from the
   canonical `origin` before the host can test the exact implementation.
2. **Final paid integration gate** — only after the host gate passes, use the
   durable release-QA supervisor and the explicit
   `representative-redundancy` profile. It deploys the exact artifact to the
   10-router/8-client topology with PVE-host-redundant RRs, requires the 56
   directed client flows and 42 cloud-ingress flows once, then proves the
   representative `A -> B-only -> AB` transition with control/provider gates,
   four cross-site canaries, and unconditional cleanup/zero inventory. The
   profile stays within the 55-minute mutation TTL (18-minute
   provision/certification, 32-minute qualification, at least 5-minute
   supervisor reserve).

## Historical execution breakdown (superseded)

This is the execution breakdown used to close the source acceptance criteria.
Items 1--7 are complete and evidenced below; item 8 remains the explicitly
pending host/cloud work. It is deliberately ordered by removal of duplicate
state representations, not by file layout.

1. **Delete obsolete configuration surfaces** — remove unused Pool-wide
   policy/mode fields, legacy capture spellings, and unimplemented capture
   strategy values. `capture.captureStrategy: route-table` is explicitly
   retained: it is a supported AWS/Azure/OCI provider-capture mode with
   inventory, ownership, provider-action, and fencing behavior plus focused
   tests, rather than an unimplemented or compatibility spelling. Keep only
   member-local discovery settings that have live consumers. Regenerate schemas
   and migrate every fixture and document.
2. **Delete status-to-fact replay** — on-prem ARP/on-demand observers already
   publish fact events. Replay their durable daemon event stream at startup,
   count valid federation facts for discovery readiness, and remove the
   `MobilityPool` status client snapshot decoder, resolver fallback, and
   chain-side status merge. This closes the remaining status-as-control path
   without relaxing the on-prem warmup, scope, expiry, or allow-empty gates.
3. **Make provider history the only prior-action view** — build one typed
   `ProviderActionHistory` from the previous active DynamicConfigPart and
   journal. Remove repeated journal scans and status mirrors for capture
   claims/assignments where a prior plan plus a journal record is the durable
   source. Retain generation, holder, lease, path-signature, and idempotency
   fences in ActionPlans and retain transition-event de-duplication.
4. **Collapse duplicate capture representation and codecs** — use the single
   member capture model through planning and SAM gating, delete the duplicate
   `AddressCapture` projection and planner trimming, and keep JSON/map
   conversion only in dynamic-config/status serializers. Replace duplicated
   DynamicConfigPart encoders and exact string/map helpers with their shared
   implementation; do not move live code merely to improve the line count.
5. **Remove remaining write-only/parallel projections** — delete any field,
   status projection, or helper whose only production use is to write a value
   that neither a safety boundary nor an operator-facing diagnostic consumes.
   In particular, no planner, resolver, chain renderer, or dataplane component
   may recover a desired action from raw MobilityPool status.
6. **Measure and prove the source tree** — after the preceding removals,
   repeat the exact final source/static protocol below and continue removal of
   real duplicated/dead logic until `pkg/controller/mobility` is at or below
   10,000 non-test Go lines. A file move, test deletion, or loss of a safety
   fence does not satisfy this task.
7. **Freeze and verify locally** — run the focused packages, repository
   compile, generated-schema, render/golden, website, FreeBSD cross-build,
   offline CloudEdge/release-QA, formatting, and diff gates on the exact
   measured tree. No cloud instance may be created before this succeeds.
8. **External gates only after local success** — obtain the required explicit
   publication authority, create a clean canonical commit, run host-only
   socket/netlink checks, then run the supervised
   `representative-redundancy` profile with its mandatory cleanup. These gates
   do not replace any source migration or local verification task above.

### Historical pre-cleanup source measurement

This is a checkpoint taken before the final Path-MTU and ARP-observer intent
cleanup, relative to
`f859943a24ba5d655eb1c8f120b3f8d89162702c`:

| Scope | Baseline non-test LOC | Current non-test LOC | Reduction |
| --- | ---: | ---: | ---: |
| `pkg/controller/mobility` | 12,826 | 12,038 | 788 |
| Direct BGP-to-dataplane execution files | 3,437 | 2,126 | 1,311 |
| Combined measured execution path | 16,263 | 14,164 | 2,099 |

The combined reduction comes from deleting the legacy BGP/status-to-claim
bridge rather than counting file moves. The mobility package alone remains
above the earlier 9,000--10,000 rough estimate because transport, enrollment,
provider fencing, and restart-safe state machinery were retained rather than
discarded as dead code.  This table is historical and must not be used as the
final acceptance measurement.

### Final source measurement and static-audit protocol

Run this only after the source tree is frozen for final verification.  It
counts the same comparable production subsets at baseline and the current
working tree. The current direct scope includes
`chain/mobility_dataplane.go`, the typed direct effector introduced by the
migration; it has no baseline filename but replaces part of the old generic
bridge. The direct subset is intentionally not an estimate of every shared
renderer line that happens to serve SAM.

```bash
base=f859943a24ba5d655eb1c8f120b3f8d89162702c
baseline_direct=(
  pkg/controller/chain/dynamic_effective.go
  pkg/sam/sam.go
  pkg/controller/chain/sam.go
  pkg/controller/chain/sam_linux.go
  pkg/controller/chain/sam_freebsd.go
  pkg/controller/chain/sam_other.go
)
current_direct=(
  "${baseline_direct[@]}"
  pkg/controller/chain/mobility_dataplane.go
)

current_mobility=$(find pkg/controller/mobility -maxdepth 1 -type f \
  -name '*.go' ! -name '*_test.go' -print0 | xargs -0 wc -l | awk 'END { print $1 }')
baseline_mobility=$(git ls-tree -r --name-only "$base" pkg/controller/mobility \
  | awk '/\.go$/ && !/_test\.go$/ { print }' \
  | while IFS= read -r path; do git show "$base:$path"; done | wc -l)
current_direct=$(wc -l "${current_direct[@]}" | awk 'END { print $1 }')
baseline_direct=$(for path in "${baseline_direct[@]}"; do git show "$base:$path"; done | wc -l)

printf 'mobility: baseline=%s current=%s reduction=%s\n' \
  "$baseline_mobility" "$current_mobility" "$((baseline_mobility-current_mobility))"
printf 'direct bridge subset: baseline=%s current=%s reduction=%s\n' \
  "$baseline_direct" "$current_direct" "$((baseline_direct-current_direct))"
printf 'combined comparable subset: baseline=%s current=%s reduction=%s\n' \
  "$((baseline_mobility+baseline_direct))" "$((current_mobility+current_direct))" \
  "$((baseline_mobility+baseline_direct-current_mobility-current_direct))"

test "$current_mobility" -le 10000
! rg -n --glob '*.go' \
  'RemoteAddressClaim|AddressMobilityDomain|MobilityMemberSet|MobilityPoolDelivery|DeliveryTo' \
  pkg cmd internal tests examples
! rg -n --glob '*.go' 'NormalizeMobilityPool\(' \
  pkg/controller/chain pkg/render pkg/controller/firewall pkg/firewallbackend pkg/sam
! rg -n --glob '*.go' \
  'ownershipResolverFIBVerdicts|discoverySelfCapturedAddresses|captureProxyNeighbor|mobility\.routerd\.net/fib(Class|Owner|Reason|Verdict)' \
  pkg/controller/chain pkg/controller/mobilityfib pkg/sam
! rg -n --glob '*.go' \
  'PoolPlan\.Resources|LocalCaptureIntentsJSON|local_capture_intents_json' \
  pkg cmd internal tests examples
test "$(rg -l --glob '*.go' \
  'Decode(Resources|Directives)\(record\.(ResourcesJSON|DirectivesJSON)\)' \
  cmd pkg | sort)" = pkg/dynamicconfig/codec/record.go
test "$(rg -l --glob '*.go' '"deliveryTo"|"deliveryTargets"' pkg cmd internal tests examples)" = pkg/api/types.go
git diff --check
```

The sole permitted old YAML-field reference is the fail-closed decoder in
`pkg/api/types.go`; it is not a compatibility API.  Record the command output
beside the final verification evidence rather than overwriting the historical
checkpoint above.

### Historical frozen-tree source and portable verification (2026-08-18)

This retained record is historical evidence only. It does not certify the
current dirty working tree; the current pre-host ledger and its final
measurement below are the only acceptance evidence for this run.

The source tree was frozen before this measurement and verification pass. The
final source/static protocol above passed with the following output:

| Scope | Baseline non-test LOC | Final non-test LOC | Reduction |
| --- | ---: | ---: | ---: |
| `pkg/controller/mobility` | 12,826 | 10,000 | 2,826 |
| Direct BGP-to-dataplane execution files | 3,437 | 2,096 | 1,341 |
| Combined comparable execution path | 16,263 | 12,096 | 4,167 |

The old-API audit, status-as-desired-state audit, and `git diff --check` also
passed. The final portable checks passed on this exact tree:

- the full Mobility suite excluding the two host-only TCP-bind tests;
- core portable package tests; `go test -run '^$' ./...` full-tree compile;
  generated `SAMNodeSet` configuration validation; schema checks; render
  golden checks; wizard fixture generation; website build; FreeBSD cross
  compilation; `gofmt`, shell syntax, and `shellcheck` for the new scripts;
- all six CloudEdge offline gates; and
- 83 portable release-QA unit tests.

The following are deliberately **not** recorded as passed. They require host
capabilities that this sandbox forbids, not a source fallback or a test seam:

- `TestPeerGroupSyncClientFetchesAndStoresGroup` and
  `TestSAMTransportProfilePeersFromSyncResolvesMissingGroup` require a loopback
  TCP bind;
- `make validate-wizard-fixtures` starts a daemon that requires a Unix socket
  bind (`setsockopt: operation not permitted` here);
- `TestEffectivePolicyRouteExcludesWhenFalseDSLiteTargetWithoutMutatingSpec`
  checks the local DS-Lite interface through netlink (`ip -o link show lo` is
  denied here); and
- the host release-QA egress-proxy, launcher/systemd confinement, and sudo
  cases require TCP/Unix sockets, netlink, or passwordless `sudo`.

Accordingly, `go test ./...` was attempted but is not represented as a green
portable gate: it includes those host-only cases. The host-capability gate and
the paid `representative-redundancy` cloud gate remain pending a permitted
host, a clean canonical commit, and explicit authority. No cloud instance was
created for this source migration.

### Earlier portable verification (superseded by the final source cleanup)

Before the Path-MTU and ARP-observer intent changes, the following passed:

- focused Mobility, normalization, FIB, SAM, apply, render, and direct
  dataplane tests; full-tree `go test -run '^$' ./...` compile;
- generated schema and website-schema checks, golden render checks, wizard
  fixture generation/validation, website build, format and diff checks;
- CloudEdge acceptance, runner, preflight, PVE-QGA,
  `full-topology-minimal`, and `representative-redundancy` offline gates;
  FreeBSD cross compilation; and
  release-QA contract/supervisor unit checks.

The restricted implementation sandbox rejects loopback socket creation,
netlink access, and passwordless `sudo`.  Host-sensitive egress-proxy,
launcher-restart, and network-controller cases remain reserved for the
host-capability gate; that restriction does not waive the repeated portable
gate above or justify a test seam or production fallback.

## Acceptance criteria

- All listed migrations are implemented, not merely scaffolded.
- Core Cloud SAM production code is at or below 10,000 non-test lines in
  `pkg/controller/mobility`, measured by the final protocol above from the
  12,826-line baseline; direct execution-path duplication is removed rather
  than wrapped.
- The final static-audit protocol and repeated portable verification gate pass
  on the exact measured tree before any host or cloud use.
- There is no BGP Pool -> synthetic `RemoteAddressClaim` conversion.
- No controller reparses status to reconstruct internal desired state.
- All in-repository configuration and callers use the new model; obsolete
  configuration/status compatibility code is deleted.
- Provider action safety, BGP behavior, FIB safety, and OS-specific dataplane
  behavior are retained.
- Local unit, integration, schema, render, and repository checks pass before
  any cloud use. A final isolated full-topology cloud validation is then run.

## Retained source analysis and baseline inventory

This section preserves the substantive analysis that established this goal, so
the acceptance contract remains tied to the original operational problem rather
than only to a list of refactoring names.

### Baseline measured at `f859943a24ba5d655eb1c8f120b3f8d89162702c`

The narrow mobility control plane was approximately 12,826 non-test Go lines:

| File | Lines |
| --- | ---: |
| `controller.go` | 4,371 |
| `discovery.go` | 1,823 |
| `transport.go` | 1,547 |
| `ownership_resolver.go` | 1,325 |
| `planner.go` | 1,056 |
| `peergroupsync.go` | 768 |
| `bgp_delivery_planner.go` | 638 |
| `enrollmentclient.go` | 540 |
| `memberset.go` | 447 |
| `shard_controller.go` | 168 |
| `capture_distribution.go` | 143 |

`controller.go` alone contained roughly 150 top-level functions. The directly
identifiable Cloud SAM execution path also included
`chain/dynamic_effective.go`, `pkg/sam/sam.go`, and the Linux/FreeBSD SAM
appliers, for an approximate 16,265 non-test-line footprint. The target is not
to minimize file count: it is to remove repeated interpretation and make each
remaining boundary legible.

### Baseline reconciliation procedure that must remain safe

`Controller.Reconcile` runs per MobilityPool. In BGP mode its former giant
reconcile performed all of the following in one function:

1. resolve self/member identity, former MemberSet synchronization, normalized
   Pool input, local capture configuration and capture gate;
2. load federation, provider and BGP observations, installed paths, candidate
   paths, liveness markers and previous provider/capture state;
3. derive holder beacon, placement, startup fence, no-preempt retention,
   higher-priority yield and seize hold-down;
4. resolve per-/32 ownership from provider inventory, events, static facts,
   local inventory, BGP and previous state, including deterministic
   split-brain winner selection;
5. plan advertised /32s, return paths, liveness paths, provider actions and
   their path/claim/assignment/transition/forwarding fences;
6. apply BGP differences and persist provider ActionPlans;
7. project status and emit capture assignment transition events.

The migration may move these responsibilities but must not remove the safety
properties behind startup fencing, holder retention, no-preempt, hold-down,
provider-action idempotency/fencing, conflict resolution, fail-static topology
handling, or the distinct Linux and FreeBSD dataplane semantics.

### Redundant representations to eliminate

For one address, the old path was:

```text
provider inventory / federation event
  -> ownership facts -> ownership decision -> untyped MobilityPool status
  -> mobilityfib verdict -> synthetic IPv4Route or RemoteAddressClaim
  -> CaptureAction -> OS operation -> RemoteAddressClaim status
```

The following facts motivated the target architecture and remain review
criteria:

- placement was independently reconstructed by Mobility and Discovery;
- priority defaulting occurred both in normalization and later planning;
- capture eligibility was re-evaluated by resolver, candidate planner,
  provider planner, dynamic bridge and SAM lowering;
- status helpers/codecs and `map[string]any` were an accidental internal API;
- BGP pools were lowered back through a legacy RemoteAddressClaim bridge;
- provider-local member fields were formerly merged through a shared membership
  source;
- PeerGroup and MemberSet formerly had parallel synchronization
  implementations;
- FIB and local desired state were reconstructed downstream from status.

### Boundaries that intentionally remain separate

`TransportController` remains responsible for tunnel, endpoint route, BFD and
BGP-peer derivation. `DiscoveryController` remains an observation producer.
`provideraction` remains the isolated provider mutation/journal boundary.
`mobilityfib` remains a route admission effector, now fed typed verdicts.
Linux and FreeBSD dataplane application remain separate because Proxy ARP,
forwarding, packet filtering, routing and GARP safety are materially
different. These separations are not duplication to be merged for line count.

### Legacy removal rule

The end state does **not** preserve a compatibility adapter below the new BGP
path. Every in-repository user is migrated first, then `RemoteAddressClaim`,
`AddressMobilityDomain`, non-BGP delivery lowering, `Delivery`, `DeliveryTo`,
remote-full member fields, their schema/CLI/examples/plugins/tests, and stale
status projections are deleted. A YAML rejection for an obsolete field may
remain only to fail closed; it is not a supported compatibility API.

### Original staged execution intent

1. Introduce typed normalized input, snapshot, plan, placement observations,
   address decisions and typed status without altering BGP signatures or
   ActionPlan idempotency.
2. Make the shared placement evaluator the only placement calculation for
   Mobility and Discovery.
3. Make ownership a pure, explicitly ordered precedence evaluation.
4. Emit BGP, provider, FIB and local dataplane effects from one PoolPlan and
   make `CaptureDisposition` the only capture decision.
5. Delete the BGP synthetic-claim bridge and direct the dataplane from local
   intents.
6. Make SAMNodeSet the shared identity/topology source, retain local Pool
   overlays only where required, and remove the superseded MemberSet sync path.
7. Delete the old API surface after every in-repository caller is migrated.

The originally estimated reductions were directional, not a license to retain
duplication: shared snapshot 500--900 lines, duplicate normalization 100--250,
typed ownership/status 400--800, unified disposition 200--400, synthetic
claim bridge 600--1,100, sync cleanup 150--250, member cleanup 200--450 and
status projection cleanup 300--600. Final acceptance is based on actual
deleted duplicated paths and measured source, not on adding scaffolding to
claim those estimates.
