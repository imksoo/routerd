# ADR 0008: Capture Coordination via Fencing Tokens (epoch-fenced level projection)

## Status

Proposed; Accepted for experimental implementation — 2026-05-31.

This ADR builds on [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md),
[ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md), and the
[Selective Address Mobility](../reference/selective-address-mobility.md) dataplane.
It is **experimental**.

It supersedes the de-provision mechanism introduced as the "durable de-provision
marker" fix (commit 26f2a729, issue #70): that fix made the unassign **durable**
but kept an **imperative cancel** path (cancel the in-flight de-provision when the
address is desired again). That cancel path is non-deterministic — it races
reconcile timing against execution — and could not be made flaky-free by patching
where it joins state. This ADR replaces it with an **epoch-fenced level
projection**.

## Context

A mobile `/32` in Selective Address Mobility is a **uniqueness-constrained shared
resource that must have exactly one capture holder** at any instant (the
single-owner invariant of any-origin symmetric arbitration). "Holding" the address
means owning its physical capture: a provider **secondary IP** assignment on a
cloud NIC (AWS ENI / Azure NIC / OCI VNIC), or **proxy-ARP + GARP** on-prem.

Capture moves between holders in two ways:

- **Graceful / planned** — maintenance drain; the active holder cooperates.
- **Abrupt / fault** — the holder's host dies or is partitioned; it *cannot*
  cooperate, and a standby must seize the capture.

The de-provision (unassign secondary IP / disable forwarding) is **releasing** the
capture; assign is **acquiring** it. The bug surfaced as a flaky test
(`TestServeChainMobilityCancelsPendingDeprovisionWhenDesiredAgain`, ~3/30 failures
without `-race`): on re-capture the in-flight de-provision was sometimes not
cancelled, leaving an orphaned marker / pending action. Patching the cancel join
target did not remove the flakiness, because **imperative cancellation of in-flight
work is the wrong abstraction** for a level-triggered reconciler.

### Theory consulted (distributed coordination)

- **Fencing tokens** (Kleppmann, *How to do distributed locking*): a lease/lock
  with TTL is necessary for *liveness* (a dead holder's lease expires so a standby
  can take over) but **insufficient for *safety*** — a paused / delayed / revived
  ("zombie") old holder can still act after its lease expired. "You cannot fix this
  by checking expiry just before writing." The only fix is a **monotonically
  increasing fencing token** that the **protected resource** checks, rejecting any
  operation carrying a token lower than the highest it has seen.
- **Generation / term / epoch**: Raft *term*, ZooKeeper *epoch* / *zxid*, etc. are
  the same monotonic fencing token, used to **fence zombies** and reconcile diverged
  state. "Downstream systems must reject operations with stale epochs."
- **Level-triggered reconciliation** (Kubernetes controllers): reconcile to desired
  state from observed state every tick; **idempotent**; do not act on edges. Edge
  logic ("on re-desire, cancel X") grafted onto a level loop races.
- **Split-brain / HA failover** (Pacemaker STONITH, keepalived VRRP + EC2
  `AssignPrivateIpAddresses`): a floating IP is held by exactly one master
  (IPaddr2 + GARP); STONITH guarantees the old node is down before takeover; the
  heartbeat interval trades detection latency against split-brain risk — but **never
  provides safety**, which comes from fencing/quorum.

### The routerd-specific constraint

The "protected resource" here is the **cloud provider API and the on-prem ARP
table**, neither of which natively checks a fencing token — AWS will not reject an
"unassign with epoch 33" just because epoch 34 already happened. **Fencing cannot be
pushed all the way to the real resource.** routerd must enforce the fence at the
**last gate it controls**: the action import / executor boundary (the "fencing
proxy" pattern).

## Decision

### 1. `captureEpoch` — a per-(pool, address, captureDomain) monotonic fencing token

A persisted, **strictly monotonic local counter** keyed by
`(pool, address, captureDomain)`, incremented every time the **desired capture
holder** changes — *including* re-capture back to a prior holder. It is **distinct
from the `AddressLease` epoch**:

- `AddressLease` epoch = epoch of the **location owner** (who owns the address).
- `captureEpoch` = epoch of the **physical capture holder** (who attaches the
  secondary IP / answers proxy-ARP).

These are different lifecycles and must not be conflated. **Wall-clock time
(`now`) must never be used as the token** — it is non-monotonic across nodes and
churns, which was the latent defect in the superseded fix. `captureDomain` is the
placement-group scope (`provider:<ref>:placement:<group>`) so that all routerds
contending for the same address within one provider group share one epoch line.

### 2. Stamp every provider action with `(captureEpoch, captureKey, holder)`

The planner stamps `assign-secondary-ip`, `unassign-secondary-ip`, and the
forwarding actions with their `captureEpoch`, `captureKey`, and the holder the
action is *for* (acquire → the desired holder; release → the vacating node). The
`idempotencyKey` is suffixed with `:epoch:<N>`, so an action for capture epoch N is
a distinct, stable key from epoch N+1 — and **stable across reconciles within the
same epoch** (no churn).

### 3. De-provision intent is a level projection, not a work queue

The set of de-provision work = a **projection** of *(previously-captured −
currently-desired)* evaluated at the current `captureEpoch`, recomputed every
reconcile. Re-capture does **not** "cancel" anything: the address re-enters desired
state, so it falls out of the projection, and the `captureEpoch` bumps. There is no
imperative cancel path.

The **durable marker table is retained as an outbox** (a `DynamicConfigPart` alone
loses the intent before import — the original #70 failure). But a marker is now an
**epoch-keyed projection item**, not a cancellable edge state; stale markers are
dropped by the same fence (`dropStaleDeprovisionMarkers`).

### 4. Fence at the import / executor gate

Before importing a provider action for address X, and when sweeping the journal,
compare its `captureEpoch`/holder to the **current** `captureEpoch` for X:

- epoch mismatch with current, **or** an acquire whose holder is no longer current,
  **or** a release whose holder is still current → the action is **stale** → it is
  skipped (fenced), and an already-imported pending/approved stale action is marked
  `skipped`. A revived old reconcile that tries to resurrect a superseded marker
  carries the old epoch and dies at this gate.

This single deterministic gate **replaces** the scattered
`cancelMarkerPlansForDesired` / `CancelActionByIdempotencyKey` cancel logic.

### 5. What makes it safe — and the honest limits

- **Within a node**: the local `captureEpoch` gate is monotonic and serial in the
  node's reconcile loop; it deterministically fences stale local reconciles. This
  is what removes the #70 flakiness.
- **Across nodes** (correction to an earlier overstatement — the per-node DB gate
  is **not** cross-node linearizable): safety is **structural**, the combination of
  (a) the provider's **single-assignment semantics** — a secondary IP lives on
  exactly one NIC — plus (b) **acquire-with-reassignment** (AWS
  `assign-private-ip --allow-reassignment` atomically *moves* the IP, rather than
  waiting for the dead holder to release — release-before-acquire would forfeit
  liveness on host failure) plus (c) **NIC-scoped** stale operations (an old
  holder's `unassign` targets only its own NIC, so it cannot strip the new holder's
  NIC).
- **On-prem proxy-ARP is weaker** and must not be dressed up as cloud-equivalent:
  there is no atomic reassignment. Safety there rests on **VRRP/keepalived master
  state as the capture authority** — inactive nodes are **fail-closed** (no
  proxy-ARP, no route lowering), only the master emits proxy-ARP + GARP. Full
  safety under partition is not achievable without STONITH / quorum, which is out
  of scope.
- **Liveness vs safety budget**: lease TTL / heartbeat interval tunes *detection
  latency* (too short → flap; too long → slow recovery), mirroring keepalived
  `advert_int` and the existing `deprovisionHoldDuration` hysteresis. **Safety must
  never depend on this knob** — only the monotonic `captureEpoch` provides it. This
  is the Kleppmann lesson made concrete.

## Phasing

- **Phase A (this ADR's minimal scope — fixes #70 deterministically)**: introduce
  `captureEpoch`; stamp actions; make markers an epoch-keyed level projection;
  import-time fence on stale epoch / holder mismatch; **remove** the cancel paths
  and the wall-clock lifecycle key. Acceptance:
  `TestServeChainMobilityCancelsPendingDeprovisionWhenDesiredAgain` passes
  `-count=100` (and `-race`) deterministically, the assertion relaxation (`< 2`) is
  removed in favour of exact deterministic counts, the re-emit test stays green, and
  no test is loosened to pass.
- **Phase B (later)**: execute-time gate (in addition to import-time) for abrupt
  seize.
- **Phase C (later — the failover feature)**: **liveness-driven placement** — drive
  activation by lease TTL / heartbeat rather than only the `maintenance.drain` flag,
  so an abrupt host fault triggers a standby **seize** (acquire-with-reassignment),
  fenced against zombie revival. This is the cloud analogue of D4 (on-prem VRRP
  failover) and is what turns drain-only migration (D5) into transparent
  host-maintenance and physical-host-failure failover on AWS / Azure / OCI.

## Consequences

- The flaky de-provision/re-capture race is removed at the abstraction level, not
  papered over: one deterministic epoch-fenced computation replaces scattered
  imperative cancellation.
- routerd gains a principled fencing token (`captureEpoch`) that the same gate can
  later use for abrupt-failover seize — the #70 fix and the failover feature share
  one mechanism.
- The design is explicit that **cloud capture is strongly safe** (provider
  single-assignment + reassignment + NIC-scope + epoch) while **on-prem proxy-ARP
  is best-effort** (VRRP master authority + fail-closed + GARP), rather than
  implying parity.
- It stays within simplicity-first: no consensus protocol (Paxos/Raft) is
  introduced; a per-address monotonic counter + a single fence gate is the whole
  coordination surface.
- Fixing the `-race` acceptance bar also surfaced and fixed a pre-existing event-bus
  data race (publish racing an unsubscribe's channel close); see the companion
  `fix(bus)` commit.
