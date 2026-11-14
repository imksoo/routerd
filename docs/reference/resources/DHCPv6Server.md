# DHCPv6Server

[Resource configuration reference](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    configPath: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    delegatedAddress: "<string>" # optional; string
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "self" # optional; string; enum=["self", "static", "none"]
    domainSearch: # optional; array
      - "<string>" # string
    domainSearchFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    interface: "<string>" # optional; string
    leaseFile: "<string>" # optional; string
    leaseTime: "<string>" # optional; string
    listenInterfaces: # optional; array
      - "<string>" # string
    logDHCP: true # optional; boolean
    managed: true # optional; boolean
    mode: "stateless" # optional; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # optional; string
    rapidCommit: true # optional; boolean
    role: "server" # optional; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq"]
    sntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    sntpServers: # optional; array
      - "<string>" # string
    stickyHoldDays: 0 # optional; integer; minimum=0
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
