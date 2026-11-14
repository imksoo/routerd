# DerivedEvent

[Resource configuration reference](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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
