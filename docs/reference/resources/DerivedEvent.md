# DerivedEvent

[Resource configuration reference](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Publish asserted/retracted events when conditions over resource status change. Unlike EventRule, this evaluates current status rather than an event sequence.

## Conditions and references

- topic and a nonempty inputs list are required. Inputs use the older field/equals/not_empty/any_of structure, not common resource/field sources, dependsOn or when.
- Current evaluation reads net.routerd.net/v1alpha1 status through Kind/name.status.field, optionally wrapped in ${...}. Strings without .status. are treated as literal values, not status references. These inputs are not checked for source existence or provided-field compatibility by ordinary status-source validation.
- The controller evaluates initially, on local routerd.** events other than its own, and periodically (default 5s). Transition memory and hysteresis timers restart after process restart; saved status does not restore that memory.
- retractWhen is validated but not read by the current evaluator. Retraction follows the inverse of emitWhen, so changing retractWhen does not alter behavior.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    [ emitInitial: <boolean> ]
    [ emitWhen: ("all_true" | "any_true") ]
    [ hysteresis: <string> ]
    inputs:
      -
        [ any_of:
            -
              -
                [ equals: <string> ]
                [ field: <string> ]
                [ not_empty: <boolean> ]
        ]
        [ equals: <string> ]
        [ field: <string> ]
        [ not_empty: <boolean> ]
    [ retractWhen: ("any_false" | "all_false") ]
    topic: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.topic` | **Required** | string | Required base event topic. Emits <topic>.asserted when true and <topic>.retracted when false; no bare-topic event is emitted. |
| `spec.inputs` | **Required** | array | Required nonempty list of predicates. emitWhen combines entries. An empty predicate object evaluates true, so write explicit conditions. |
| `spec.inputs[].field` | *Optional* | string | Optional string, default empty. Kind/name.status.field reads a top-level status member; missing source/member yields empty. Otherwise evaluated as a literal. Arrays become JSON strings, so equals compares the complete serialized value. |
| `spec.inputs[].equals` | *Optional* | string | Optional exact case-sensitive comparison, default empty means no equality comparison. With a nonempty field and neither equals nor not_empty, the implicit condition is nonblank. |
| `spec.inputs[].not_empty` | *Optional* | boolean | Default false. True rejects blank/whitespace values; when equals is also nonempty, both constraints must pass. |
| `spec.inputs[].any_of` | *Optional* | array | Optional OR-list of AND-groups. When nonempty, takes precedence over this input’s field/equals/not_empty. Every predicate in a group must pass; empty groups are false. |
| `spec.inputs[].any_of[][].field` | *Optional* | string | Group predicate field; same status/literal rules as inputs[].field. |
| `spec.inputs[].any_of[][].equals` | *Optional* | string | Group predicate equality; same empty/default semantics as inputs[].equals. |
| `spec.inputs[].any_of[][].not_empty` | *Optional* | boolean | Group predicate nonblank check, default false; same semantics as inputs[].not_empty. |
| `spec.emitWhen` | *Optional* | string; enum=["all_true", "any_true"] | Default all_true: every input must pass. any_true: at least one input must pass. This target also determines retraction when it becomes false. |
| `spec.retractWhen` | *Optional* | string; enum=["any_false", "all_false"] | Accepted values any_false (default) and all_false; currently unused after validation. Do not rely on an independent retraction predicate. |
| `spec.hysteresis` | *Optional* | string | Optional Go duration, default 0 for immediate transitions. A new target must persist until the timer fires, when it is rechecked; a return to the prior target cancels it. Initial state is established without waiting. Validation accepts negative durations, which fire immediately. |
| `spec.emitInitial` | *Optional* | boolean | Default false: save the initial asserted/retracted status without emitting an event. True emits the initial result once for each new controller state, including after restart. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/derived/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/derived/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `topic`
