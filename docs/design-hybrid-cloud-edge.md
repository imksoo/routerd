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

The model extends the existing routerd architecture in
[Architecture overview](./design.md): controllers still reconcile one desired
configuration, resources still use the same `apiVersion`, `kind`,
`metadata.name`, `spec`, and `status` shape, and the state database remains the
runtime record for generated state.

## Goals

CloudEdge is aimed at mixed cloud and on-prem deployments where a router needs
runtime intent from a trusted local source:

- cloud inventory that changes faster than the startup YAML should change
- cloud-side address claims, route hints, or VPN attachment observations
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
- `hybrid.routerd.net/v1alpha1` API group constant for future hybrid resources
- `plugin.routerd.net/v1alpha1` request/result contract types
- documentation for layering, merge behavior, plugin I/O, policy, and future
  operator commands

Out of scope for this PR:

- dataplane behavior
- controllers or reconcile-loop integration
- CLI commands
- plugin process execution
- state database persistence
- schema generation changes
- remote plugin install, remote plugin registry, or remote provider execution

## Roadmap

The intended path is L3 hybrid first. Future PRs can add typed hybrid resources
such as cloud address claims, route advertisements, VPN attachment observations,
and provider inventory snapshots. These resources should become normal
effective-config resources after validation, so existing route, firewall, NAT,
and observability flows can consume them without a separate cloud control path.

Selective L2 extension is future work and should stay narrow. VXLAN, EVPN, VRF,
WireGuard, and IPsec groundwork already exists, but CloudEdge should prefer L3
reachability and explicit routing policy before bridging remote fault domains.
Any selective L2 design needs a separate safety discussion covering loop
avoidance, failure isolation, MTU, broadcast containment, and operational
rollback.

See also [Dynamic config reference](./reference/dynamic-config.md) and
[Plugin protocol](./plugin-protocol.md).
