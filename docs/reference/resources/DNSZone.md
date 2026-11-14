# DNSZone

[Resource configuration reference](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # optional; object
      ddns: true # optional; boolean
      hostnameSuffix: "<string>" # optional; string
      leaseFile: "<string>" # optional; string
      sources: # optional; array
        - "<string>" # string
      ttl: 0 # optional; integer; minimum=0
    dnssec: # optional; object
      enabled: true # optional; boolean
      mode: "" # optional; string; enum=["", "validate", "sign"]
    records: # optional; array
      - # object
        hostname: "<string>" # Required; string
        ipv4: "<string>" # optional; string
        ipv4From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ipv6: "<string>" # optional; string
        ipv6From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ttl: 0 # optional; integer; minimum=0
    reverseZones: # optional; array
      - # object
        name: "<string>" # Required; string
    ttl: 0 # optional; integer; minimum=0
    zone: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
