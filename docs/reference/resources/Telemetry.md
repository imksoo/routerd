# Telemetry

[Resource configuration reference](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

Configures OpenTelemetry environment variables for rendered managed services. It does not define a LogSink or a separate collector process.

## Conditions and references

- Only one selected Telemetry or ObservabilityPipeline with a nonempty OTLP endpoint may supply the generated telemetry environment; multiple selected resources fail rendering. No resource-name reference is involved.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # optional; object
      "<key>": "<string>" # optional; string
    otlp: # Required; object
      endpoint: "<string>" # Required; string
      insecure: true # optional; boolean
    serviceNamespace: "<string>" # optional; string
    signals: # optional; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.otlp` | Required OTLP connection object. |
| `spec.otlp.endpoint` | Required nonblank exporter endpoint; emitted as OTEL_EXPORTER_OTLP_ENDPOINT. No default. This validator checks nonblank content, not endpoint reachability. |
| `spec.otlp.insecure` | Default false. True emits OTEL_EXPORTER_OTLP_INSECURE=true; false does not emit that variable. |
| `spec.serviceNamespace` | OpenTelemetry service namespace, default routerd after trimming whitespace. |
| `spec.attributes` | Resource-attribute map, default empty. Sorted and serialized into OTEL_RESOURCE_ATTRIBUTES as comma-separated key=value entries; values are not escaped by this renderer. |
| `spec.signals` | logs, metrics and traces; default all three. A nonempty list disables omitted signals by emitting their exporter variable as none. It does not itself create a receiver. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `signals`

[Example](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
