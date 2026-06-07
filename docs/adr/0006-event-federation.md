# ADR 0006: CloudEdge Event Federation (routerd-to-routerd typed events)

![Diagram showing ADR 0006 Event Federation from hand-authored claim problem through EventGroup, EventPeer, and EventSubscription decisions to observed-fact invariants](/img/diagrams/adr-0006-event-federation.png)

## Status

Accepted; experimental implementation in progress — 2026-05-30.
Phases 1, 1.5, 2, and 3 are **implemented on `event-federation`**:

- **Phase 1** (event envelope + `EventGroup` Kind + SQLite local store + `routerctl
  federation event emit/list`) — done.
- **Phase 1.5** (`EventPeer`/`EventSubscription` Kinds + validation) — done.
- **Phase 2** (peer delivery over the overlay via `routerd-eventd`, HMAC, retry,
  retention prune) — done; **lab-smoke PASS**
  ([transport evidence](../releases/evidence/cloudedge-event-federation-transport-20260530.md)).
- **Phase 3** (subscription → plugin → `RemoteAddressClaim` `DynamicConfigPart`) —
  done; **lab-smoke PASS**
  ([subscription evidence](../releases/evidence/cloudedge-event-federation-subscription-20260530.md),
  [how-to](../how-to/event-federation-subscription.md)).

Phase 4 (provider `actionPlan` plugins, dry-run) is **next, not started**.
Phase 5 (provider action execution) is **out of MVP**.

## Context

SAM ([reference](../reference/selective-address-mobility),
[milestone](../releases/cloudedge-sam-mvp-milestone.md)) is clean-validated
on Azure×PVE, AWS×PVE, and OCI×PVE (3-cloud parity). It proves the
**capture (provider-specific) / delivery+claim (routerd-common)** split. But the
`RemoteAddressClaim` that drives it is **hand-authored** today. The next step is to
discover, propagate, and materialize claims **event-driven**:

> on-prem routerd observes a client IPv4 (ARP/Clients/DHCP) → emits a typed event →
> federation bus delivers it to the cloud routerd → a subscription triggers a
> provider plugin → plugin returns a `RemoteAddressClaim` as a `DynamicConfigPart`
> (+ a provider secondary-IP `actionPlan`) → cloud is ready for `provider-secondary-ip`
> capture, with **no human editing the cloud config**.

### What already exists (so the MVP is *not* greenfield)

Grounding the design in the current tree — most building blocks are present; the
genuinely new work is the **node-to-node federation transport** and the
**event→plugin subscription trigger**:

- **Typed event envelope**: `pkg/daemonapi` `DaemonEvent{Type,Time,Daemon,Resource,
  Severity,Reason,Message,Attributes}` + `NewEvent(...)`. Today it flows
  daemon→main, but it is already a typed topic-carrying envelope.
- **Daemon→routerd transport pattern**: daemons POST to the control socket over
  HTTP-over-unix (`cmd/routerd-dhcp-event-relay` → `controlapi.Prefix +
  /dhcp-lease-event` via `unix:/run/routerd/routerd.sock`). There is even an
  *event-relay daemon precedent*.
- **Separate long-lived daemon precedent**: 13 `cmd/routerd-*` daemons
  (`routerd-bgp`, `routerd-ra-observer`, `routerd-dhcp-event-relay`, …). The
  gobgp pivot (ADR 0004) established "separate long-lived process over in-process"
  to avoid restart-drops.
- **Plugin → DynamicConfigPart pipeline**: `pkg/plugin/runner.go`,
  `pkg/plugin/dynamic_config.go`, `pkg/dynamicconfig/{types,merge}.go`,
  `PluginRequest`/`PluginResult`. effective = startup + active dynamic − masks.
- **State**: SQLite (`pkg/state/sqlite.go`).
- **Provider profile + external auth**: `CloudProviderProfile` with
  `auth.mode=external-command` (specs.go:1193) — already the hook for
  provider-specific plugins. `provider: oci|aws|azure|gcp` already validated.

## Decision

Build **CloudEdge Event Federation** as the next experimental MVP, on a new branch
on top of merged-experimental SAM. **Do not cut scope — decompose into ordered,
independently-acceptable phases and drive each phase as a workflow.** Each phase
ships a working, demoable slice and gates the next.

### Design principles

1. **Events are observed facts, not config.** A node sends
   `routerd.client.ipv4.observed`, never a raw `RemoteAddressClaim`. The receiver's
   *trusted local plugin* decides whether/how to turn it into a typed claim +
   actionPlan. The wire never carries commands to execute.
2. **at-least-once + idempotent**, not exactly-once. Store idempotency is keyed on
   the event `id` (a duplicate `id` is a no-op insert); `dedupeKey` is a
   subscription-side grouping key for collapsing repeated observations of the same
   fact, **not** a DB unique constraint in Phase 1. Dynamic resource names are
   deterministic (`onprem-10-88-60-9`); provider actions are no-op if already
   satisfied. No consensus, no gossip, no total ordering.
3. **Reuse, don't reinvent.** Reuse the `DaemonEvent` envelope, the control-socket
   HTTP transport idiom, the Plugin→DynamicConfigPart pipeline, SQLite state, and
   `CloudProviderProfile`/`Plugin` (no new `CloudProviderPlugin` Kind).
4. **Minimize new Kinds.** MVP introduces **three**: `EventGroup` (bus identity +
   auth + retention), `EventPeer` (delivery target + inline push/receive filters),
   `EventSubscription` (received-event → local plugin trigger). Fold the proposed
   standalone `EventFilter` into `EventPeer` for now; promote to its own Kind only
   if filters need to be shared across peers.
5. **Separate daemon.** Federation send/receive lives in a new
   `cmd/routerd-eventd` long-lived daemon (per ADR 0004 precedent), not the
   reconcile loop. It binds to the overlay (`wg-hybrid`) only.
6. **Provider mutation stays dry-run in the MVP.** Plugins emit `actionPlan`s;
   execution is a later phase behind an explicit approval/auto-apply policy.

### Transport & security (MVP)

- Receiver = HTTP listener **bound to the WireGuard overlay interface/address only**
  (e.g. `169.254.x.y:9443`). The WG tunnel is the confidentiality boundary; add
  **message-level HMAC** (shared secret from file) for integrity/anti-misroute.
  **Defer TLS** — a TLS listener needs cert provisioning, which reintroduces exactly
  the bootstrap friction the SAM stocktake flagged. (Future: mTLS / per-peer Ed25519
  / cloud-KMS signing.)
- Push-only for MVP (`onprem→cloud` observations; `cloud→onprem` claim/result acks).
- Retry with backoff; per-(event,peer) delivery status in SQLite.

### Critical invariants to review at the state-machine level (not just diff)

Per the project's rule for out-of-process stateful daemons, these are the
correctness conditions, stated as invariants:

- **No feedback loop.** A node MUST NOT re-emit `*.observed` for an address it itself
  *captures* (provider-secondary-ip or proxy-arp). Observation is scoped by
  `ownerSide` + `domain`; captured/secondary addresses are excluded from the
  observer's source set. Without this, cloud's own secondary `.9` gets re-observed →
  re-propagated → flap.
- **Asymmetric provision vs de-provision.** Provisioning (claim appears) may be
  prompt. **De-provisioning (TTL expiry / `*.expired`) must be hysteretic** — a much
  longer grace + debounce than the 300s observe TTL — because a flapping client must
  not drive repeated cloud secondary-IP assign/unassign (API rate limits + cost +
  dataplane churn). TTL→teardown policy is explicit and conservative.
- **Single writer per (domain, address).** The owning side is authoritative; the
  receiver only ever proposes a claim for an address whose `ownerSide` is the *sender*.
- **Idempotent provider actions.** "already assigned" ⇒ success/no-op across
  aws/azure/oci.

### Provider plugin framework

OS-CLI-invoking local executables, **not** SDKs statically linked into routerd
(keeps SDK churn/auth out of core; enables cloud-native identity; easier debug):

- **AWS**: `aws ec2 assign-private-ip-addresses` — auth: **IAM instance profile**
  first, `AWS_PROFILE`/env fallback.
- **Azure**: `az network nic ip-config …` — auth: **managed identity** first,
  `az login`/SP env fallback.
- **OCI**: `oci network private-ip create` / `vnic` — auth: **instance principal**
  first, OCI config profile fallback.

`Plugin.capabilities` gate what a plugin may do
(`observe.events`/`propose.dynamicConfig`/`propose.providerAction`).

## Phased decomposition (one workflow per phase, run in order)

Each phase = an independently-acceptable slice; later phases gated on earlier
acceptance. Implementation delegated to codex; claude orchestrates + reviews.

- **✅ DONE — Phase 1 — Event model + local store.** `EventGroup` Kind; reuse/extend
  `DaemonEvent` as the external `Event` envelope (id, group, sourceNode, type,
  subject, ttl, dedupeKey, payload); SQLite `federation_events` table; `routerctl
  federation event emit/list`. *Accept:* emit→stored w/ TTL; dup id idempotent;
  expired ignored.
- **✅ DONE (lab-smoke PASS) — Phase 1.5 — `EventPeer`/`EventSubscription` Kinds + validation.**
- **✅ DONE (lab-smoke PASS) — Phase 2 — Peer delivery over overlay.** `EventPeer` Kind; `routerd-eventd`
  receiver bound to `wg-hybrid`; HMAC; push + backoff; `event_deliveries`.
  *Accept:* onprem pushes to cloud over `wg-hybrid`; dup push idempotent; bad HMAC
  rejected; `routerctl event deliveries`; `routerd-eventd` periodically prunes
  `federation_events` per `EventGroup` retention (`maxAge`/`maxEvents`), and
  `routerctl federation event prune --dry-run` reports what would be removed.
- **✅ DONE (lab-smoke PASS) — Phase 3 — Subscription-triggered plugin → DynamicConfigPart.**
  `EventSubscription` Kind; event batch → `PluginRequest`; `PluginResult` →
  `DynamicConfigPart` (with `routerd.net/dynamic-source`, `event-id`, `event-group`
  annotations); debounce/batchWindow; `event_subscription_runs`. *Accept:* cloud
  receives `client.ipv4.observed` for `10.88.60.9/32` → plugin → `RemoteAddressClaim`
  DynamicConfigPart visible in `routerctl dynamic render`; actionPlan shown,
  not executed.
- **⏭ NEXT (not started) — Phase 4 — Provider actionPlan plugins (dry-run).** `aws/azure/oci-address-claim`
  example plugins; standardized `actionPlan` format; instance-identity auth.
  *Accept:* plugins propose assign-secondary-IP; no mutation; plan visible in
  `routerctl plugin`/`dynamic`.
- **Phase 5 — (post-MVP) provider action execution.** Approval/auto-apply policy;
  action journal; best-effort undo; identity docs. Out of MVP.

The first end-to-end smoke is **manual `routerctl federation event emit` →
federation → DynamicConfigPart** (Phases 1–3). The ARP/Clients observer plugin comes *after*
that smoke (modeled on `routerd-ra-observer`), so failures are isolatable.

### MVP event types

`routerd.client.ipv4.observed`, `…ipv4.expired`, `…dynamic.part.accepted/rejected`,
`…provider.action.planned/succeeded/failed`. `observed`+`expired` alone suffice for
the first smoke.

## Consequences

- **Positive:** turns SAM from hand-authored to event-driven; small, demoable
  phases; reuses existing envelope/transport/plugin/state; no new Kind sprawl (3);
  provider mutation stays gated; cloud-native identity from day one.
- **Negative / risks:** a new network listener (overlay-bound + HMAC mitigates);
  loop/flap and provision/de-provision asymmetry must be enforced as invariants
  (above); at-least-once pushes idempotency onto plugins and naming; TLS/mTLS
  deferred. de-provisioning automation is deliberately the *last* thing enabled.
- **Scope-out (MVP):** consensus, exactly-once, gossip mesh, arbitrary remote
  command execution, automatic provider mutation, full IP lifecycle automation,
  remote plugin registry, cross-node config rewrite.

## Known limitations (experimental)

- **`routerd-eventd` supervision is generated for systemd and FreeBSD `rc.d`.**
  Other service managers still need explicit renderer support before eventd can
  be supervised there automatically.
- **`EventSubscription` `batchWindow`/`debounce` are accepted but coarse.** The
  fields validate and are honored at poll granularity — the controller batches
  events **per poll tick**, not on a precise sub-tick timer. Tight debounce
  windows are effectively rounded up to the tick interval.

## Out of scope / open questions for later

- Whether `cloud→onprem` needs more than acks (e.g. capture-ready signal that
  toggles on-prem proxy-arp only after cloud secondary exists).
- Sharing filters across peers (promote `EventFilter` to its own Kind).
- Multi-peer / >2-node groups (MVP targets the validated pair topology).
