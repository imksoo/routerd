---
title: Dynamic config
slug: /reference/dynamic-config
---

# Dynamic config

Dynamic config lets trusted local sources contribute runtime intent without
editing startup-config. routerd derives one effective-config from startup YAML,
active dynamic parts, and active masks. Effective-config is the only reconcile
target.

This page documents the dynamic-config API shape for the CloudEdge MVP. The
plugin runner can store validated plugin output as `DynamicConfigPart` records;
provider actions from `actionPlans` remain inert inside dynamic config and are
not merged into effective config. The separate provider-action engine can import
them into its journal and execute them only through `ProviderActionPolicy`,
approval, and executor-plugin gates.

![Dynamic config diagram showing startup config, DynamicOverridePolicy, trusted local plugin output, DynamicConfigPart, effective config, inert actionPlans, action journal, and gated executor plugin path](/img/diagrams/dynamic-config-provider-actions.png)

## DynamicConfigPart

`DynamicConfigPart` is one validated runtime fragment from a dynamic source.
The source can contribute normal `api.Resource` objects and directives.

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicConfigPart
metadata:
  name: oci-inventory
spec:
  source: Plugin/oci-inventory
  generation: 12
  observedAt: "2026-05-29T12:00:00Z"
  expiresAt: "2026-05-29T12:05:00Z"
  digest: sha256:...
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata: { name: app-10-0-1-123 }
      spec:
        domainRef: cloudedge-same-subnet
        address: 10.0.1.123/32
        ownerSide: cloud
        capture: { type: provider-secondary-ip, providerRef: oci-prod, providerMode: vnic-private-ip, nicRef: ocid1.vnic.oc1..example }
        delivery: { peerRef: cloud-main, mode: route, tunnelInterface: wg-hybrid }
  directives:
    - op: mask
      target: { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
      reason: "RemoteAddressClaim/app-10-0-1-123 is active"
```

| Field | Meaning |
| --- | --- |
| `spec.source` | Stable source identity, for example `Plugin/oci-inventory`. |
| `spec.generation` | Monotonic source generation for explanation and ordering. |
| `spec.observedAt` | RFC3339 time when the source observed the input. |
| `spec.expiresAt` | RFC3339 time after which the part is inactive. |
| `spec.digest` | Digest of the validated part payload. |
| `spec.resources` | Resources contributed to effective-config while active. |
| `spec.directives` | Merge directives, currently only `op: mask`. |
| `spec.actionPlans` | Provider action proposals. They are not resources; the provider-action engine imports only active parts and applies its own gates before execution. |

Plugins return a TTL duration in `PluginResult.status.ttl`; routerd resolves it
against `observedAt` into the stored `expiresAt`.

## DynamicConfigSource

`DynamicConfigSource` is startup-config policy that binds one plugin to dynamic
config production.

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata: { name: oci-inventory }
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

The MVP merge policy supports only `conflict: reject`.

## DynamicConfigDirective

The MVP supports one directive operation:

| Operation | Meaning |
| --- | --- |
| `mask` | Suppress one matching startup-config resource while the directive is active. |

A directive target is identified by `apiVersion`, `kind`, and `name`. The
target is intentionally exact; wildcard masks are out of scope for the MVP.

## DynamicOverridePolicy

`DynamicOverridePolicy` grants a source permission to use dynamic directives
against selected resources. A plugin can propose a mask, but the mask is active
only if policy allows that source, operation, and target.

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicOverridePolicy
metadata: { name: allow-cloud-plugin-mask }
spec:
  allow:
    - source: Plugin/oci-inventory
      operations: [mask]
      targets:
        - { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
```

Policy is startup-config intent. Dynamic sources do not grant themselves
override permissions.

## Merge Algorithm

The effective-config merge is deterministic:

1. Load and validate startup-config.
2. Load validated dynamic parts from the state database.
3. Drop dynamic parts whose `expiresAt` is at or before the merge time.
4. Sort active dynamic parts by `source`, then `generation`, then
   `metadata.name` for stable rendering and diagnostics.
5. Evaluate active directives against `DynamicOverridePolicy`.
6. Mark startup resources targeted by allowed active masks as suppressed.
7. Build effective-config from unsuppressed startup resources plus active
   dynamic resources.
8. Validate the resulting effective-config before reconcile or dry-run output.

Conflict rules:

- A dynamic resource must not replace a startup resource with the same
  `apiVersion`, `kind`, and `metadata.name`.
- Two active dynamic resources with the same identity conflict unless a later
  design defines source-specific ownership rules.
- A disallowed directive is ignored for merge and reported as a validation or
  diagnostic finding.
- Expired dynamic parts do not contribute resources or masks.

## Mask Semantics

A mask suppresses; it does not delete. The startup YAML remains unchanged, git
history remains operator-owned, and the static resource becomes active again
when every matching active mask expires or is removed.

Suppressed resources should surface status similar to:

```yaml
status:
  phase: Suppressed
  maskedBy:
    - Plugin/oci-inventory#12
  maskedUntil: "2026-05-29T12:05:00Z"
```

When multiple masks target the same resource, the resource remains suppressed
until the last active mask expires. `maskedBy` lists every active source and
generation. `maskedUntil` is the latest `expiresAt` among active masks.

The MVP expiry behavior is `onExpire=restoreStatic`: when a mask expires,
routerd restores the startup-config resource to effective-config during the
next merge. There is no destructive cleanup step because no startup resource was
modified.

## CLI

The current operator surface is:

```text
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
routerctl plugin run <name> [--dry-run]
```

`dynamic list` shows active and expired parts. `dynamic describe` explains
source, generation, digest, resources, directives, and expiry. `dynamic render`
prints effective-config. `dynamic diff` compares startup-config to
effective-config. `plugin run --dry-run` prints a candidate dynamic part without
writing the state database.

See [Hybrid cloud edge design](../design-hybrid-cloud-edge) and
[Plugin protocol](/docs/reference/plugin-protocol).
