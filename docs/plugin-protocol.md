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
not fetch plugins from a remote registry or install plugins remotely.

Plugin output is always validated before it is stored as dynamic-config or used
to derive effective-config. A plugin can propose resources, directives,
provider action plans, and events. `actionPlans` are inert inside dynamic-config
and are never executed by the plugin runner or merge path. They can be imported
into the provider-action journal and handed to an executor plugin only after
`ProviderActionPolicy`, approval, allowlist, and dry-run/live mode gates pass.

![Plugin dynamic config diagram showing trusted local plugin observations flowing into DynamicConfigPart and inert provider action plans flowing separately into the gated action journal and executor plugin path](/img/diagrams/dynamic-config-provider-actions.png)

## Resource shapes

A plugin is declared as a local executable and optional trigger set:

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: oci-inventory
spec:
  executable: /usr/local/libexec/routerd/plugins/oci-inventory/bin/oci-inventory
  timeout: 10s
  capabilities: [observe.cloud, propose.dynamicConfig]
  triggers:
    - type: interval
      every: 300s
    - type: event
      topic: routerd.cloud.oci.refresh
```

A dynamic config source binds a plugin to dynamic-config production policy:

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata:
  name: oci-inventory
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

The runner requires `spec.executable` to be an absolute executable file. Plugin
capabilities are currently `observe.cloud`, `observe.providerPrivateIPs`,
`propose.dynamicConfig`, `propose.providerAction`, and
`execute.providerAction`. Interval triggers use `every`; event triggers use
`topic`.

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

The child process receives a minimal environment: `PATH` from routerd's
environment, or a fixed system fallback if `PATH` is unset, plus explicit
`Plugin.spec.env` entries. routerd does not pass through the full parent
environment.

### PluginRequest

```json
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginRequest",
  "metadata": {
    "name": "oci-inventory"
  },
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
  "metadata": {
    "name": "oci-inventory"
  },
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "300s",
    "resources": [
      {
        "apiVersion": "hybrid.routerd.net/v1alpha1",
        "kind": "RemoteAddressClaim",
        "metadata": { "name": "app-10-0-1-123" },
        "spec": {
          "domainRef": "cloudedge-same-subnet",
          "address": "10.0.1.123/32",
          "ownerSide": "cloud",
          "capture": {
            "type": "provider-secondary-ip",
            "providerRef": "oci-prod",
            "providerMode": "vnic-private-ip",
            "nicRef": "ocid1.vnic.oc1..example"
          },
          "delivery": {
            "peerRef": "cloud-main",
            "mode": "route",
            "tunnelInterface": "wg-hybrid"
          }
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
        "reason": "RemoteAddressClaim/app-10-0-1-123 is active"
      }
    ],
    "actionPlans": [
      {
        "name": "assign-cloud-secondary-ip",
        "provider": "oci",
        "action": "assign-secondary-ip",
        "target": {
          "nicRef": "ocid1.vnic.oc1..example",
          "address": "10.0.1.123"
        },
        "undo": {
          "action": "unassign-secondary-ip"
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

routerd decodes plugin stdout with YAML decoding, even when the plugin emits
JSON, so resource specs are restored to typed routerd structs. It validates the
plugin result shape, stores accepted output as a `DynamicConfigPart`, and derives
`expiresAt` from `observedAt + ttl`. Full effective-config validation, including
dynamic override policy evaluation, happens when dynamic parts are merged with
startup config.

`actionPlans` describe provider operations an operator may choose to import into
the provider-action journal. The plugin result itself must stay a dry-run plan;
`mode: execute` is rejected. Live provider mutation, when used, happens only via
`routerctl action execute --approved` or the daemon auto-execution gate, and the
executor plugin receives no routerd-held secrets.

### ObservePrivateIPsResult

Plugins with `observe.providerPrivateIPs` return
`providerinventory.routerd.net/v1alpha1` `ObservePrivateIPsResult` objects. The
legacy `status.ips` field remains wire-compatible and is treated as observed
candidate addresses for ownership-discovery events. New plugins should also set
`status.localIPs` to the authoritative local provider inventory for the scanned
VPC/VNet/VCN or subnet, including VM NIC and private-endpoint addresses before
routerd applies trap exclusions or ownership selectors. If `localIPs` is absent,
routerd falls back to `observedCandidates` and then `ips`.

`status.observedCandidates` can be used when a plugin wants to return a narrower
event-emission candidate set while still exposing the full local inventory in
`localIPs`. SAM's ownership resolver uses `localIPs` for shadow locality
classification; the existing discovery event path continues to use
`observedCandidates` or legacy `ips`.

Each private-IP record should set `resourceRef` to the owning compute instance
ID when the provider can report it, and `resourceType` to distinguish router
NICs from ordinary instance NICs or private endpoints. `status.self` may also
set `resourceRef`/`resourceType` for the local router instance. SAM uses this
identity to distinguish a secondary IP captured onto the router instance from a
real home client address on the same provider subnet.

## CLI

The MVP operator commands are:

```text
routerctl plugin list [--config <startup>] [-o table|json|yaml]
routerctl plugin run <name> [--dry-run] [--config <startup>] [--state-file <db>] [-o table|json|yaml]
routerctl action import|list|show|approve|execute|journal|rollback ...
```

`plugin run --dry-run` executes the plugin and prints the candidate
`DynamicConfigPart` without writing state. Without `--dry-run`, routerctl records
the plugin run and stores the validated dynamic part in the local state
database.

## Current status

The main router features are advanced inside the routerd binary and its managed daemons. The plugin protocol is the safe foundation for site-local extensions; the manifest format and the I/O contract may still change before the protocol is frozen as a stable public surface.

See also [Hybrid cloud edge design](/docs/design-hybrid-cloud-edge) and
[Dynamic config reference](./reference/dynamic-config.md).
