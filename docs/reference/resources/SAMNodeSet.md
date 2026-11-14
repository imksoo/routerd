# SAMNodeSet

[Resource configuration reference](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # Required; array
      - # object
        eventEndpoint: "<string>" # optional; string
        macAddresses: # optional; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # optional; object
          drain: true # optional; boolean
        maxSecondaryIPs: 0 # optional; integer; minimum=0
        nodeRef: "<string>" # Required; string
        placement: # optional; object
          group: "<string>" # optional; string
          priority: 0 # optional; integer; minimum=0; maximum=1000000
        role: "" # optional; string; enum=["", "onprem", "cloud"]
        routeReflector: true # optional; boolean
        samEndpoint: "<string>" # optional; string
        samEndpointFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        site: "<string>" # optional; string
        wireGuard: # optional; object
          allowedIPs: # optional; array
            - "<string>" # string
          endpoint: "<string>" # optional; string
          persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
          publicKey: "<string>" # optional; string
          transportEndpoint: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
