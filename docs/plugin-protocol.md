---
title: Plugin protocol
slug: /reference/plugin-protocol
---

# Plugin protocol

routerd plugins are trusted local executables. The plugin mechanism lets you add resource-specific behaviour as a small program on the same host, without modifying the routerd binary.

Remote plugin registration, remote installation, and a public plugin registry are intentionally out of scope.

## Layout

The default install path is:

```text
/usr/local/libexec/routerd/plugins/<name>/
```

Each plugin has a manifest and an executable:

```text
plugin.yaml
bin/<plugin>
```

## Responsibilities

A plugin can take part in:

- Resource validation
- Plan generation
- Host state observation
- Host state application

Operations that mutate network state should be split into testable units. As with the main code base, tests that touch real host networking should run inside isolated network namespaces (see `tests/netns`).

## MVP policy

For the CloudEdge MVP, plugins are trusted local executables only. routerd does
not fetch plugins from a remote registry, install plugins remotely, or execute
cloud/provider actions on behalf of a plugin.

Plugin output is always validated before it is stored as dynamic-config or used
to derive effective-config. A plugin can propose resources, directives,
display-only action plans, and events. `actionPlans` are dry-run / display only
in the MVP and are never executed by routerd.

## Resource shapes

A plugin is declared as a local executable and optional trigger set:

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: oci-inventory
spec:
  executable: bin/oci-inventory
  triggers:
    - type: interval
      interval: 300s
    - type: event
      topic: routerd.cloud.oci.refresh
```

A dynamic config source binds a plugin to dynamic-config production policy:

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata:
  name: oci-inventory
spec:
  pluginRef: oci-inventory
  source: Plugin/oci-inventory
```

These resource shapes document the intended API surface. The MVP foundation PR
only defines the Go I/O and dynamic-config types; controller and CLI wiring come
later.

## Triggers

Plugins run from explicit triggers:

| Trigger | Meaning |
| --- | --- |
| `interval` | Periodic refresh, usually for inventory or lease-like observations. |
| `event` | Event-bus driven refresh for a named topic. |

The `PluginRequest.spec.trigger` field records the actual trigger for one
invocation. `trigger.type` is `interval` or `event`; `trigger.topic` is set for
event-triggered invocations.

## I/O contract

routerd starts the plugin executable, writes one `PluginRequest` JSON object to
stdin, and expects one `PluginResult` JSON object on stdout. Timestamps use
RFC3339. Duration strings use Go-style duration syntax such as `300s`.

### PluginRequest

```json
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginRequest",
  "spec": {
    "trigger": {
      "type": "interval",
      "topic": ""
    },
    "startupConfigHash": "sha256:...",
    "effectiveGeneration": 44,
    "previousDynamicGeneration": 12,
    "now": "2026-05-29T12:00:00Z"
  }
}
```

| Field | Meaning |
| --- | --- |
| `spec.trigger` | Why this plugin invocation happened. |
| `spec.startupConfigHash` | Digest of the current startup-config. |
| `spec.effectiveGeneration` | Current effective-config generation before this result. |
| `spec.previousDynamicGeneration` | Last accepted generation for this source. |
| `spec.now` | routerd's invocation timestamp. |

### PluginResult

```json
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginResult",
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "300s",
    "resources": [
      {
        "apiVersion": "hybrid.routerd.net/v1alpha1",
        "kind": "CloudAddressClaim",
        "metadata": { "name": "app-10-0-1-123" },
        "spec": {
          "address": "10.0.1.123/32",
          "providerRef": "oci-prod",
          "peerRef": "onprem-main"
        }
      }
    ],
    "directives": [
      {
        "op": "mask",
        "target": {
          "apiVersion": "net.routerd.net/v1alpha1",
          "kind": "IPv4Route",
          "name": "cloud-app-static-fallback"
        },
        "reason": "CloudAddressClaim/app-10-0-1-123 is active"
      }
    ],
    "actionPlans": [
      {
        "name": "attach-vnic-app",
        "provider": "oci",
        "action": "AttachVNIC",
        "target": {
          "vnicID": "ocid1.vnic.oc1..example",
          "address": "10.0.1.123"
        },
        "undo": {
          "action": "DetachVNIC"
        }
      }
    ],
    "events": [
      {
        "type": "InventoryObserved",
        "message": "observed app private address",
        "attributes": {
          "provider": "oci",
          "address": "10.0.1.123"
        }
      }
    ]
  }
}
```

routerd validates `status.resources` as routerd resources, validates
`status.directives` against dynamic override policy, stores accepted output as a
`DynamicConfigPart`, and derives `expiresAt` from `observedAt + ttl`.

## Current status

The main router features are advanced inside the routerd binary and its managed daemons. The plugin protocol is the safe foundation for site-local extensions; the manifest format and the I/O contract may still change before the protocol is frozen as a stable public surface.

See also [Hybrid cloud edge design](./design-hybrid-cloud-edge.md) and
[Dynamic config reference](./reference/dynamic-config.md).
