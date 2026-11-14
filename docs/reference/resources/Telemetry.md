# Telemetry

[Resource configuration reference](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

Configures OpenTelemetry environment variables for rendered managed services. It does not define a LogSink or a separate collector process.

## Conditions and references

- Only one selected Telemetry or ObservabilityPipeline with a nonempty OTLP endpoint may supply the generated telemetry environment; multiple selected resources fail rendering. No resource-name reference is involved.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    [ attributes:
        [ <name>: <string> ]
    ]
    otlp:
      endpoint: <string>
      [ insecure: <boolean> ]
    [ serviceNamespace: <string> ]
    [ signals:
        - ("logs" | "metrics" | "traces")
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.otlp` | **Required** | object | Required OTLP connection object. |
| `spec.otlp.endpoint` | **Required** | string | Required nonblank exporter endpoint; emitted as OTEL_EXPORTER_OTLP_ENDPOINT. No default. This validator checks nonblank content, not endpoint reachability. |
| `spec.otlp.insecure` | *Optional* | boolean | Default false. True emits OTEL_EXPORTER_OTLP_INSECURE=true; false does not emit that variable. |
| `spec.serviceNamespace` | *Optional* | string | OpenTelemetry service namespace, default routerd after trimming whitespace. |
| `spec.attributes` | *Optional* | object | Resource-attribute map, default empty. Sorted and serialized into OTEL_RESOURCE_ATTRIBUTES as comma-separated key=value entries; values are not escaped by this renderer. |
| `spec.signals` | *Optional* | array | logs, metrics and traces; default all three. A nonempty list disables omitted signals by emitting their exporter variable as none. It does not itself create a receiver. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `signals`

[Example](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
