# FirewallRule

[Resource configuration reference](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # Required; string; enum=["accept", "drop", "reject"]
    connLimit: # optional; object
      log: true # optional; boolean
      maxPerSource: 0 # optional; integer; minimum=0
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationPorts: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    fromZone: "<string>" # Required; string
    icmpType: "<string>" # optional; string
    icmpv6Type: "<string>" # optional; string
    log: true # optional; boolean
    port: 0 # optional; integer; minimum=0; maximum=65535
    protocol: "" # optional; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # optional; object
      burst: 0 # optional; integer; minimum=0
      log: true # optional; boolean
      packetsPerSecond: 0 # optional; integer; minimum=0
      per: "" # optional; string; enum=["", "second", "minute"]
      rate: 0 # optional; integer; minimum=0
      unit: "" # optional; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # optional; array
      - "<string>" # string
    srcCIDRs: # optional; array
      - "<string>" # string
    toZone: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
