# NAT44Rule

[Resource configuration reference](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    egressInterface: "<string>" # optional; string
    egressPolicyRef: "<string>" # optional; string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    outboundInterface: "<string>" # optional; string
    snatAddress: "<string>" # optional; string
    snatAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    sourceCIDRs: # optional; array
      - "<string>" # string
    sourceRanges: # optional; array
      - "<string>" # string
    translation: # optional; object
      address: "<string>" # optional; string
      addresses: # optional; array
        - "<string>" # string
      portMapping: # optional; object
        end: 1 # optional; integer; minimum=1; maximum=65535
        start: 1 # optional; integer; minimum=1; maximum=65535
        type: "auto" # optional; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # Required; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # optional; string; enum=["masquerade", "snat"]
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
