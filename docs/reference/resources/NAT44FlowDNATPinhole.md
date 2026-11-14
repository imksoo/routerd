# NAT44FlowDNATPinhole

[Resource configuration reference](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Learn an IPv4 UDP internal endpoint from outbound traffic and dynamically DNAT inbound UDP by its destination port. Linux nftables only; the current PF renderer does not implement this resource.

## Conditions and references

- fromInterfaceRefs, outbound, inbound and a source selector are required. Every interface and IPAddressSet reference must exist in the same Router. IPv4/UDP only; this is port-triggered DNAT, not ordinary conntrack reply handling.
- The map key is the outbound local UDP source port; value is internal source IPv4 plus the same port, learned before SNAT. All selected interfaces share one map per resource. Different clients using the same source port overwrite the same mapping; remote address is not part of the key.
- Inbound DNAT checks selected incoming interfaces, allowed remote source ports and a learned destination port. It does not reapply outbound destination CIDRs/sets to the inbound remote address and does not create a firewall forward-accept rule; the firewall policy must also permit the translated traffic.
- Learned maps are kernel runtime state, not configuration or persistent DB records; recreating the NAT table or rebooting loses them. Outbound traffic must relearn mappings.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    [ correlation:
        [ key: ("" | "localPort") ]
    ]
    fromInterfaceRefs:
      - <string>
    inbound:
      sourcePorts:
        - <string>
    outbound:
      [ destinationCIDRs:
          - <string>
      ]
      destinationPorts:
        - <string>
      [ destinationSetRefs:
          - <string>
      ]
      protocol: ("udp")
      [ sourceCIDRs:
          - <string>
      ]
      [ sourceSetRef: <string> ]
    [ timeout: <string> ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.outbound` | **Required** | object | Required outbound-learning match. It records eligible UDP flow identities but does not itself allow outbound traffic through the firewall. |
| `spec.outbound.sourceSetRef` | **Conditionally required** | string | Optional existing IPAddressSet/<name> source selector. At least this or sourceCIDRs is required; both may be combined as OR alternatives. Default empty. |
| `spec.outbound.sourceCIDRs` | **Conditionally required** | array | Optional IPv4 source-prefix OR list, default empty. At least one source selector is required. IPv6 is rejected; host bits are masked for rendering. |
| `spec.outbound.destinationCIDRs` | *Optional* | array | Optional IPv4 remote destinations for learning, default empty. Combined with destinationSetRefs as OR; no destination selector means any remote address. |
| `spec.outbound.destinationSetRefs` | *Optional* | array | Optional existing IPAddressSet/<name> remote destination references; duplicates rejected. Default empty. Source and destination categories combine with AND. |
| `spec.outbound.protocol` | **Required** | string; enum=["udp"] | Required udp; no default and no TCP/IPv6 support for this learning mechanism. |
| `spec.outbound.destinationPorts` | **Required** | array | Required nonempty outbound UDP destination ports: numeric 1–65535 or a sole start-end range. A range cannot be combined with other list entries. |
| `spec.inbound` | **Required** | object | Required return-traffic match; sourcePorts selects allowed remote UDP sender ports, not local destination ports. |
| `spec.inbound.sourcePorts` | **Required** | array | Required nonempty remote UDP source-port list with the same 1–65535 / sole-range constraints as outbound.destinationPorts. |
| `spec.timeout` | *Optional* | string | Optional strictly positive Go duration; default 3m. Each matching outbound packet refreshes the learned element. Rendering rounds to seconds with a minimum of 1s; this is learned-state expiry, not a UDP protocol timeout. |
| `spec.when` | *Optional* | object | Optional common activation condition, default unconditional. A filtered resource contributes no learned map/set rules. See common fields for nested conditions and schema limitations. |
| `spec.fromInterfaceRefs` | **Required** | array | Required nonempty egress/ingress interface list; bare names or Interface/name, PPPoESession/name, DSLiteTunnel/name. Outbound matches oifname and inbound matches iifname on this same list. |
| `spec.correlation` | *Optional* | object | Optional correlation object; only localPort is supported. Default omitted uses the same key. |
| `spec.correlation.key` | *Optional* | string; enum=["", "localPort"] | Empty or localPort only. The configured key is not otherwise branched by the renderer: UDP source port always indexes the learned map. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`
