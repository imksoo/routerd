# VXLANTunnel

[Resource configuration reference](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # optional; string
    ifname: "<string>" # optional; string
    localAddress: "<string>" # Required; string
    mtu: 0 # optional; integer; minimum=0
    outerDF: "" # optional; string; enum=["", "inherit", "set", "unset"]
    peers: # optional; array
      - "<string>" # string
    tcpMSSClamp: true # optional; boolean
    udpPort: 1 # optional; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # Required; string
    vni: 1 # Required; integer; minimum=1; maximum=16777215
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```
