# DNSUpstream

[Resource configuration reference](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # optional; string
    addressFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    bootstrap: # optional; array
      - "<string>" # string
    bootstrapFrom: # optional; array
      - # object
        field: "<string>" # Required; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    path: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    protocol: "udp" # Required; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # optional; string
    tlsName: "<string>" # optional; string
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
