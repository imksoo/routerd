# FirewallFlowPinhole

[Resource configuration reference](../resources.md#firewallflowpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Learn allowed return IPv4 UDP endpoints for a pair of firewall zones, without address translation. Linux nftables only; the current PF renderer omits this resource.

## Conditions and references

- fromZone and toZone must be different existing FirewallZone bare names; self is forbidden. Outbound/inbound and at least one source selector are required. All IPAddressSet references must exist.
- Outbound matching traffic updates a timed set but continues to later rules; it is not automatically accepted. Reverse traffic matches swapped source/destination restrictions, allowed remote UDP source ports and a learned tuple, then is accepted before ordinary explicit zone-pair rules.
- Default remoteAddress binds a learned tuple to remote IPv4 + local IPv4 + local UDP port. localEndpoint omits the remote address from the key, but the configured remote CIDR/set restriction still applies to inbound sources. Remote UDP destination ports are not part of the learned key.
- Learned elements are transient kernel sets; firewall table recreation or reboot loses them. Existing conntrack accepts and control exceptions can precede the pinhole rules, so this is not a replacement for the complete firewall policy.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    [ correlation:
        [ key: ("" | "remoteAddress" | "localEndpoint") ]
    ]
    fromZone: <string>
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
    toZone: <string>
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
| `spec.fromZone` | **Required** | string | Required existing FirewallZone metadata.name for outbound origin. Must differ from toZone; self is rejected. |
| `spec.toZone` | **Required** | string | Required existing FirewallZone metadata.name for outbound destination; becomes inbound origin for return checks. No self. |
| `spec.correlation` | *Optional* | object | Optional tuple-key selection; default remoteAddress. Does not configure a resource reference. |
| `spec.correlation.key` | *Optional* | string; enum=["", "remoteAddress", "localEndpoint"] | remoteAddress (default) or localEndpoint. The former includes remote IPv4 in each key; the latter uses only internal IPv4 + UDP port. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`
