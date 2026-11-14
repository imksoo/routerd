# FirewallRule

[Resource configuration reference](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Declare a named accept/drop/reject rule for a zone path, with address/protocol/port matches and Linux rate/connection predicates.

## Conditions and references

- fromZone, toZone and action are required. Zone names must exist or be self. Although fromZone=self passes validation, these renderers do not generate a self-origin chain from it: local outbound traffic follows the OS/output policy instead.
- Rules are sorted by metadata.name, not YAML order. Existing state, IPv6 control traffic and generated service/ingress allowances can be accepted earlier. PF also emits broad implicit passes before explicit rules; for forwarded PF rules toZone does not add an output-interface/destination-zone match, so specify destinationCIDRs when destination scope is needed.
- port and destinationPorts are mutually exclusive. Any source/destination port requires tcp or udp. A range must be the only entry in its list; numeric ports and start-end strings are accepted. ICMP type selectors require their matching protocol.
- On Linux, source CIDRs are alternatives; destinationCIDRs and destinationSetRefs form a union; excluded sets are then subtracted. Source/destination/protocol families must be compatible. PF currently ignores address-set references, rateLimit and connLimit despite validation accepting them.
- Linux rateLimit matches traffic OVER the configured rate, and connLimit matches counts OVER maxPerSource; the rule action is applied to the excess. Use a suitable drop/reject action for limiting. With both enabled they produce separate alternatives, not an AND of both thresholds.

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

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.fromZone` | Required bare FirewallZone name or self. self is accepted but does not produce local-outbound filtering here. |
| `spec.toZone` | Required bare FirewallZone name or self. Linux matches zone-pair input/output interfaces; self selects local input. PF self uses router addresses; other toZone values do not become an output-zone match. |
| `spec.srcCIDRs` | Optional IPv4/IPv6 source-prefix alternatives; default empty means any source. Prefixes must parse; Linux masks host bits before rendering. |
| `spec.destinationCIDRs` | Optional destination-prefix alternatives, default empty. Linux unions these with destinationSetRefs; with neither specified, destination is unrestricted within the zone path. PF uses only these static destination prefixes. |
| `spec.destinationSetRefs` | Optional existing IPAddressSet/<name> references, default empty; current PF does not enforce them. Linux references the enabled IPv4/IPv6 nftables sets. |
| `spec.excludeDestinationSetRefs` | Optional existing IPAddressSet/<name> references subtracted from Linux destination matches. Default empty; current PF ignores them. |
| `spec.protocol` | Default empty means any protocol. Allowed tcp, udp, icmp, icmpv6, ipv6-icmp, ipip. Here ipip is IPv4 encapsulation in IPv6 (IPv6 next-header 4), not an IPv4-underlay tunnel protocol rule. |
| `spec.port` | Optional single destination port, default 0 means absent. Positive 1–65535 requires tcp/udp and excludes destinationPorts. |
| `spec.sourcePorts` | Optional tcp/udp source-port OR list; each port 1–65535 or a sole start-end range. Default empty means any source port. |
| `spec.destinationPorts` | Optional tcp/udp destination-port OR list with the same port/range rules. Mutually exclusive with nonzero port; default empty means unrestricted. |
| `spec.icmpType` | Optional named IPv4 ICMP type, requires icmp. Supported: echo-reply, destination-unreachable, source-quench, redirect, echo-request, router-advertisement, router-solicitation, time-exceeded, parameter-problem, timestamp-request, timestamp-reply. Empty means all types. Camel-case key overrides the accepted YAML alias icmp-type. |
| `spec.icmpv6Type` | Optional IPv6 ICMP name, requires icmpv6/ipv6-icmp. Supported destination-unreachable, packet-too-big, time-exceeded, parameter-problem, echo-request/reply, router-solicit/advert, neighbor-solicit/advert and their nd- aliases. Empty means all types. Overrides YAML alias icmpv6-type. |
| `spec.action` | Required accept, drop or reject; no default. With rate/connection predicates on Linux, applies to excess traffic rather than defining an allowed quota. |
| `spec.log` | Default false. True adds logging before the rule action; Linux uses kernel logging or enabled FirewallEventLog NFLOG, PF uses its log action. |
| `spec.rateLimit` | Optional Linux over-rate predicate; default disabled. Current PF ignores all nested rate options. |
| `spec.rateLimit.rate` | Nonnegative rate, default 0 falls back to packetsPerSecond. Effective 0 disables the predicate and requires unit/per/burst/log to remain empty/zero/false. Positive rate takes precedence over packetsPerSecond. |
| `spec.rateLimit.unit` | packet (default when active), byte, kilobyte or megabyte. Selects nftables rate and burst units; no effect without a positive effective rate. |
| `spec.rateLimit.per` | second (default when active) or minute; requires a positive effective rate. |
| `spec.rateLimit.log` | Default false. True logs rate-exceeding matches before applying action; requires a positive effective rate. |
| `spec.rateLimit.packetsPerSecond` | Legacy nonnegative rate fallback when rate=0, default 0. Despite its name, explicit unit/per still control the effective units if supplied. |
| `spec.rateLimit.burst` | Nonnegative burst amount in the selected unit, default 0 omits the burst clause and uses nftables behavior. Requires an active rate if nonzero. |
| `spec.connLimit` | Optional Linux per-source connection-count predicate; default disabled. Independent from rateLimit; current PF ignores it. |
| `spec.connLimit.maxPerSource` | Nonnegative maximum, default 0 disabled. Positive N matches ct count over N per source address (separate IPv4/IPv6 meters), then applies action. |
| `spec.connLimit.log` | Default false. True adds a conn-limit log for excess-count matches; has no effect when maxPerSource=0. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `action`

[Example](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
