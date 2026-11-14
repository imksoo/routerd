# SAMSubnetPolicy

[Resource configuration reference](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    poolRef: "<string>" # Required; string
    shards: # Required; array
      - # object
        assignedNodes: # Required; array
          - "<string>" # string
        prefix: "<string>" # Required; string
    sourcePrefix: "<string>" # Required; string
```
