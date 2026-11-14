# IPsecConnection

[Resource configuration reference](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # optional; string
    cloudProviderHint: "" # optional; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # Required; string
    localAddress: "<string>" # Required; string
    phase1Proposals: # optional; array
      - "<string>" # string
    phase2Proposals: # optional; array
      - "<string>" # string
    preSharedKey: "<string>" # optional; string
    remoteAddress: "<string>" # Required; string
    rightSubnet: "<string>" # Required; string
```
