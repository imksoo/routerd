# ADR 0011: Generalized Failover (liveness-driven seize, cross-provider action parity)

## Status

Proposed; Accepted for experimental implementation — 2026-06-01.

Consumes [ADR 0010: Capture Ownership Arbitration](../adr/0010-capture-ownership-arbitration.md)
(ownership map + `ownershipEpoch`) and realizes the failover deferred as Phase C of
[ADR 0008](../adr/0008-capture-coordination-fencing.md). Addresses issue #74.
Experimental.

## Context

CloudEdge today only moves capture on **cooperative drain** (`maintenance.drain`);
there is **no liveness/health-driven promotion** and the per-provider actions
(assign/unassign secondary IP, forwarding) are AWS-only with Azure/OCI/on-prem
thin or absent. #74 asks for one failover framework across AWS / Azure / OCI /
on-prem (VRRP/keepalived) that keeps L3 continuity (the captured address keeps being
served by promoting a standby), with unified split-brain/flap defense.

ADR 0010 gives the ownership primitive (converged owner map + `ownershipEpoch`
fencing). This ADR adds the **liveness → desired-owner → seize** loop and the
**provider-agnostic action layer**.

### Provider reassignment semantics (researched — informs the seize)

- **AWS**: `assign-private-ip-addresses --allow-reassignment` moves a secondary IP
  to another ENI; **asynchronous** (confirm via instance metadata
  `local-ipv4s`), last-writer-wins, associated EIP moves with it.
- **OCI**: `assign-private-ip --unassign-if-already-assigned` force-reassigns to
  another VNIC in the same subnet; last-writer-wins; public IP moves with it.
- **Azure**: no single atomic reassign — **remove the ipConfig from the old NIC +
  add it to the new NIC** (two operations; optimistic concurrency via ETag/If-Match
  is available).

So reassignment is **not** universally atomic (AWS async, Azure two-op). Failover is
therefore **experimental and relies on provider assign semantics + `ownershipEpoch`
fencing + (Phase 4) cloud-inventory drift reconciliation** — not on a lock.

## Decision

### Unified eligibility & liveness model

The desired owner (ADR 0010 arbitration) is computed over **eligible** members,
where eligibility is the intersection of:

- `maintenance.drain == false` (drained → excluded immediately);
- **heartbeat fresh** — each member periodically emits a liveness/heartbeat
  federation event; an expired heartbeat (TTL) makes it ineligible **after a
  promotion hold** (see below);
- `HealthCheck` not failed (per policy);
- on-prem: **VRRP-master** authority signal (`activeWhen{vrrp-master}`,
  `sam.EvaluateCaptureGate`) — non-master is fail-closed.

Because TTL is evaluated against each node's own clock (`projectionNow` differs),
promotion uses a **promotion hold** (`heartbeatTTL + holdDuration`) to absorb clock
skew and suppress flapping. Drain is immediate; failure-driven promotion waits the
hold.

### Liveness-driven seize

When the eligible-owner changes (drain, heartbeat expiry, health failure), the
`ownershipEpoch` bumps and the **new owner seizes**: it issues the provider
acquire-with-reassignment for the secondary IP and enables forwarding; the old
owner's actions carry the stale epoch and are fenced at the gate. `autoFailover`
(ADR 0010 `ipOwnershipPolicy`) gates whether this is automatic.

### Provider-agnostic action layer

- The **planner emits provider-agnostic ownership/action intent** (a desired
  `(owner, address, verb)` set + `ownershipEpoch`); the **executor holds the
  provider difference** (AWS `--allow-reassignment`, OCI
  `--unassign-if-already-assigned`, Azure remove+add). This is the common
  `ActionPlan` + executor contract (already used for AWS), generalized.
- **On-prem is not a cloud provider**: its "action" is local dataplane
  (proxy-ARP/GARP/VIP), so it is handled as an on-prem executor / SAM-GARP bridge,
  **not** modeled as a provider-API call.

## Phasing (this ADR)

- **Phase 2**: cloud liveness failover — heartbeat events + TTL + promotion hold +
  unified eligibility, `ownershipEpoch` bump, **cloud secondary-IP seize** (AWS
  first, the proven path), `autoFailover` gate. Forced-failure CI/lab test that L3
  does not break (the standby serves the address after promotion).
- **Phase 3**: provider action parity — Azure (remove+add ipConfig) and OCI
  (`--unassign-if-already-assigned`) executors; on-prem VRRP/GARP integration via an
  on-prem executor / SAM bridge so VRRP/keepalived failover is covered by the same
  policy.
- **Phase 4**: cloud-inventory observe capability (`describe-secondary-ips`) →
  drift/orphan/conflict detection surfaced in status + `doctor`, hardening the
  experimental seize into reconciled ownership; management API for the ownership map.

## Consequences

- One failover framework spans the providers: liveness/health/maintenance/VRRP feed
  a unified eligibility model; the planner is provider-agnostic; per-provider
  reality is confined to executors.
- L3 continuity is achieved by promoting a standby + seizing the captured IP, fenced
  by `ownershipEpoch`; the honest limit (no consensus, provider reassignment not
  universally atomic) is documented, with cloud inventory (Phase 4) closing the
  drift gap.
- On-prem is integrated without being forced into the cloud-provider mold.
