# ADR 0015: WireGuard Peer Enrollment for Hub/Spoke Bootstrap

## Status

Proposed -- 2026-06-09.

Related issue: #377.

## Context

`WireGuardInterface.spec.peersFrom` can already derive WireGuard peers from a
shared `SAMNodeSet`. This removes most static peer duplication when every
router already has a trusted copy of the node registry.

That does not fully solve hub/spoke bootstrap. In a route-reflector or spine
deployment, leaf routers usually initiate WireGuard traffic toward fixed
RR/spine endpoints. The RR/spine side still needs each leaf's public key,
allowed IPs, and optional endpoint before the kernel will accept the peer.
Adding more `pve-rt` style leaves therefore still creates operational churn in
the RR/spine source of truth.

The first contact path cannot be the target WireGuard tunnel. WireGuard drops
unknown peers before any application protocol can run, so enrollment must use a
separate bootstrap transport such as a management address, an underlay listener,
or another pre-established control channel.

## Decision

Add an optional WireGuard peer enrollment flow for hub/spoke deployments.

An RR/spine router may expose an enrollment endpoint on an explicitly configured
non-WireGuard listen address and port. A leaf submits its node identity and
WireGuard peer material, and the RR/spine validates that request against local
policy and the expected topology before the peer becomes active.

The enrollment record should include:

- `nodeRef` and target WireGuard interface;
- WireGuard `publicKey`;
- endpoint or listen port, when the leaf has a stable endpoint;
- requested `allowedIPs` and/or `samEndpoint`;
- a nonce or generation value so retries are idempotent and stale writes are
  detectable.

Approved registrations are stored as dynamic config, not as ad-hoc runtime
state hidden from the config graph. The effective config path then turns those
records into ordinary WireGuard peer inputs, either as generated
`WireGuardPeer` resources or as entries consumable by the existing
`WireGuardInterface.spec.peersFrom` machinery. Static `WireGuardPeer` resources
continue to override generated peers by name so operators retain an emergency
override.

The leaf's static bootstrap config stays small: it needs its own private key,
the RR/spine public key and fixed endpoint, and the enrollment credentials. The
RR/spine owns approval and activation.

## Validation and Security

Enrollment fails closed. A request is accepted only when all configured checks
pass.

- The enrollment endpoint is disabled by default and bound only to configured
  addresses.
- The request is authenticated with an explicit mechanism such as a bearer
  token, mTLS client identity, or a signed registration payload.
- The requested `nodeRef` is allowed by policy and, when configured, present in
  the expected `SAMNodeSet`.
- Requested `allowedIPs` and `samEndpoint` match the node identity and do not
  collide with existing nodes.
- Public keys are unique unless the same node is retrying the same generation.
- Re-registration, key rotation, rejection, revocation, and expiration are
  visible in audit/status output.
- Rate limiting protects the bootstrap endpoint from repeated invalid
  registrations.

`routerctl` should expose enrollment state as `Pending`, `Approved`,
`Rejected`, `Revoked`, or `Expired`, with the validation reason when a request
is not active.

## Non-Goals

- Do not replace WireGuard cryptokey routing. The RR/spine still installs one
  kernel peer per approved leaf.
- Do not accept arbitrary public keys without an explicit policy decision.
- Do not run first-contact enrollment through the target WireGuard interface.
- Do not make `SAMNodeSet` distribution depend on a tunnel that itself requires
  the newly enrolled peer.

## Implementation Plan

1. Define the enrollment API resource shape, status model, and CLI/status
   output. Keep it separate from WireGuard runtime reconciliation.
2. Add RR/spine enrollment storage as a dynamic config source with durable audit
   information and stale-entry cleanup.
3. Add validation against policy and optional `SAMNodeSet` membership.
4. Feed approved registrations into the existing effective WireGuard peer
   generation path while preserving static peer override behavior.
5. Add leaf-side submit/retry logic that is idempotent and safe to run at boot.
6. Add revocation and key rotation flows.

## Consequences

RR/spine configs stop growing by one hand-authored WireGuard peer block per leaf
when the deployment has an approved enrollment policy. The kernel peer count and
the need for identity validation remain, but the operator workflow shifts from
editing peer material on every RR/spine to approving or pre-authorizing leaf
registrations.

The feature also creates a clear bootstrap boundary: topology distribution can
continue to use `SAMNodeSet` and `peersFrom`, while first-contact trust is
handled by an explicit non-WireGuard enrollment surface.
