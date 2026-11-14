# DHCPv4Reservation

[Resource configuration reference](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # optional; string
    ipAddress: "<string>" # Required; string
    leaseTime: "<string>" # optional; string
    macAddress: "<string>" # Required; string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    scope: "<string>" # optional; string
    server: "<string>" # optional; string
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
