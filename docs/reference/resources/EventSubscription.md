# EventSubscription

[Resource configuration reference](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    match: # Required; object
      payload: # optional; object
        "<key>": "<string>" # optional; string
      sourceNodes: # optional; array
        - "<string>" # string
      subjectPrefixes: # optional; array
        - "<string>" # string
      types: # Required; array
        - "<string>" # string
    trigger: # Required; object
      batchWindow: "<string>" # optional; string
      debounce: "<string>" # optional; string
      pluginRef: "<string>" # Required; string
```
