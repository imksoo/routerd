# TunnelInterface

[Resource configuration reference](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # optional; string
    encapDport: 1 # optional; integer; minimum=1; maximum=65535
    encapSport: 1 # optional; integer; minimum=1; maximum=65535
    key: 0 # optional; integer; minimum=0; maximum=4294967295
    local: "<string>" # optional; string
    localFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    mode: "ipip" # Required; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    pathMTU: # optional; object
      forceFragmentIPv4: true # optional; boolean
    peerAddress: "<string>" # optional; string
    remote: "<string>" # optional; string
    remoteFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    trustedUnderlay: true # Required; boolean
    ttl: 1 # optional; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
