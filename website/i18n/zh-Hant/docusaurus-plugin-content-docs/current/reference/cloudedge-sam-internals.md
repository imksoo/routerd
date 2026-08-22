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
  preference changes**. Operators never hand-author leases, per-address
  ownership records, or provider actions.
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
| `64512:120` | `…CommunityFailover` | a seize advertisement during failover |
| **`64512:121`** | **`…CommunityActiveHolder`** | **holder-beacon: attached only by the active holder** |
| (per node) | node-identity community | identifies which node advertised (derived from nodeRef) |

LOCAL_PREF is set relative to `bgpMobilityLocalPrefBase = 200`, so an active
advertisement carries a higher preference than a standby's make-before-break
advertisement.

### The holder-beacon (`64512:121`) is the linchpin

`bgpMobilityPathAttrs` (`controller.go`) attaches `64512:121` only when the
advertisement is from an **active holder**.

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

Each `SAMNodeSet` node has `placement.group` and `placement.priority`; the
MobilityPool imports that shared placement through `membersFrom`.

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

`fencePlacementForStartupWithReadiness` defers an active assertion when all
three hold: **"about to assert active", "no incumbent peer observed yet", and
"startup readiness is not complete"**. Readiness means the local BGP control
plane has completed an initial observation and, for provider-inventory-backed
captures, provider self-observation has completed. `placementSettleWindow`
remains as a conservative fallback for callers that do not provide readiness
signals. When readiness is known but remains incomplete, the fence is bounded:
after `placementSettleWindow * 3` (360 seconds by default) routerd releases the
active assertion even while readiness remains incomplete, so overlay liveness is
not blocked forever by a provider API or observation failure.

- A just-returned node would otherwise win the equal-priority tie-break and
  reclaim holdership before its fresh BGP RIB / provider observations converge.
  The fence prevents this.
- A node whose BGP/provider observations have completed can leave the fence
  before the wall-clock window expires, which avoids crash-loop nodes remaining
  artificially passive after every restart.
- A node whose observations are still incomplete remains fenced even after the
  wall-clock window, so a partitioned or blind node does not assert active merely
  because time elapsed.
- A node that already observes an incumbent peer is not fenced; the normal
  no-preempt placement tie-break already defers to that holder.

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

RR nodes may publish `SAMPeerGroup` resources over the TCP 19652 sync endpoint
so leaves can bootstrap their transport peers. Fetched peer groups are saved as
dynamic config parts with ordinary TTLs:

- `peer-group-sync/<name>` for `SAMPeerGroup`

TTL expiry does not mean the data plane should be dismantled. If a leaf has a
previously fetched peer group and the RR publisher disappears, routerd treats
the expired record as **last-known-good** input, marks the source `Stale`, and
keeps the generated tunnel and BGP peer rendered. Only a source that has never
been seen remains `Pending`. MobilityPool membership is resolved from static
`SAMNodeSet` configuration. The stale marker is
an operator signal that topology freshness is no longer being refreshed. Status
also includes a `warning` field on stale sources so long-lived fail-static mode
is visible without tearing down the working data plane.

當 policy 設定了 `directMesh.peerGroupRef`，並且 leaf 的 claim 已簽署且選擇了
`directMesh: true` 時，RR 會把第二個、帶 policy 範圍的 `SAMPeerGroup` 放進與
`SAMRRSet` 相同的 dynamic-config part。該 group 只包含符合資格的遠端 leaf，必須符合
本機 transport fingerprint，並帶有由簽署 claim 投影的 IPv4 `/32` 清單；尚未擁有位址的
leaf 的清單可以是空的。

這個 direct group 是選用的加速器，不是新的 L2 overlay。`SAMTransportProfile` 保留
`SAMRRSet` source，再把 direct group 作為最後一個 `direct: true` source 加入。只有 direct
BGP session 存活時，它的路由才會取得比 RR 更高的 `LOCAL_PREF`。若 group 缺失、過期、
不相容或 underlay 無法到達，routerd 不會建立該 direct artifact，仍使用 RR peer。這樣 RR
同時負責啟動與安全備援。

剛加入時，已簽署 leaf 的 `mobility.ownedAddresses` 可以是空的，這是正常狀態。routerd 仍會
建立已驗證的 direct BGP session，但會為該鄰居加入明確的 **全部拒絕** import 規則：空 claim
不會通告 mobility route，也不會從這條 direct link 接收 route。之後出現已簽署的 `/32` 時，
才只允許那一個位址並使用 direct preference。這樣不用虛構 IP，準備期間的通信仍安全地經由
RR 轉送。

## Capture strategies (how cloud ingress is built)

`capture.type` selects the normal ingress mechanism. `capture.captureStrategy`
is only an explicit `route-table` override for `provider-secondary-ip`.

| configuration | providers | behavior |
| --- | --- | --- |
| `type: provider-secondary-ip` | AWS / Azure / OCI / GCP | assign the `/32` to the NIC as a secondary IP |
| `captureStrategy: route-table` | Azure | point a UDR entry at the holder's NIC |
| `type: proxy-arp` | on-prem | capture on the L2 segment via proxy-ARP/GARP |

Current release lab certification covers `secondary-ip` capture only. The
`route-table` strategy is **uncertified**. On Azure it requires
`capture.target.nextHopIPAddress`, and routerd waits for provider inventory to
observe the UDR pointing at the local router before advertising the captured
`/32` to BGP. This coupling is specific to `route-table`; ARM/provider API
latency can delay overlay convergence for this strategy. `secondary-ip` capture
is not gated on route-table observation.

認證不會採用 write-accepted gate。route-table write 被接受只證明 provider API
接受了 mutation；它不能證明實際生效的 route table 已經把該 `/32` steering 到本地
router。若在 write acceptance 後立即廣告 BGP，retry、throttling 或 inventory
propagation 延遲造成的 write-to-observation window 中可能出現 black-hole。更安全的契約是
provider 已觀測到 ingress 後再發布 overlay advertisement。

本 release 中已認證的 hybrid strategy 仍是 `secondary-ip`。它同樣透過 provider self
inventory 確認，但觀測對象是 NIC secondary-address attachment，而不是 route-table entry。
將來若要認證 `route-table`，在移除 uncertified 標記前必須包含 large-pool behavior、
failover rewrite ordering、inventory UDR 解析，以及 ARM/provider delay 或 throttling 證據。

Every capture is accompanied by a **forwarding-enable** action so the NIC can
forward packets that are not addressed to itself.

## Provider split-brain reconciliation

BGP remains the control-plane truth for overlay reachability, but provider
inventory can temporarily report the same `/32` as owned in more than one cloud
fabric after a partition. When fresh provider-discovery facts disagree, the
ownership resolver marks the address `Conflict` with
`conflictReason=duplicate-provider-home-owners` and includes all observed owners.

The resolver also records a deterministic `conflictWinnerNode`:

- if the healed BGP RIB has a home-owner path for the `/32`, that BGP owner wins;
- otherwise the lowest stable owner key wins (`nodeRef`, provider ref, resource
  ref, NIC ref, subnet ref, then address), independent of provider scan recency.

Losers do not create new provider capture actions. If the losing node observes
the conflicting `/32` still attached to its own provider-secondary capture, the
status records `conflictResolution=loser-release-local-capture`; after the same
stale-capture hold-down used for trap cleanup, routerd emits a scoped
`unassign-secondary-ip` for that local capture. Nodes that do not hold a local
capture report `loser-withhold-local-capture`. The winner reports
`winner-retain-local-capture`.

The generated RR-client admission policy is a route-admission boundary, not a
per-address authorization system. It requires the advertising node's identity,
forbids other topology node identities, and limits accepted routes to `/32`s
inside the declared MobilityPool prefixes. A compromised leaf can still advertise
a pool-local `/32` with its own identity; preventing that requires an additional
ownership authorization signal outside this BGP filter.

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
  three `/32`s in about 132 seconds → full dataplane recovery.
- **A2 restore**: bring the high-priority node back → it reclaims the three `/32`s
  one at a time (no flapping). Client ping at 1-second intervals during the
  reclaim had **0% loss**.
- For an equal-priority pair, no-preempt held for 561 seconds with no holder
  swap, no split, no dip, and no cold-start deadlock.

## Related

- [What is CloudEdge SAM](../concepts/cloudedge-sam.md) — concepts and new terms
- [Selective Address Mobility](selective-address-mobility.md) — the `MobilityPool` config model
- [ADR 0012: BGP /32 Address Mobility](../adr/0012-bgp-address-mobility.md) — the decision to make BGP the source of truth
- [ADR 0008: Capture Coordination via Fencing](../adr/0008-capture-coordination-fencing.md) — background on fencing
- [provider action execution](provider-action-execution.md) — the approval/execution gate for provider operations
