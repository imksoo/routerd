# HybridRoute

[Resource configuration reference](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # Required; array
      - "<string>" # string
    healthCheckRef: "<string>" # optional; string
    install: # optional; object
      metric: 0 # optional; integer; minimum=0
      table: "" # optional; string; enum=["", "main"]
    peerRef: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
