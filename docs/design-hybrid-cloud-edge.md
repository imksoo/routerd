---
title: Hybrid cloud edge design
---

# Hybrid cloud edge design

routerd's CloudEdge direction is to let one declarative router model cover the
local edge and selected cloud-side facts without letting cloud automation edit
the human-managed router file. The current implementation includes dynamic
configuration, plugin I/O, BGP-mode selective address mobility, generated SAM
transport resources, and gated provider-action execution.

CloudEdge has two declarative pillars in this MVP:

- L3 hybrid routing, where `HybridRoute` lowers remote IPv4 prefixes through an
  `OverlayPeer`.
- Selective Address Mobility, where selected `/32` IPv4 addresses are captured
  locally and delivered to the owning side over a routerd-to-routerd overlay
  without stretching L2.

The model extends the existing routerd architecture in
[Architecture overview](./design): controllers still reconcile one desired
configuration, resources still use the same `apiVersion`, `kind`,
`metadata.name`, `spec`, and `status` shape, and the state database remains the
runtime record for generated state.

![CloudEdge SAM diagram showing MobilityPool and SAMTransportProfile generating DynamicConfigPart resources, IPIP TunnelInterface delivery, BGP peers, ECMP-capable FIB paths, and endpoint-only WireGuard underlay](/img/diagrams/cloudedge-sam-ipip.png)

## Goals

CloudEdge is aimed at mixed cloud and on-prem deployments where a router needs
runtime intent from a trusted local source:

- cloud inventory that changes faster than the startup YAML should change
- selective address claims, route hints, or VPN attachment observations
- provider actions that should be visible during plan/dry-run before operators
  decide whether to import and execute provider-side changes through the gated
  executor path
- selective suppression of static fallback resources while a dynamic cloud
  resource is healthy

The posture is conservative. Dynamic input can contribute resources and `mask`
directives to the effective configuration, but it cannot mutate the startup
file. Provider action execution is a separate, default-off journaled path with
explicit policy gates.

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
- Provider action plans are inert inside dynamic-config. They can be imported
  into the action journal and executed only through explicit
  `ProviderActionPolicy`, approval, allowlist, and executor-plugin gates.

## Current scope

The CloudEdge foundation now includes:

- `DynamicConfigPart` / `DynamicOverridePolicy` for runtime intent and masks.
- Trusted local plugin execution for observation, dynamic resources, provider
  action proposals, and executor plugins.
- `MobilityPool` as the primary selective-address mobility intent.
- `SAMTransportProfile` as the primary transport authoring surface. It generates
  IPIP/GRE `TunnelInterface`, endpoint `/32` `IPv4Route`, and `BGPPeer`
  resources through dynamic config.
- BGP-mode SAM delivery: owners advertise selected IPv4 `/32` paths, non-owners
  import best paths into the local FIB, and multipath is preserved where BGP
  supplies multiple next hops.
- Linux SAM capture for provider-secondary-IP and proxy-ARP cases, including
  on-prem VRRP/single-router gating, GARP on active transition, and conservative
  on-demand ARP discovery.
- Provider action execution as an experimental, default-off path. Action plans
  remain review artifacts until imported and gated by policy, approval, and an
  executor plugin that holds provider credentials outside routerd core.

Still out of scope:

- remote plugin install or a public plugin registry
- consensus-based global ownership or split-brain prevention
- treating CloudEdge SAM as full L2 extension
- automatic rollback of arbitrary provider or OS mutations

## L3 hybrid routing

`HybridRoute` is the conservative L3 pillar. It represents non-default remote
IPv4 prefixes that should be lowered through an `OverlayPeer`. The lowering and
status path is intentionally explicit: default routes are rejected, and
operators can review the generated route intent through the normal routerctl plan
and dry-run flow.

## Selective Address Mobility

Selective Address Mobility is the second CloudEdge pillar. It is not full L2
extension. Public cloud fabrics do not expose an operator-controlled Ethernet
segment, and provider address ownership models differ. routerd therefore models
only selected mobile `/32` IPv4 addresses.

The current primary authoring model is:

- `MobilityPool` declares the mobility prefix, federation group, member
  identities, site roles, capture policy, provider trap placement, and BGP
  delivery policy.
- `SAMTransportProfile` declares the router-to-router transport: self node,
  shared topology node list, inner prefix, IPIP/GRE mode, optional WireGuard
  encryption underlay, BGP router, and peers.
- `CloudProviderProfile` describes provider capabilities and external auth shape.
- `ProviderActionPolicy` controls whether imported provider action plans may be
  handed to an executor plugin.

The lower-level `AddressMobilityDomain` and `RemoteAddressClaim` resources remain
available for compatibility and experiments, but they are no longer the primary
CloudEdge SAM authoring surface.

Selective Address Mobility lives in the ordinary switching/forwarding plane and
contains no firewall or NAT concept. Source and destination transparency is
intrinsic, not a configurable field. Operators compose firewall and NAT policy
separately by referencing literal addresses in existing `FirewallZone`,
`FirewallRule`, or `NAT44Rule` resources.

See [Selective Address Mobility](./reference/selective-address-mobility) for
the resource model and provider capability framing.

## Cloud inventory and provider actions

Cloud inventory plugins can observe provider state and return dynamic resources
without mutating the provider. Provider capture planners can also emit
`actionPlans` such as `assign-secondary-ip`,
`unassign-secondary-ip`, `ensure-forwarding-enabled`, or
`ensure-forwarding-disabled`.

An `actionPlan` is not merged into effective-config and is not executed by the
dynamic-config controller. It is persisted as reviewable state. Operators may
import it into the provider-action journal and then run `routerctl action`
commands. Live mutation is still default-off and requires all hard gates:
`ProviderActionPolicy.enabled`, not `dryRunOnly`, approval or explicit
auto-approval policy, provider/action/CIDR allowlists, a positive
`maxActionsPerRun`, and an executor plugin with `execute.providerAction`.

routerd core never holds cloud credentials. The executor plugin runs as its own
process and authenticates with cloud-native identity or its own environment.

## Roadmap

Further CloudEdge work should keep using normal effective-config resources after
validation, so existing route, firewall, NAT, ownership, GC, and observability
flows can consume them without a separate cloud control path. Remaining design
areas include broader provider parity, operational evidence automation, more
transport derivation ergonomics, and production hardening around split-brain
observability.

Full L2 extension remains out of scope. VXLAN, EVPN, VRF, WireGuard, and IPsec
groundwork already exists, but CloudEdge should prefer L3 reachability and
explicit per-address mobility before bridging remote fault domains. Any full L2
design needs a separate safety discussion covering loop avoidance, failure
isolation, MTU, broadcast containment, and operational rollback.

See also [Dynamic config reference](./reference/dynamic-config.md) and
[Plugin protocol](/docs/reference/plugin-protocol).
