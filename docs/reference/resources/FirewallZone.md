# FirewallZone

[Resource configuration reference](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # Required; array
      - "<string>" # string
    role: "untrust" # Required; string; enum=["untrust", "trust", "mgmt"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
