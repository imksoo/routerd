# FederationSLO

[Resource configuration reference](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # optional; object
      expiresSoonSeconds: 0 # optional; integer; minimum=0
      lagFailSeconds: 0 # optional; integer; minimum=0
      lagWarnSeconds: 0 # optional; integer; minimum=0
    groupRef: "<string>" # Required; string
    subscription: # optional; object
      maxFailedRuns: 0 # optional; integer; minimum=0
      maxPendingRuns: 0 # optional; integer; minimum=0
```
