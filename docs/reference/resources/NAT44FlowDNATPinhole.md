# NAT44FlowDNATPinhole

[Resource configuration reference](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Learn an IPv4 UDP internal endpoint from outbound traffic and dynamically DNAT inbound UDP by its destination port. Linux nftables only; the current PF renderer does not implement this resource.

## Conditions and references

- fromInterfaceRefs, outbound, inbound and a source selector are required. Every interface and IPAddressSet reference must exist in the same Router. IPv4/UDP only; this is port-triggered DNAT, not ordinary conntrack reply handling.
- The map key is the outbound local UDP source port; value is internal source IPv4 plus the same port, learned before SNAT. All selected interfaces share one map per resource. Different clients using the same source port overwrite the same mapping; remote address is not part of the key.
- Inbound DNAT checks selected incoming interfaces, allowed remote source ports and a learned destination port. It does not reapply outbound destination CIDRs/sets to the inbound remote address and does not create a firewall forward-accept rule; the firewall policy must also permit the translated traffic.
- Learned maps are kernel runtime state, not configuration or persistent DB records; recreating the NAT table or rebooting loses them. Outbound traffic must relearn mappings.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # optional; object
      key: "" # optional; string; enum=["", "localPort"]
    fromInterfaceRefs: # optional; array
      - "<string>" # string
    inbound: # Required; object
      sourcePorts: # Required; array
        - "<string>" # string
    outbound: # Required; object
      destinationCIDRs: # optional; array
        - "<string>" # string
      destinationPorts: # Required; array
        - "<string>" # string
      destinationSetRefs: # optional; array
        - "<string>" # string
      protocol: "udp" # Required; string; enum=["udp"]
      sourceCIDRs: # optional; array
        - "<string>" # string
      sourceSetRef: "<string>" # optional; string
    timeout: "<string>" # optional; string
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

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.outbound` | Required outbound-learning match. It records eligible UDP flow identities but does not itself allow outbound traffic through the firewall. |
| `spec.outbound.sourceSetRef` | Optional existing IPAddressSet/<name> source selector. At least this or sourceCIDRs is required; both may be combined as OR alternatives. Default empty. |
| `spec.outbound.sourceCIDRs` | Optional IPv4 source-prefix OR list, default empty. At least one source selector is required. IPv6 is rejected; host bits are masked for rendering. |
| `spec.outbound.destinationCIDRs` | Optional IPv4 remote destinations for learning, default empty. Combined with destinationSetRefs as OR; no destination selector means any remote address. |
| `spec.outbound.destinationSetRefs` | Optional existing IPAddressSet/<name> remote destination references; duplicates rejected. Default empty. Source and destination categories combine with AND. |
| `spec.outbound.protocol` | Required udp; no default and no TCP/IPv6 support for this learning mechanism. |
| `spec.outbound.destinationPorts` | Required nonempty outbound UDP destination ports: numeric 1–65535 or a sole start-end range. A range cannot be combined with other list entries. |
| `spec.inbound` | Required return-traffic match; sourcePorts selects allowed remote UDP sender ports, not local destination ports. |
| `spec.inbound.sourcePorts` | Required nonempty remote UDP source-port list with the same 1–65535 / sole-range constraints as outbound.destinationPorts. |
| `spec.timeout` | Optional strictly positive Go duration; default 3m. Each matching outbound packet refreshes the learned element. Rendering rounds to seconds with a minimum of 1s; this is learned-state expiry, not a UDP protocol timeout. |
| `spec.when` | Optional common activation condition, default unconditional. A filtered resource contributes no learned map/set rules. See common fields for nested conditions and schema limitations. |
| `spec.fromInterfaceRefs` | Required nonempty egress/ingress interface list; bare names or Interface/name, PPPoESession/name, DSLiteTunnel/name. Outbound matches oifname and inbound matches iifname on this same list. |
| `spec.correlation` | Optional correlation object; only localPort is supported. Default omitted uses the same key. |
| `spec.correlation.key` | Empty or localPort only. The configured key is not otherwise branched by the renderer: UDP source port always indexes the learned map. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go)
