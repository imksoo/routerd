# NAT44FlowDNATPinhole

[Resource configuration reference](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # optional; object
      key: "" # optional; string; enum=["", "localPort"]
    fromInterfaceRefs: # optional; array
      - "<string>" # string
    inbound: # Required; object
      sourcePorts: # Required; array
        - "<string>" # string
    outbound: # Required; object
      destinationCIDRs: # optional; array
        - "<string>" # string
      destinationPorts: # Required; array
        - "<string>" # string
      destinationSetRefs: # optional; array
        - "<string>" # string
      protocol: "udp" # Required; string; enum=["udp"]
      sourceCIDRs: # optional; array
        - "<string>" # string
      sourceSetRef: "<string>" # optional; string
    timeout: "<string>" # optional; string
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
