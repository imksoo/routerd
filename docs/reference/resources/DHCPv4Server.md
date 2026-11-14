# DHCPv4Server

[Resource configuration reference](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    authoritative: true # optional; boolean
    dns: # optional; object
      cacheSize: 0 # optional; integer; minimum=0
      enabled: true # optional; boolean
      upstreamInterface: "<string>" # optional; string
      upstreamServers: # optional; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # optional; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # optional; string
    domainFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    gateway: "<string>" # optional; string
    gatewayFrom: # optional; object
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
    ntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    ntpServers: # optional; array
      - "<string>" # string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    profiles: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
    rangeEnd: "<string>" # optional; string
    rangeStart: "<string>" # optional; string
    role: "server" # optional; string; enum=["server", "transit"]
    router: "<string>" # optional; string
    routerSource: "interfaceAddress" # optional; string; enum=["interfaceAddress", "static", "none"]
    scopes: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        match: # Required; object
          macAddresses: # optional; array
            - "<string>" # string
          ouiPrefixes: # optional; array
            - "<string>" # string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
        profileRef: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq", "kea", "dhcpd"]
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
