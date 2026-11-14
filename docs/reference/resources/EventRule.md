# EventRule

[Resource configuration reference](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # Required; object
      attributes: # optional; object
        "<key>": "<string>" # optional; string
      topic: "<string>" # Required; string
    pattern: # Required; object
      allow_missing_correlation: true # optional; boolean
      correlate_by: "<string>" # optional; string
      duration: "<string>" # optional; string
      expected: "<string>" # optional; string
      interval: "<string>" # optional; string
      operator: "all_of" # Required; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # optional; string
      rate: 0 # optional; integer; minimum=0
      strict: true # optional; boolean
      threshold: 0 # optional; integer; minimum=0
      topic: "<string>" # optional; string
      topics: # optional; array
        - "<string>" # string
      trigger: "<string>" # optional; string
      window: "<string>" # optional; string
```
