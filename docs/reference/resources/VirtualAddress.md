# VirtualAddress

[Resource configuration reference](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # Required; string
    addressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    externalDNS: true # optional; boolean
    family: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # optional; boolean
    hostname: "<string>" # optional; string
    interface: "<string>" # Required; string
    mode: "" # optional; string; enum=["", "static", "vrrp"]
    track: # optional; array
      - # object
        confirmConsecutiveHealthy: 1 # optional; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # optional; integer; minimum=1; maximum=255
        resource: "<string>" # Required; string
        unhealthyPenalty: 0 # optional; integer; minimum=0; maximum=254
    vrrp: # optional; object
      additionalFailoverVMACs: # optional; array
        - # object
          interface: "<string>" # Required; string
          linkLocalAddress: "<string>" # optional; string
          macAddress: "<string>" # Required; string
          parentInterface: "<string>" # Required; string
          withdrawRouterAdvertisement: true # optional; boolean
      authentication: "<string>" # optional; string
      authenticationFrom: # optional; object
        base64: true # optional; boolean
        env: "<string>" # optional; string
        file: "<string>" # optional; string
      failoverVMAC: # optional; object
        interface: "<string>" # Required; string
        linkLocalAddress: "<string>" # optional; string
        macAddress: "<string>" # Required; string
        parentInterface: "<string>" # Required; string
        withdrawRouterAdvertisement: true # optional; boolean
      gracefulActivation: # optional; object
        readyWhen: # Required; object
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
        timeout: "<string>" # optional; string
      peers: # optional; array
        - "<string>" # string
      preempt: true # optional; boolean
      priority: 1 # optional; integer; minimum=1; maximum=254
      virtualRouterID: 1 # Required; integer; minimum=1; maximum=255
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

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
