# DSLiteTunnel

[Resource configuration reference](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # optional; integer; minimum=1
    aftrAddressSelection: "ordinal" # optional; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # optional; array
      - "<string>" # string
    aftrFQDN: "<string>" # optional; string
    aftrFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    aftrIPv6: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    enabled: true # optional; boolean
    encapsulationLimit: "<string>" # optional; string
    interface: "<string>" # Required; string
    localAddress: "<string>" # optional; string
    localAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    localAddressSource: "interface" # optional; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # optional; string
    localDelegatedAddress: "<string>" # optional; string
    mtu: 1280 # optional; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # optional; string
    routeMetric: 0 # optional; integer; minimum=0
    tunnelName: "<string>" # optional; string
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
