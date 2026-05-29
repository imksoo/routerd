---
title: Hybrid cloud edge design
---

# Hybrid cloud edge design

routerd's CloudEdge direction is to let one declarative router model cover the
local edge and selected cloud-side facts without letting cloud automation edit
the human-managed router file. The first implementation wave is intentionally
only design documentation and Go type definitions. It introduces the API shapes
for dynamic configuration, plugin I/O, and future hybrid resources; it does not
wire controllers, the CLI, or dataplane renderers.

CloudEdge has two declarative pillars in this MVP:

- L3 hybrid routing, where `HybridRoute` lowers remote IPv4 prefixes through an
  `OverlayPeer`.
- Selective Address Mobility, where selected `/32` IPv4 addresses are captured
  locally and delivered to the owning side over a routerd-to-routerd overlay
  without stretching L2.

The model extends the existing routerd architecture in
[Architecture overview](./design.md): controllers still reconcile one desired
configuration, resources still use the same `apiVersion`, `kind`,
`metadata.name`, `spec`, and `status` shape, and the state database remains the
runtime record for generated state.

## Goals

CloudEdge is aimed at mixed cloud and on-prem deployments where a router needs
runtime intent from a trusted local source:

- cloud inventory that changes faster than the startup YAML should change
- selective address claims, route hints, or VPN attachment observations
- provider actions that should be visible during plan/dry-run before operators
  decide whether to apply provider-side changes outside routerd
- selective suppression of static fallback resources while a dynamic cloud
  resource is healthy

The MVP is conservative. Dynamic input can contribute resources and `mask`
directives to the effective configuration, but it cannot mutate the startup
file and routerd will not execute provider action plans.

## Config layers

CloudEdge introduces three explicit configuration layers.

### startup-config

The startup-config is the human-managed YAML loaded from the normal routerd
configuration path, usually `/usr/local/etc/routerd/router.yaml`. Operators
should keep it in git, review it like source code, and apply it through the
existing validate, plan, dry-run, and apply flow.

Plugins must never edit startup-config. A plugin can observe the startup hash
and emit dynamic intent, but it cannot rewrite, reorder, or remove resources in
the source file. Static fallback routes, emergency management access, and other
operator-owned safety resources belong here.

### dynamic-config

Dynamic-config is runtime intent produced by trusted local sources. In the MVP
the primary source is a local plugin installed under the platform plugin
directory, for example:

```text
/usr/local/libexec/routerd/plugins/<name>/
```

Each plugin result is validated and stored as a `DynamicConfigPart` in the
state database. A part has a source, generation, observedAt timestamp, expiresAt
timestamp, digest, and resources/directives. Plugins return a TTL in their
`PluginResult`; routerd resolves that duration into the stored `expiresAt` for
the part. Expired parts are ignored when deriving effective-config.

Dynamic-config is not host state and is not an imperative command queue. It is
runtime desired intent that participates in the same resource model as the
startup configuration.

### effective-config

Effective-config is the only reconcile target:

```text
effective-config = startup-config + active dynamic parts - active masks
```

Controllers, renderers, dry-run, plan, status, and future CloudEdge surfaces
should reason about effective-config. Startup resources suppressed by active
masks are not deleted from the startup file; they are omitted from the active
reconcile set and reported as suppressed with enough metadata to explain why.

## Design principles

- Startup-config is immutable from routerd plugins. Operators own it.
- Dynamic-config is runtime intent, not a direct OS mutation path.
- Effective-config is the only reconcile target.
- Dynamic resources never overwrite startup resources in place.
- Masks suppress matching startup resources; they do not delete them.
- Every dynamic change must be explainable by source, generation, digest,
  observed time, expiry time, and directive reason.
- Plugin output is always validated before it becomes dynamic-config.
- Provider action plans are dry-run and display only in the MVP.

## MVP scope

In scope for the CloudEdge MVP foundation:

- `config.routerd.net/v1alpha1` type definitions for `DynamicConfigPart` and
  `DynamicOverridePolicy`
- `hybrid.routerd.net/v1alpha1` `OverlayPeer`, `HybridRoute`,
  `AddressMobilityDomain`, `CloudProviderProfile`, and `RemoteAddressClaim`
  resource shapes
- `plugin.routerd.net/v1alpha1` request/result contract types
- documentation for layering, merge behavior, plugin I/O, policy, and future
  operator commands

Out of scope for this PR:

- live address capture or forwarding dataplane behavior
- controllers or reconcile-loop integration
- CLI commands
- plugin process execution
- state database persistence
- schema generation changes
- remote plugin install, remote plugin registry, or remote provider execution

## L3 hybrid routing

`HybridRoute` is the conservative L3 pillar. It represents non-default remote
IPv4 prefixes that should be lowered through an `OverlayPeer`. The lowering and
status path is intentionally explicit: default routes are rejected, and
operators can review the generated route intent through the normal routerd plan
and dry-run flow.

## Selective Address Mobility

Selective Address Mobility is the second CloudEdge pillar. It is not full L2
extension. Public cloud fabrics do not expose an operator-controlled Ethernet
segment, and provider address ownership models differ. routerd therefore models
only selected mobile `/32` IPv4 addresses:

- `AddressMobilityDomain` defines the IPv4 prefix and requires
  `mode: selective-address`.
- `CloudProviderProfile` describes a provider and its declared capabilities; it
  does not call provider APIs.
- `RemoteAddressClaim` declares one `/32`, its owner side, a capture mechanism,
  and route delivery over an `OverlayPeer`.

The MVP layer is declarative only. No controller assigns secondary cloud IPs,
enables proxy ARP, installs `/32` forwarding routes, toggles `ip_forward`, or
programs netlink for these resources. Live capture and forwarding are a later
dataplane step.

Selective Address Mobility lives in the ordinary switching/forwarding plane and
contains no firewall or NAT concept. Source and destination transparency is
intrinsic, not a configurable field. Operators compose firewall and NAT policy
separately by referencing literal addresses in existing `FirewallZone`,
`FirewallRule`, or `NAT44Rule` resources.

See [Selective Address Mobility](./reference/selective-address-mobility.md) for
the resource model and provider capability framing.

## Observe-only cloud inventory

Cloud inventory plugins can observe provider state and return dynamic resources
without mutating the provider. The example `oci-inventory` plugin emits a static
`RemoteAddressClaim` candidate plus an OCI-style `actionPlan` that describes
how a secondary private IP could be assigned outside routerd.

`RemoteAddressClaim` is declarative and dry-run/plan only in this MVP. It
records the mobility domain, `/32` address, owner side, capture metadata such as
a provider secondary IP or proxy-ARP interface, and the route delivery hint for
an overlay peer. routerd validates and displays the resource as dynamic-config,
but no controller calls a cloud API, assigns a secondary IP, or mutates host
networking for this kind.

Provider `actionPlans` stay display-only. They are useful for dry-run review and
operator handoff, but they are not an imperative queue and routerd never
executes them.

## Roadmap

Future PRs can add live capture and forwarding for Selective Address Mobility,
route advertisements, VPN attachment observations, and provider inventory
snapshots. These resources should remain normal effective-config resources
after validation, so existing route, firewall, NAT, and observability flows can
consume them without a separate cloud control path.

Full L2 extension remains out of scope. VXLAN, EVPN, VRF, WireGuard, and IPsec
groundwork already exists, but CloudEdge should prefer L3 reachability and
explicit per-address mobility before bridging remote fault domains. Any full L2
design needs a separate safety discussion covering loop avoidance, failure
isolation, MTU, broadcast containment, and operational rollback.

See also [Dynamic config reference](./reference/dynamic-config.md) and
[Plugin protocol](./plugin-protocol.md).
