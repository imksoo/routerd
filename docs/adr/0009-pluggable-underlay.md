# ADR 0009: Pluggable Overlay Underlay (ipip / gre, then fou / gue)

![Diagram showing ADR 0009 pluggable underlay with TunnelInterface, IPIP or GRE delivery, optional WireGuard encrypted underlay, MTU overhead derivation, and MSS clamp safety](/img/diagrams/adr-0009-pluggable-underlay.png)

## Status

Proposed; Accepted for experimental implementation — 2026-06-01.

Builds on the CloudEdge overlay/SAM dataplane ([ADR 0006](../adr/0006-event-federation.md),
[Selective Address Mobility](../reference/selective-address-mobility)) and the
zone-independent PMTU/MSS clamp (#53/#68). Experimental.

## Context

The CloudEdge overlay (`OverlayPeer`) currently uses **WireGuard** as its only
implemented underlay. Over a *trusted* private underlay — ExpressRoute,
DirectConnect, FastConnect, VPC/VNet peering — WireGuard's encryption is redundant
and its ~80-byte overhead is pure cost. We want to let the operator pick a lighter,
lower-overhead L3 transport when the underlay is already trusted, **without**
changing how addresses are delivered.

The overlay is already abstracted at the right seams (confirmed in code):

- **Delivery is underlay-independent.** `hybrid.RouteTarget(peer)` maps an
  `OverlayPeer.Underlay.Type` to `(device, gateway)`, and the `/32` delivery routes
  (`RemoteAddressClaim` / `HybridRoute`) point at that device. Adding a transport is
  a new `switch` case.
- **MTU / MSS clamp is parameterized.** `hybrid.EstimateMTU = underlayMTU(interface)
  − overheadFor(type)`; the zone-independent clamp follows `EstimateMTU`. A new
  transport just needs an overhead value and an interface MTU; the clamp
  auto-follows.

The only real gap: **device creation is WireGuard-specific** (a dedicated
`WireGuardInterface` Kind + controller). New L3 transports need an equivalent
"create the tunnel device" resource + controller.

## Decision

### New Kind `TunnelInterface` (`hybrid.routerd.net/v1alpha1`)

Mirrors `WireGuardInterface`: a resource that owns one OS tunnel device's desired
state. `OverlayPeer.Underlay` stays the *delivery-selection* reference;
`TunnelInterface` is the *device desired state* — a clean split (inline fields on
`OverlayPeer` would proliferate device specs per peer and make device
ownership/idempotency/delete ambiguous).

Phase 1 fields:

- `mode`: `ipip | gre`.
- `local`, `remote`: underlay (physical) endpoint IPs (required).
- `address`: overlay inner address (optional; otherwise set by the
  `ipv4-static-address` controller, as for WireGuard).
- `mtu` (optional), `ttl` (optional, default 64), `key` (GRE only; if set, +4
  overhead).
- `trustedUnderlay: true` — **required** (see Safety).

Phase 2 extends the same Kind with IPIP-over-UDP:

- `mode`: `fou | gue` mean an `ipip` tunnel device with Linux UDP encapsulation
  (`encap fou` or `encap gue`).
- `encapSport`, `encapDport`: local UDP source/listen port and peer destination
  port. Both are required for `fou`/`gue`.

`OverlayPeer.Underlay.Type` enum gains `ipip`, `gre`, `fou`, `gue`; `.Interface`
names the `TunnelInterface`.

### New controller `tunnel`

A `framework.FuncController` reconciling `TunnelInterface` (Linux only in Phase 1;
other platforms report an unsupported status rather than erroring the chain):

- **argv-based `ip` invocations** (not string-concatenated shell), idempotent via
  `ip link show` → add/modify/`ip link del`:
  - `ip link add <dev> type ipip|gre local <L> remote <R> ttl <t> [key <k>]`
  - for `fou`/`gue`: `ip fou add port <sport> ipproto 4|gue`, then
    `ip link add <dev> type ipip local <L> remote <R> ttl <t> encap fou|gue
    encap-sport <sport> encap-dport <dport>`
  - `ip link set <dev> mtu <m> up`
- Address handled by the existing `ipv4-static-address` controller (as for
  WireGuard).
- Status: phase, device, mode, local, remote, mtu.

### Overhead, delivery, MTU

- `overheadFor`: `ipip = 20`, `gre = 24` (outer IPv4 20 + GRE base 4), `fou = 28`
  (outer IPv4 + UDP), `gue = 32` (outer IPv4 + UDP + minimal 4-byte GUE header).
  GRE `key` adds 4.
- `RouteTarget`: `ipip`, `gre`, `fou`, `gue` → `(device, "")` (the `/32` route
  points at the tunnel device, like WireGuard).
- `EstimateMTU` and the PMTU/MSS clamp follow automatically; the
  `pathMTUResourceMTU` fallback gains a `TunnelInterface` default (or `spec.mtu` is
  honoured).

### Validation

- `OverlayPeer.Underlay.Type` enum += `ipip`, `gre`, `fou`, `gue`.
- `TunnelInterface`: `mode ∈ {ipip, gre, fou, gue}`; `local`/`remote` required,
  valid IPs; `trustedUnderlay == true` required (reject with a clear message
  otherwise); MTU/TTL/key/encap port ranges.

## Safety (hard invariant)

`ipip`, `gre`, `fou`, `gue` are **unencrypted and unauthenticated** — fundamentally
unlike WireGuard. They are only safe over an already-trusted underlay.

- **WireGuard stays the default.**
- A `TunnelInterface` is rejected unless it sets **`trustedUnderlay: true`** — an
  explicit operator acknowledgment that the underlay carries plaintext. Docs/doctor
  warnings alone are too weak; this is a validation gate.

## Phasing

- **Phase 1**: `TunnelInterface` Kind + `tunnel` controller
  (Linux `ipip`/`gre`) + `trustedUnderlay` gate + `RouteTarget`/overhead/MTU +
  validation + unit/fixture tests + an example config. Tests include the **deletion
  ordering** invariant: removing the `OverlayPeer`/claim drops the `/32` route, and
  removing the `TunnelInterface` yields a device-delete plan; route install must
  tolerate a missing device.
- **Phase 2 (implemented)**: `fou` / `gue` as IPIP-over-UDP. We deliberately do
  not expose GRE-over-FOU/GUE yet; that would require an explicit inner-mode field
  or combined type strings. Adds `ip fou add` encap-port setup; documents the
  minimal-header overhead assumption with the existing explicit `mtu` escape hatch.
- **Phase 3**: FreeBSD (`gif` for ipip, `gre`) — different config/status surface, so
  not crammed into the Linux controller.
- **Phase 4**: firewall auto-holes (raw `ipip` = IP proto 4, `gre` = IP proto 47,
  `fou`/`gue` = UDP) + `doctor hybrid` checks.

## Consequences

- The operator gains a lighter overlay transport for trusted underlays; delivery and
  MSS clamp are unchanged and auto-follow the new overhead.
- The encryption trade-off is explicit and gated (`trustedUnderlay: true`), so the
  lighter transports cannot be selected by accident over an untrusted path.
- `TunnelInterface` is a general device-desired-state resource that Phases 2–3 extend
  (encap, FreeBSD) without touching the delivery/MTU seams.
- No change to WireGuard behaviour or to existing deployments (default unchanged).
