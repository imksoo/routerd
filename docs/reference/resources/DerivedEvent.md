# DerivedEvent

[Resource configuration reference](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Publish asserted/retracted events when conditions over resource status change. Unlike EventRule, this evaluates current status rather than an event sequence.

## Conditions and references

- topic and a nonempty inputs list are required. Inputs use the older field/equals/not_empty/any_of structure, not common resource/field sources, dependsOn or when.
- Current evaluation reads net.routerd.net/v1alpha1 status through Kind/name.status.field, optionally wrapped in ${...}. Strings without .status. are treated as literal values, not status references. These inputs are not checked for source existence or provided-field compatibility by ordinary status-source validation.
- The controller evaluates initially, on local routerd.** events other than its own, and periodically (default 5s). Transition memory and hysteresis timers restart after process restart; saved status does not restore that memory.
- retractWhen is validated but not read by the current evaluator. Retraction follows the inverse of emitWhen, so changing retractWhen does not alter behavior.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # optional; boolean
    emitWhen: "all_true" # optional; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # optional; string
    inputs: # Required; array
      - # object
        any_of: # optional; array
          - # array
            - # object
              equals: "<string>" # optional; string
              field: "<string>" # optional; string
              not_empty: true # optional; boolean
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        not_empty: true # optional; boolean
    retractWhen: "any_false" # optional; string; enum=["any_false", "all_false"]
    topic: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.topic` | Required base event topic. Emits <topic>.asserted when true and <topic>.retracted when false; no bare-topic event is emitted. |
| `spec.inputs` | Required nonempty list of predicates. emitWhen combines entries. An empty predicate object evaluates true, so write explicit conditions. |
| `spec.inputs[].field` | Optional string, default empty. Kind/name.status.field reads a top-level status member; missing source/member yields empty. Otherwise evaluated as a literal. Arrays become JSON strings, so equals compares the complete serialized value. |
| `spec.inputs[].equals` | Optional exact case-sensitive comparison, default empty means no equality comparison. With a nonempty field and neither equals nor not_empty, the implicit condition is nonblank. |
| `spec.inputs[].not_empty` | Default false. True rejects blank/whitespace values; when equals is also nonempty, both constraints must pass. |
| `spec.inputs[].any_of` | Optional OR-list of AND-groups. When nonempty, takes precedence over this input’s field/equals/not_empty. Every predicate in a group must pass; empty groups are false. |
| `spec.inputs[].any_of[][].field` | Group predicate field; same status/literal rules as inputs[].field. |
| `spec.inputs[].any_of[][].equals` | Group predicate equality; same empty/default semantics as inputs[].equals. |
| `spec.inputs[].any_of[][].not_empty` | Group predicate nonblank check, default false; same semantics as inputs[].not_empty. |
| `spec.emitWhen` | Default all_true: every input must pass. any_true: at least one input must pass. This target also determines retraction when it becomes false. |
| `spec.retractWhen` | Accepted values any_false (default) and all_false; currently unused after validation. Do not rely on an independent retraction predicate. |
| `spec.hysteresis` | Optional Go duration, default 0 for immediate transitions. A new target must persist until the timer fires, when it is rechecked; a return to the prior target cancels it. Initial state is established without waiting. Validation accepts negative durations, which fire immediately. |
| `spec.emitInitial` | Default false: save the initial asserted/retracted status without emitting an event. True emits the initial result once for each new controller state, including after restart. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/derived/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/derived/engine.go)
