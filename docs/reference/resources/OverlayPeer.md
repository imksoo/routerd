# OverlayPeer

[Resource configuration reference](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # Required; string
    pathMTU: # optional; object
      forceFragmentIPv4: true # optional; boolean
    remote: # optional; object
      address: "<string>" # optional; string
      nodeID: "<string>" # optional; string
    role: "onprem" # Required; string; enum=["onprem", "cloud"]
    underlay: # Required; object
      address: "<string>" # optional; string
      interface: "<string>" # optional; string
      type: "wireguard" # Required; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
