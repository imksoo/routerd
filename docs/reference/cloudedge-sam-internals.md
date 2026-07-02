---
title: CloudEdge SAM Internals
---

# CloudEdge SAM Internals

This page explains the **architecture, internal implementation, and
configuration fields** of CloudEdge SAM (Selective Address Mobility) at a level
where operators and implementers can both follow "what happens inside". Read
[What is CloudEdge SAM](../concepts/cloudedge-sam.md) for the conceptual
introduction and [Selective Address Mobility](selective-address-mobility.md) for how
to author the config first.

The implementation lives in `pkg/controller/mobility/`. The descriptions here are
kept consistent with that code (notably `planner.go` and `controller.go`).

## Architecture: two planes

CloudEdge SAM cleanly separates reachability from cloud ingress.

### Plane 1: overlay reachability — the BGP best path is the truth

Each owned address in a `MobilityPool` is represented as an **IPv4 unicast `/32`
BGP advertisement**.

- The **holder of a `/32` is the node that wins the BGP best path** for that
  prefix.
- Non-holder nodes learn remote owned addresses from the BGP best path and
  install delivery routes via the overlay next hop into the FIB.
- Address movement is expressed as **BGP withdraw / advertise and path
  preference changes**. Operators never hand-author leases or claims.
- Failure detection is accelerated by **BFD** (FRR `bfdd`); when BFD is unstable,
  BGP hold timers remain the non-destructive authority for route withdrawal.

This is the decision in [ADR 0012](../adr/0012-bgp-address-mobility.md), which
replaced the older bespoke ledgers (`AddressLease` / `ownershipEpoch` /
`captureEpoch`).

### Plane 2: cloud ingress — provider operations are background reconciliation

Packets entering **from outside** through a VPC / VNet / VCN follow the cloud
fabric's routing, not the BGP overlay. So routerd:

- assigns the target `/32` to the holder VM's NIC as a **secondary IP**, and
- **enables forwarding** on that NIC (AWS `sourceDestCheck=false` / Azure
  `ipForwarding=true` / OCI `skipSourceDestCheck=true` / GCP `canIpForward=true`).

But these are **not the source of truth for reachability**; they are operations
**reconciled eventually in the background** from the BGP mobility view and
provider inventory. Even if the provider API lags, overlay reachability recovers
from BGP convergence alone.

## The BGP community taxonomy

The BGP communities that mobility attaches to `/32` advertisements are the
**signal wires** that tell other nodes a node's role, the provenance of an
advertisement, and whether it is the holder. They are defined in
`pkg/controller/mobility/controller.go`.

| Community | Constant | Meaning |
| --- | --- | --- |
| `64512:100` | `…CommunityOwner` | this advertisement is a mobility owner `/32` |
| `64512:101` | `…CommunityRoleOnPrem` | advertising node's role is on-prem |
| `64512:102` | `…CommunityRoleCloud` | advertising node's role is cloud |
| `64512:110` | `…CommunitySourceObserved` | provenance: observation-derived advertisement |
| `64512:111` | `…CommunitySourceStatic` | provenance: a static owned-address advertisement |
| `64512:112` | `…CommunitySourceHandover` | provenance: advertisement during handover |
| `64512:113` | `…CommunitySourceCapture` | provenance: provider-capture background route |
| `64512:120` | `…CommunityFailover` | a seize advertisement during failover |
| **`64512:121`** | **`…CommunityActiveHolder`** | **holder-beacon: attached only by the active holder** |
| (per node) | node-identity community | identifies which node advertised (derived from nodeRef) |

LOCAL_PREF is set relative to `bgpMobilityLocalPrefBase = 200`, so an active
advertisement carries a higher preference than a standby's make-before-break
advertisement.

### The holder-beacon (`64512:121`) is the linchpin

`bgpMobilityPathAttrs` (`controller.go`) attaches `64512:121` only when the
advertisement is from an **active holder** and is not a provider-capture
background route.

On the receiving side, `bgpObservedGroupHolder` (`planner.go`) treats a node as
the group holder only when the best path for a `/32` carries **both the node's
node-identity community and `64512:121`**. This means a:

- standby's weak (lower-preference) make-before-break advertisement, and
- just-booted (cold-start) advertisement that is not yet active,

are **not mistaken** for holdership. It is an **authoritative holder signal**
that is plugin-independent (BGP is always present) and best-path-independent (only
the active node emits the beacon).

> Design history: earlier attempts inferred holdership from next-hop matching or
> a provider self-scan. Both failed — "the next hop is the tunnel underlay, not
> the SAM endpoint" and "a node cannot observe its peer's NIC holdings",
> respectively. Concentrating on a dedicated beacon community on the BGP best path
> resolves both, including the cold-start mutual-defer deadlock.

## Placement: deciding active/standby

Each `MobilityPool` member has `placement.group` and `placement.priority`.

- **group** — the unit that competes for active/standby (e.g. `azure-edge`).
- **priority** — a **lower number is higher priority**. Members left at `0`
  (unset) are auto-numbered `10, 20, 30, …` within the group by
  `autoPlacementPriorities`.

### The decision logic (`evaluatePlacementWithIncumbent`)

1. Order the non-drained members of the same group by **priority ascending, then
   nodeRef ascending**.
2. Take the head as the active candidate.
3. **No-preempt tie-break**: on an equal-priority tie, prefer the **current holder
   (incumbent)** over the lexicographic nodeRef winner, so a returning peer does
   not reclaim a live holder and cause a pointless handoff.
4. But a **strictly higher priority (lower number) member still reclaims** — the
   incumbent override applies only when the incumbent *shares* the top priority.

When `incumbentHolder` is empty the logic is pure priority/nodeRef ordering,
which is also how the group bootstraps before any holder is observed.

## Three mechanisms that reconcile no-preempt with failover

On top of the bare placement decision, three mechanisms suppress return-time
accidents and switch churn (all in `planner.go`).

### 1. Startup fence

```
placementSettleStart  = time.Now()        // captured at process start (resets on restart)
placementSettleWindow = 120 * time.Second
```

`placementSettleDefersActive` defers an active assertion only when all three
hold: **"about to assert active", "no incumbent peer observed yet", and "inside
the settle window"**. `fencePlacementForStartup` applies this, converting a
fenced active into standby.

- A just-returned node would otherwise win the equal-priority tie-break and
  reclaim holdership before its fresh BGP RIB / provider observations converge.
  The fence prevents this.
- A node past the settle window (a long-running standby) is not fenced, so **real
  failover is not delayed**: it seizes immediately when the active dies.
  This is a new-flow reachability guarantee after convergence; in-flight TCP
  sessions can still reset or stall across an abrupt active loss because SAM
  does not synchronize conntrack or transport state.

### 2. Holder retention

`applyHolderRetention` keeps a node active **while it physically holds its
group's captures (`selfHolds`)**. It applies when:

- the node is not already active,
- `selfHolds` is true,
- `yieldToHigherPriority` is false (see below), and
- the startup settle window has elapsed (so the fresh self-capture observation is
  trusted rather than a returning node's stale "I used to hold" memory).

Thus a live holder does not surrender ownership to a deterministic tie-break
winner or a transient peer observation (the ADR 0016 principle: **yield only on
losing your own holdership, never because a peer was observed**).

### 3. Unequal-priority auto-restore (`higherPriorityHolderActive`)

`higherPriorityHolderActive` returns true when the holder observed via the BGP
holder-beacon is a **strictly higher-priority peer (lower priority number)** than
self. It feeds the `yieldToHigherPriority` argument of `applyHolderRetention`.

- At **equal priority** it is always false → retention holds and the result is
  no-preempt.
- At **unequal priority**, the low-priority interim holder releases retention and
  **yields** once the high-priority node returns and starts emitting the beacon →
  the configured auto-restore proceeds.

The handover moves `/32`s one at a time, so the dataplane never dips.

## Fencing: rejecting stale provider operations

Provider operations (secondary-IP assign/unassign, etc.) carry the **mobility
path signature (`mobilityPathSig`)** at generation time, plus the desired holder
and the observed provider/journal transition. On reconcile, **operations whose
desired BGP path no longer matches are skipped**. The old ownership/capture epoch
tables are gone.

Seize (the takeover during failover) has dedicated hold-downs:

- `bgpSeizeLivenessMissingHold = 30s` — suppress seize when the liveness marker
  is missing
- `bgpProviderMissingRetryHold = 30s` — suppress retry when the provider
  observation is missing
- `bgpTrapRIBMissingHold = 2m` — retention when the trap route is absent from the
  RIB

## Dynamic RR sync is fail-static

RR nodes may publish `SAMPeerGroup` and `MobilityMemberSet` resources over the
TCP 19652 sync endpoint so leaves can bootstrap their transport peers and shared
member topology. Those fetched resources are saved as dynamic config parts with
ordinary TTLs:

- `peer-group-sync/<name>` for `SAMPeerGroup`
- `member-set-sync/<name>` for `MobilityMemberSet`

TTL expiry does not mean the data plane should be dismantled. If a leaf has a
previously fetched record and the RR publisher disappears, routerd treats the
expired record as **last-known-good** input, marks the source `Stale`, and keeps
the generated tunnel, BGP peer, and MobilityPool planning artifacts rendered.
Only a source that has never been seen remains `Pending`. This keeps a route
reflector outage from cascading into leaf transport teardown; the stale marker is
an operator signal that topology freshness is no longer being refreshed.

## Capture strategies (how cloud ingress is built)

`capture.captureStrategy` selects how the cloud ingress is built.

| strategy | providers | behavior |
| --- | --- | --- |
| `secondary-ip` (default) | AWS / Azure / OCI / GCP | assign the `/32` to the NIC as a secondary IP |
| `route-table` | Azure | point a UDR entry at the holder's NIC (`NextHopType=VirtualAppliance`) |
| `proxy-arp` | on-prem | capture on the L2 segment via proxy-ARP/GARP |
| `addr-add` | (generic) | add the OS address |

The `route-table` strategy requires `capture.target.nextHopIPAddress` on Azure.

**Same-subnet constraint ([#516](https://github.com/imksoo/routerd/issues/516),
live-validated 2026-06-16):** CloudEdge SAM's primary use case is same-subnet
lift-and-shift, where the mobility prefix falls inside the VPC/VNet/VCN CIDR.
Under this constraint, only Azure UDR accepts intra-subnet `/32` routes. AWS
rejects VPC-internal `/32` destinations with `InvalidParameterValue`, and OCI
rejects intra-subnet rules with `InvalidParameter: Intra-subnet/vlan rule is
not supported`. Consequently `route-table` is effective only on Azure for
same-subnet deployments.

| Cloud | Same-subnet `/32` route | Recommended strategy |
| --- | --- | --- |
| **Azure** | UDR accepted | `secondary-ip` (few addresses) or `route-table` (many addresses, UDR limit 1000) |
| **AWS** | Rejected by VPC API | `secondary-ip` only; scale via N+1 instance distribution ([#352](https://github.com/imksoo/routerd/issues/352)) |
| **OCI** | Rejected by VCN API | `secondary-ip` only (VNIC limit ~31) |

Every capture is accompanied by a **forwarding-enable** action so the NIC can
forward packets that are not addressed to itself.

## On-prem LAN authority is unchanged

BGP decides **remote overlay reachability**, but it does not replace the local
L2/ARP authority. On the on-prem side, the following remain in force as local
safety mechanisms:

- VRRP-master gating,
- proxy-ARP / GARP,
- non-master fail-closed behavior, and
- the duplicate-holder doctor check.

## Graceful stop (make-before-break handover)

`routerd serve --graceful-stop-timeout` (default `20s`) makes a node, on
SIGTERM/SIGINT, **wait up to this long for the mobility make-before-break
handover**. `0` disables it. On a planned restart, the new holder establishes its
advertisement before the old holder steps down, avoiding a dip.

## Status fields

A `MobilityPool` status surfaces placement-related observations:

- `placementActive` — whether self is active for this group
- `placementActiveNode` — the group's active node
- `placementGroup` — the group name
- `livenessMarkers` — observed peer liveness markers (node-identity communities)

These are visible via the `routerctl doctor` SAM diagnostics and `routerctl
show`.

## Behavior observed on real hardware (for reference)

Measured on an unequal-priority pair (priority 10 vs 20, Azure hardware):

- **A1 failover**: stop the high-priority node → the low-priority node seizes all
  three `/32`s in about 132 seconds → new-flow dataplane recovery.
- **A2 restore**: bring the high-priority node back → it reclaims the three `/32`s
  one at a time (no flapping). Client ping at 1-second intervals during the
  reclaim had **0% loss**.
- For an equal-priority pair, no-preempt held for 561 seconds with no holder
  swap, no split, no dip, and no cold-start deadlock.
- A long-lived throttled HTTP transfer that was already active before an abrupt
  active leaf stop is tracked separately from the normal failover matrix. It can
  time out even when post-convergence ping, SSH, tracepath, and fresh HTTP
  transfers pass through the standby. Treat this as expected reset/retry
  semantics until a future session-continuity design explicitly changes it.

## Related

- [What is CloudEdge SAM](../concepts/cloudedge-sam.md) — concepts and new terms
- [Selective Address Mobility](selective-address-mobility.md) — the `MobilityPool` config model
- [ADR 0012: BGP /32 Address Mobility](../adr/0012-bgp-address-mobility.md) — the decision to make BGP the source of truth
- [ADR 0008: Capture Coordination via Fencing](../adr/0008-capture-coordination-fencing.md) — background on fencing
- [provider action execution](provider-action-execution.md) — the approval/execution gate for provider operations
