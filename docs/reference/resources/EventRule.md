# EventRule

[Resource configuration reference](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Derive an event from patterns in the persisted local event journal, with optional per-correlation state.

## Conditions and references

- pattern.operator and emit.topic are required. At least one of topic, topics, trigger or expected must be supplied, but this loose validation does not guarantee a usable pattern: expected alone is not an input list and absence needs a trigger/topic. No topic-to-resource existence check is performed.
- Input order is topics, then topic, then trigger. all_of and sequence use exact topic strings; any_of, window, count, throttle, debounce and absence support dot-separated * (one segment) and ** (zero or more segments).
- all_of remembers each seen topic and does not clear the set after firing, so later matching events fire again once all have been seen. sequence resets only after completion or a strict out-of-order listed event. window clears its time bucket after reaching threshold; count resets its counter after reaching threshold.
- Rule counters, partial sequences and timers are in memory and restart from empty after process restart. The journal consumer cursor is persisted; a new consumer starts at the current journal tip. Delivery is at least once, so cursor-save failure can repeat an input. Events emitted by routerd-eventrule are excluded from all rules to avoid chaining loops.
- Correlation state is capped at 4096 keys per rule; the least recently observed key is evicted with its counters and timers. Time-window/throttle comparisons use input event timestamps, while absence/debounce timers use elapsed process time.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit:
      [ attributes:
          [ <name>: <string> ]
      ]
      topic: <string>
    pattern:
      [ allow_missing_correlation: <boolean> ]
      [ correlate_by: <string> ]
      [ duration: <string> ]
      [ expected: <string> ]
      [ interval: <string> ]
      operator: ("all_of" | "any_of" | "sequence" | "window" | "absence" | "throttle" | "debounce" | "count")
      [ quiet: <string> ]
      [ rate: <integer> ]
      [ strict: <boolean> ]
      [ threshold: <integer> ]
      [ topic: <string> ]
      [ topics:
          - <string>
      ]
      [ trigger: <string> ]
      [ window: <string> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.pattern` | **Required** | object | Required pattern object; fields not used by the selected operator are not rejected as mutually exclusive. |
| `spec.pattern.operator` | **Required** | string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"] | Required: all_of, any_of, sequence, window, absence, throttle, debounce or count. No default. |
| `spec.pattern.topic` | *Optional* | string | Optional single input topic appended after topics. For absence, used as trigger only when trigger is empty. Default empty. |
| `spec.pattern.topics` | *Optional* | array | Optional ordered input list, default empty. Order matters for sequence; all_of requires every listed exact topic to have occurred. |
| `spec.pattern.trigger` | *Optional* | string | Optional absence trigger, overriding topic for starting/restarting its timer. Also appended to ordinary input lists after topic. Default empty. |
| `spec.pattern.expected` | *Optional* | string | Optional cancellation topic for absence. Matching cancels a pending timer for the same correlation key and takes precedence over trigger matching. Not an ordinary input topic. Default empty: no cancellation. |
| `spec.pattern.duration` | *Optional* | string | Optional Go duration fallback: window/throttle/absence default 1m; debounce defaults 1s. Operator-specific window/interval/quiet takes precedence. Valid durations are accepted even when zero or negative; use positive values for delayed behavior. |
| `spec.pattern.window` | *Optional* | string | window operator lookback duration; overrides duration, otherwise 1m. Events at or after the cutoff count; threshold firing clears the bucket. Unused by other operators. |
| `spec.pattern.quiet` | *Optional* | string | debounce quiet duration; overrides duration, otherwise 1s. Each matching input restarts the timer; emission uses the last matching input. |
| `spec.pattern.interval` | *Optional* | string | throttle minimum time between emissions per correlation key; overrides duration, otherwise 1m. First matching event emits immediately. |
| `spec.pattern.rate` | *Optional* | integer; minimum=0 | Nonnegative integer accepted by validation, default 0, but not read by the current engine. It does not control throttle; use interval. |
| `spec.pattern.threshold` | *Optional* | integer; minimum=0 | Nonnegative event count for window/count; omitted or 0 means 1. Unused by other operators. |
| `spec.pattern.correlate_by` | *Optional* | string | Optional attributes.<key>, resource.name/kind/apiVersion or daemon.instance/kind. Empty uses the shared key default. Input topics are strings, not Kind/name references. |
| `spec.pattern.allow_missing_correlation` | *Optional* | boolean | Default false: skip inputs with missing selected correlation and increment warningCount. True groups missing values under an empty key. For resource selectors, an existing resource object with an empty selected member is accepted even when false. |
| `spec.pattern.strict` | *Optional* | boolean | Default false. sequence only: a listed topic arriving out of order resets progress to zero; that mismatching event is not reused as a new first item. Unlisted topics are ignored. |
| `spec.emit` | **Required** | object | Required output event object. Emitted severity is info and resource is this EventRule. |
| `spec.emit.topic` | **Required** | string | Required nonempty emitted event type; no default. The rule engine does not consume its own emitted events. |
| `spec.emit.attributes` | *Optional* | object | Optional string map. Expands ${event.type}, ${correlation}, ${count}, ${resource.name/kind/apiVersion} and ${attributes.<key>}; unresolved placeholders remain literal. Positive count and nonempty correlation overwrite those output keys. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/eventrule/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/eventrule/engine.go) · [pkg/eventconsumer/consumer.go](https://github.com/imksoo/routerd/blob/main/pkg/eventconsumer/consumer.go) · [pkg/bus/bus.go](https://github.com/imksoo/routerd/blob/main/pkg/bus/bus.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `topic`
