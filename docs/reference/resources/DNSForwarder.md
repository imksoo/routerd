# DNSForwarder

[Resource configuration reference](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # optional; boolean
    healthcheck: # optional; object
      failThreshold: 1 # optional; integer; minimum=1
      interval: "<string>" # optional; string
      passThreshold: 1 # optional; integer; minimum=1
      timeout: "<string>" # optional; string
    match: # Required; array
      - "<string>" # string
    queryTimeout: "<string>" # optional; string
    resolver: "<string>" # Required; string
    upstreams: # optional; array
      - "<string>" # string
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
    zoneRefs: # optional; array
      - "<string>" # string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
