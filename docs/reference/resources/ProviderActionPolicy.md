# ProviderActionPolicy

[Resource configuration reference](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # optional; boolean
    allowedActions: # optional; array
      - "<string>" # string
    allowedCIDRs: # optional; array
      - "<string>" # string
    allowedProviderRefs: # optional; array
      - "<string>" # string
    allowedProviders: # optional; array
      - "<string>" # string
    dryRunOnly: true # optional; boolean
    enabled: true # optional; boolean
    executionWindow: "<string>" # optional; string
    maxActionsPerRun: 0 # optional; integer
    requireApproval: true # optional; boolean
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
