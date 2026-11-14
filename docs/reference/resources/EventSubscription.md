# EventSubscription

[Resource configuration reference](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Match nonexpired persisted federation events and invoke a local Plugin, storing its result as a DynamicConfigPart.

## Conditions and references

- groupRef, match.types (at least one) and trigger.pluginRef are required. Group and plugin references are bare metadata names, not Kind/name. They are not checked for existence by this validator: a missing Plugin yields Pending at runtime; a group with no matching stored events remains Idle.
- Matching is case-sensitive. Types, subject prefixes, payload entries and source-node restrictions combine with AND; list alternatives combine with OR. This is exact event-type matching, not local topic wildcard matching.
- The controller polls stored events (normally every 5s). Succeeded subscription/event pairs are skipped; failed pairs are retried up to three attempts; pending pairs can be retried after interruption. Processing is at least once, not an exactly-once guarantee for plugin side effects.
- With neither batching option positive, one invocation receives the current eligible batch, not necessarily one event. Batching timers are in memory; event/run records are persisted. Scheduled batches flush early at 1024 events or approximately 4 MiB of selected payload/identity data.
- The plugin receives matched event facts plus only the config resources explicitly allowlisted by Plugin.context.resources; secrets are redacted. Result TTL and generated resources come from PluginResult, not this subscription. Distinct event batches produce distinct active DynamicConfigPart sources.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    match:
      [ payload:
          [ <name>: <string> ]
      ]
      [ sourceNodes:
          - <string>
      ]
      [ subjectPrefixes:
          - <string>
      ]
      types:
        - <string>
    trigger:
      [ batchWindow: <string> ]
      [ debounce: <string> ]
      pluginRef: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.groupRef` | **Required** | string | Required bare EventGroup name used to select stored event group_name. No default; does not automatically create or start a group. |
| `spec.match` | **Required** | object | Required matching object; types must be nonempty. Optional empty categories impose no restriction. |
| `spec.match.types` | **Required** | array | Required exact event-type list with nonblank entries. At least one type must match; wildcards are literal strings. |
| `spec.match.subjectPrefixes` | *Optional* | array | Optional subject prefix list, default empty allows all. Matching is strings.HasPrefix, not path-component or wildcard matching. |
| `spec.match.payload` | *Optional* | object | Optional key-to-string equality map; every entry must match. Keys must be nonblank. A missing payload key reads as empty, so a requested empty value also matches a missing key. |
| `spec.match.sourceNodes` | *Optional* | array | Optional exact sourceNode allowlist, default empty permits all. It does not automatically exclude this node’s own events; specify the intended source identities. |
| `spec.trigger` | **Required** | object | Required local plugin invocation settings. This is distinct from Plugin.triggers and is executed by the subscription controller. |
| `spec.trigger.pluginRef` | **Required** | string | Required bare Plugin metadata.name, not Plugin/name. Missing plugin yields Pending. Execution uses that Plugin’s executable, timeout, environment and context restrictions. |
| `spec.trigger.batchWindow` | *Optional* | string | Optional Go duration measured from the first selected event; default empty/0 means no batching cap unless debounce is positive, when the cap defaults to 1m. Positive batchWindow bounds repeated debounce. Negative durations pass validation and act as nonpositive. |
| `spec.trigger.debounce` | *Optional* | string | Optional Go duration after the latest newly selected event, default empty/0 disabled. Reobserving the same event does not extend it. The earlier of debounce and batchWindow deadlines fires; no positive batchWindow gives a 1m cap. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventsubscription/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventsubscription/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
