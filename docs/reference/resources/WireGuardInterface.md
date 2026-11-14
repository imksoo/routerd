# WireGuardInterface

[Resource configuration reference](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    listenPort: 1 # optional; integer; minimum=1; maximum=65535
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    privateKey: "<string>" # optional; string
    privateKeyFile: "<string>" # optional; string
    selfNodeRef: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
