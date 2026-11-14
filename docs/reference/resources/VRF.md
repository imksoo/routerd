# VRF

[Resource configuration reference](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    members: # optional; array
      - "<string>" # string
    routeTable: 1 # Required; integer; minimum=1; maximum=4294967295
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
