# ADR 0012: BGP /32 Address Mobility

![Diagram showing ADR 0012 BGP address mobility replacing lease and epoch ownership with BGP best-path /32 advertisements, liveness markers, Route Reflector paths, FIB imports, and background provider capture](/img/diagrams/adr-0012-bgp-address-mobility.png)

## Status

Accepted. Phase 1 clean Option B implemented through B6/B7 -- 2026-06-03.
Placement no-preempt + holder-beacon addendum implemented and validated on real
cloud hardware -- 2026-06-15 (see the addendum at the end of this ADR and
[CloudEdge SAM internals](../reference/cloudedge-sam-internals)).

Supersedes the custom overlay-reachability source of truth introduced by
[ADR 0006](../adr/0006-event-federation.md),
[ADR 0008](../adr/0008-capture-coordination-fencing.md),
[ADR 0010](../adr/0010-capture-ownership-arbitration.md), and
[ADR 0011](../adr/0011-generalized-failover.md) for the CloudEdge mobility
dataplane. The older provider-action, VRRP, and doctor safety pieces remain in
scope as background reconciliation and local capture guards.

## Context

CloudEdge selective address mobility originally built overlay reachability from
a routerd-specific control plane:

- event federation carries observed/expired/heartbeat facts;
- the mobility controller projects those events into `AddressLease` rows;
- the planner lowers leases into `AddressMobilityDomain`,
  `RemoteAddressClaim`, provider `ActionPlan`s, `captureEpoch`, and
  `ownershipEpoch` state;
- SAM lowers generated claims into routes, proxy-ARP, and provider secondary-IP
  actions;
- the provider-action controller approves/executes cloud mutations.

This proved the product path, but it also made failover depend on a long
routerd-specific chain. In live 4-site testing, overlay/cloud failover remained
bounded by reconcile ticks, lease/epoch projection, action import/auto-execute,
provider API behavior, and cloud fabric propagation. Recent smoke results showed
cloud failovers around 120s in AWS/OCI, while the desired target is sub-60s and
preferably seconds for overlay traffic.

routerd already ships a GoBGP-backed `routerd-bgp` daemon and a BGP controller.
The existing surface can start GoBGP, configure peers and policies, advertise
static IPv4/IPv6 unicast prefixes with `AddPath`, withdraw with `DeletePath`, and
observe/import best paths into the Linux IPv4 FIB. GoBGP v3.37.0 also supports
EVPN Type-2/Type-5 and MAC mobility sequence numbers, but routerd's current BGP
resource model and FIB syncer expose only IPv4/IPv6 unicast. The fastest useful
cut is therefore plain IPv4 unicast `/32` mobility, not EVPN.

Cloud provider fabrics are a separate constraint. AWS VPC route tables, Azure
UDR/Route Server, and OCI VCN route tables do not automatically follow a VM's
private GoBGP overlay advertisements unless an explicit cloud routing integration
is configured. Provider secondary-IP assignment, route-table target changes, or
provider services such as Azure Route Server can still be required for
cloud-native ingress. BGP can remove provider API calls from the overlay
reachability critical path, but it does not delete the provider ingress problem.

## Decision

Move the **overlay reachability source of truth** for CloudEdge mobility to the
BGP RIB:

- Each owned address in a `MobilityPool` is represented as an IPv4 unicast `/32`
  BGP advertisement.
- The owner of an address is the node whose advertisement wins BGP best-path
  selection for that `/32`.
- Non-owners learn remote owned addresses from BGP best paths and install overlay
  delivery routes through the BGP FIB importer, not through generated SAM delivery
  routes.
- Mobility movement is expressed as BGP withdraw/advertise and path preference
  changes. Operator intent remains declarative in `MobilityPool`; operators do
  not hand-author leases, claims, or provider actions.
- Best-path arbitration uses standard unicast attributes first:
  `LOCAL_PREF`/`MED`/communities, plus deterministic router policy. A route
  sequence community may be added for observability, but plain BGP does not treat
  "newer sequence wins" as a native rule.
- EVPN is explicitly deferred. EVPN Type-2 MAC/IP mobility is a future interop
  option, not the Phase 1 mechanism.

Provider secondary-IP and forwarding actions are **demoted to background
reconciliation**:

- They remain necessary for cloud fabric ingress paths where packets enter through
  a VPC/VNet/VCN instead of an established routerd overlay path.
- They are reconciled eventually from the same BGP mobility view and provider
  inventory/action journal.
- They must not be the source of truth for overlay reachability.

On-prem LAN capture remains local:

- VRRP-master gating, proxy-ARP, GARP, non-master fail-closed behavior, and
  duplicate-holder doctor checks remain in force.
- BGP decides remote overlay reachability; it does not replace the local L2/ARP
  authority guard.

## Clean Option B Final State

The pre-release implementation now uses BGP as the mobility source of truth
directly:

- **Ownership:** the owner of a mobile `/32` is the current BGP best path for
  that prefix. There is no separate `AddressLease`, ownership epoch, or capture
  epoch registry.
- **Delivery:** non-owners import the BGP best path into the local FIB and route
  the `/32` over the overlay next hop. MobilityPool route-mode planning and
  generated SAM delivery claims are not part of the mainline.
- **Capture/trap:** cloud provider secondary-IP actions are derived from the BGP
  best-path view and local placement. They are background fabric-ingress
  reconciliation, not overlay reachability prerequisites.
- **Fencing:** provider actions carry the current mobility path signature
  (`mobilityPathSig`) plus desired holder and observed provider/journal
  transition. Stale actions are skipped when the desired BGP path no longer
  matches; the old ownership/capture epoch tables are gone.
- **Liveness:** mobility failover relies on BGP withdrawal and best-path
  convergence. Fast failure detection is provided by `BFD` resources rendered to
  FRR `bfdd`; BGP hold timers remain the non-destructive authority for route
  withdrawal when BFD is unstable. Custom mobility heartbeat/staleness
  projection is removed.
- **On-prem LAN authority:** VRRP-master gating, proxy-ARP, GARP,
  non-master fail-closed behavior, and duplicate proxy-ARP doctor checks remain
  local safety mechanisms.
- **State removed:** B6 physically removed the mobility lease, ownership epoch,
  capture epoch, and deprovision marker tables and APIs, with a net reduction of
  about 6.2k lines in that stage.

## Non-goals

- Do not implement EVPN in Phase 1.
- Do not remove provider executors in Phase 1.
- Do not claim cloud-native ingress is solved by BGP alone.
- Do not add consensus, etcd, Raft, or a single-writer lease database.
- Do not require operators to author dynamic BGP path resources for each address.
- Do not remove event federation globally; only retire mobility-specific uses
  once the BGP path is proven.

## Model

The intended steady-state mapping is:

| Existing concept | BGP mobility concept |
| --- | --- |
| `AddressLease` active owner | BGP best path for `pool/address/32` |
| observed owner event | local `/32` advertise |
| expired/released event | local `/32` withdraw |
| `staticOwnedAddresses` | static local `/32` advertise by the owning member |
| F3 handover | release/withdraw barrier, then new owner advertise |
| `RemoteAddressClaim` delivery route | imported BGP `/32` FIB route |
| capture placement active member | path preference / origin eligibility |
| `ownershipEpoch`/`captureEpoch` for overlay routing | best-path view and optional route metadata |
| provider secondary-IP action | background fabric-ingress reconciliation |
| on-prem proxy-ARP authority | unchanged VRRP-master gate |

## Phase 1 Scope

Phase 1 built the BGP unicast path and then removed the superseded custom
mobility planner/state path before release.

1. Add source-aware dynamic BGP path management for routerd-generated `/32`
   advertisements.
2. Project `MobilityPool` owner state into BGP advertisements.
3. Consume BGP best paths as the remote-address delivery view.
4. Move failover and static handover overlay reachability to BGP withdraw/advertise.
5. Convert provider secondary-IP handling into background reconciliation.
6. After parity was proven, remove the old lease/planner/epoch path.

## Consequences

Positive:

- Overlay failover becomes a routing convergence problem instead of a
  routerd-specific lease/action/provider serial workflow.
- The design aligns with Kubernetes edge patterns such as BGP service VIP and
  pod/service route advertisement.
- The most complex custom state (`AddressLease` projection, capture placement,
  capture/ownership epoch planning, deprovision markers) can be reduced
  substantially after migration.
- D3/D5/D6/D7 overlay reachability can converge even while cloud provider
  secondary-IP reconciliation is still pending.

Negative / risks:

- Plain BGP needs explicit policy to avoid ambiguous same-prefix advertisements.
  A sequence community is not a native fencing token.
- Provider fabric ingress can still be unavailable until background provider
  state catches up, unless the deployment also configures cloud routing
  integration.
- Existing live demos and acceptance probes must distinguish overlay reachability
  from cloud-native ingress.
- GoBGP observation is currently poll-based in routerd; Phase 1 must add an
  event-driven `WatchEvent` path or the BGP route installation loop will retain
  poll latency.
- Split-brain protection still depends on VRRP/provider fencing/doctor checks.
  BGP best path picks one forwarding path, but it does not by itself remove stale
  local proxy-ARP or stale provider assignments.

## Migration Rules

- Keep `MobilityPool` as the only operator-authored mobility intent.
- Default MobilityPool delivery to BGP. The old MobilityPool route-mode planner
  was a migration aid and is not accepted in the clean pre-release API.
- Never run two route-lowering sources for the same `(pool,address)` without a
  deterministic precedence rule.
- Mark generated BGP paths with source metadata so static BGP advertisements are
  not accidentally withdrawn by mobility reconciliation.
- Preserve provider-action idempotency and path-signature fencing while provider
  reconciliation remains present.

## Exit Criteria

- A 4-site demo can pass the directed SSH matrix using BGP-learned `/32` overlay
  routes.
- Cooperative drain and stale-owner failover converge through BGP without manual
  provider action approval/execution in the overlay path.
- Delaying or failing provider secondary-IP actions does not break overlay
  reachability.
- VRRP/proxy-ARP on-prem fail-closed semantics remain unchanged.
- The old mobility lease/planner path is removed after tests and live evidence
  cover the BGP path.

## Addendum: Placement no-preempt, startup fence, and the holder-beacon (2026-06-15)

Phase 1 established the BGP `/32` source of truth but left a gap: with two
placement members at equal priority, the deterministic best-path/nodeRef
tie-break would let a *returning* node reclaim a live holder, churning the
dataplane. The opposite failure also existed at cold start: both members could
mutually defer and leave the group with no holder. This addendum records the
mechanism that closes both gaps while keeping abrupt failover fast. It is
implemented in `pkg/controller/mobility/` and validated on real AWS/Azure/OCI
hardware. The operational detail lives in
[CloudEdge SAM internals](../reference/cloudedge-sam-internals); this section
captures the decision.

**Holder-beacon community.** The active capture holder (and only the active
holder) advertises its owner `/32` with community `64512:121`
(`bgpMobilityCommunityActiveHolder`). A peer treats a node as the group holder
only when that node's owner `/32` best path carries both its node-identity
community and `64512:121`. This is deliberately independent of the provider
plugin (BGP is always present) and of a standby's lower-preference
make-before-break advertisement (which never wins best path). It supersedes the
earlier attempts to infer holdership from next-hop matching or provider
self-scan, both of which proved unreliable.

**Equal-priority no-preempt.** On an equal-priority tie, placement prefers the
observed incumbent holder over the nodeRef tie-break, so a returning peer does
not preempt a live holder. A strictly higher-priority member still reclaims.

**Startup fence.** A node anchors a settle window at process start
(`placementSettleStart`, reset on every VM stop/start or reboot), but the fence
is now readiness-first. A node that would assert active and has not yet observed
an incumbent defers until initial BGP observation has completed and, for
provider-inventory-backed captures, provider self-observation has completed.
The wall-clock window remains a conservative fallback when readiness signals are
unavailable. This lets a crash-looping node leave the fence as soon as its
observations are actually ready, while a partitioned or blind node does not
assert active merely because 120 seconds elapsed.

**Holder retention and priority restore.** While a node physically holds its
group's captures it stays active (it yields only on losing its own holdership,
never because of a transient peer observation). The single exception is when the
holder-beacon shows a strictly higher-priority peer is now the holder, in which
case a low-priority interim holder yields so the configured priority restore
proceeds — handing the `/32`s over one at a time with no dataplane dip.

The community taxonomy, placement evaluation, and the three mechanisms above are
specified in full in
[CloudEdge SAM internals](../reference/cloudedge-sam-internals).
