# Telemetry

[Resource configuration reference](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
