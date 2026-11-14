# SelfAddressPolicy

[Resource configuration reference](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    candidates: # Required; array
      - # object
        address: "<string>" # optional; string
        addressSuffix: "<string>" # optional; string
        delegatedAddress: "<string>" # optional; string
        interface: "<string>" # optional; string
        matchSuffix: "<string>" # optional; string
        ordinal: 1 # optional; integer; minimum=1
        source: "delegatedAddress" # Required; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```
