# ADR 0010: Capture Ownership Arbitration (multi-instance ownership map + ownershipEpoch fencing)

## Status

Proposed; Accepted for experimental implementation — 2026-06-01.

Builds on [ADR 0008: Capture Coordination via Fencing Tokens](../adr/0008-capture-coordination-fencing.md)
and the [Selective Address Mobility](../reference/selective-address-mobility)
dataplane. Addresses issue #76. Its consumer is
[ADR 0011: Generalized Failover](../adr/0011-generalized-failover.md) (#74).
Experimental.

## Context

At scale a single cloud router cannot hold every captured secondary IP (ENI/NIC/
VNIC slot limits), so an `N+1` set of same-provider routers must **distribute**
captured addresses. Today routerd has **no cross-node ownership map and no mutual
exclusion**:

- Coordination is **single-node local projection**: every node independently
  projects the same federation event stream to the same `AddressLease` state
  (`pkg/controller/mobility/controller.go`). There is **no distributed lock,
  quorum, or consensus**.
- "Single owner" is *implicit* (capturePolicy `all-non-owner-sites` + deterministic
  `evaluatePlacement`), and `captureEpoch`
  (`pkg/state/mobility_capture_epoch.go`) is a **per-node, per-(pool,address,
  captureDomain)** monotonic token that fences stale provider actions at the
  import/execute gate (ADR 0008).
- The reserved `MobilityPoolSpec.Authority` field is unused.

#76 asks for a centralized ownership map, conflict exclusion, and split-brain
prevention. ADR 0008 deliberately **avoided consensus** (Paxos/Raft/etcd) and built
safety from a monotonic fencing token + the provider's structural single-assignment
+ idempotent convergence. This ADR keeps that philosophy.

### What "ownership" can and cannot guarantee without consensus (honest scope)

This is **not** a linearizable distributed lock. With event-ordered arbitration +
fencing + the cloud's single-assignment semantics we guarantee:

1. all nodes that see the same event stream **converge to the same owner map**;
2. a node that has seen ownershipEpoch *N+1* will not execute an epoch-*N* action
   (fenced at the gate);
3. a cloud secondary IP can belong to exactly one NIC, so provider state
   **converges to a single assignment**.

We **cannot** guarantee that an old owner that is still alive but partitioned from
federation (has not seen *N+1*) never re-grabs the address via the provider API —
eliminating that needs consensus / STONITH / provider conditional-fencing, which we
do not add. So the property is **"fenced eventual ownership + provider-enforced
single assignment,"** not "split-brain prevention." On-prem **proxy-ARP** is weaker
still (no provider single-assignment): the cap there is VRRP-master authority +
fail-closed (per ADR 0008).

## Decision

### `ownershipEpoch` — a per-(pool, address) cluster fence token

Introduce **`ownershipEpoch`**, a higher-level concept than `captureEpoch`: a
per-(pool, address) monotonic token that increments **only on a confirmed owner
change** (not while a lease is candidate/holding). It is the fence token that spans
cloud / on-prem / provider / action. `captureEpoch` is retained as a
compatibility/derived annotation; the source of truth moves to `ownershipEpoch`.

### Ownership map — leader-less deterministic convergence

There is **no elected leader** (leader election needs consensus). The ownership map
is a **converged view** each node builds deterministically from the federated event
stream:

- For each `(pool, address)`, the owner is chosen by a deterministic arbitration:
  **preferNodes → placement priority → stable tie-break** over the *eligible*
  members (eligibility defined by ADR 0011: not drained, healthy, live, VRRP-master
  where applicable).
- Multi-instance distribution: within a placement group, each address is arbitrated
  to one owner; the set of addresses spreads across the eligible members (future:
  least-loaded). One IP → one owner at a time.
- The map is **surfaced** (status DB + metrics + control/`routerctl`) so operators
  see "which IP is owned by which node" — the "centralized ownership map" #76 wants,
  realized as a converged view rather than a single-writer store.

### `ipOwnershipPolicy` on `MobilityPool`

```yaml
spec:
  ipOwnershipPolicy:
    type: centralized          # converged deterministic map (only mode)
    epochLocking: true         # stamp + fence actions by ownershipEpoch
    preferNodes: [aws-router-a, aws-router-b]
    autoFailover: true         # consumed by ADR 0011 (liveness-driven seize)
```

`preferNodes` biases arbitration; `epochLocking` enables ownershipEpoch fencing;
`autoFailover` is the hook ADR 0011 uses. `type` has one mode now (`centralized` =
converged-deterministic).

### Action idempotency key

Provider-action idempotency keys carry, at minimum, `pool / address / ownerNode /
ownershipEpoch / actionVerb / provider / nicRef` — so a stale-epoch or
wrong-owner action is fenced deterministically.

## Phasing (this ADR)

- **Phase 1 (this ADR's minimal scope)**: the `ownershipEpoch` token, the
  deterministic ownership record + arbitration (preferNodes/priority/tie-break),
  `ipOwnershipPolicy` spec + validation, and **ownership-map visibility** (status +
  metrics + `routerctl`). **No automatic seizure** — Phase 1 only *computes and
  exposes* desired ownership and fences actions by ownershipEpoch; the existing
  static placement still drives who acts.
- Liveness-driven failover/seize is **ADR 0011**.

## Consequences

- routerd gains a cluster-converged, fenced ownership model for distributing
  captured IPs across N+1 same-provider routers, without adding a consensus store.
- The safety scope is stated honestly ("fenced eventual ownership," not a
  distributed lock); cloud is structurally strong, on-prem is VRRP-authority
  best-effort.
- `ownershipEpoch` is the single cross-cutting fence token that ADR 0011's seize and
  Phase-4 cloud-inventory/drift detection build on.
