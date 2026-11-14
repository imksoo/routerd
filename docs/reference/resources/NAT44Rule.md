# NAT44Rule

[Resource configuration reference](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Translate routed IPv4 source addresses through an egress interface or selected EgressRoutePolicy device. NAT does not itself permit forwarding or assign the translated address.

## Conditions and references

- Use type + egressInterface/egressPolicyRef + sourceRanges for the runtime controller. type is required (masquerade or snat); at least one egress selector and at least one IPv4 source range are required. Explicit egressInterface wins if both egress selectors are supplied.
- snat requires exactly one of snatAddress and snatAddressFrom. masquerade prohibits both. A dynamic address source follows common resource/field validation and leaves Pending while no usable address resolves; optional does not skip the need for a SNAT address.
- The alternative outboundInterface/sourceCIDRs/translation shape is accepted by validation and shown in plans, but the runtime NAT44 controller does not translate it into its render rule. It can remain Pending or fail rendering rather than apply on Linux. Static PF rendering supports only interfaceAddress/address of this shape, not pool; portMapping has no renderer effect. Do not use this shape as a working runtime configuration.
- The two shapes are mutually exclusive. Alternative shape requires outboundInterface, nonempty IPv4 sourceCIDRs and translation.type; it prohibits all traditional type/egress/sourceRanges/destination/SNAT fields. Irrelevant translation.address/addresses are not mutually rejected, but only the selected branch would use them.
- Linux combines destinationCIDRs and destinationSetRefs as alternatives, then excludes the configured CIDRs/sets. PF applies static destination/exclusion CIDRs but ignores both address-set reference lists. Rule ordering uses metadata.name, not YAML order.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    [ destinationCIDRs:
        - <string>
    ]
    [ destinationSetRefs:
        - <string>
    ]
    [ egressInterface: <string> ]
    [ egressPolicyRef: <string> ]
    [ excludeDestinationCIDRs:
        - <string>
    ]
    [ excludeDestinationSetRefs:
        - <string>
    ]
    [ outboundInterface: <string> ]
    [ snatAddress: <string> ]
    [ snatAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ sourceCIDRs:
        - <string>
    ]
    [ sourceRanges:
        - <string>
    ]
    [ translation:
        [ address: <string> ]
        [ addresses:
            - <string>
        ]
        [ portMapping:
            [ end: <integer> ]
            [ start: <integer> ]
            [ type: ("auto" | "preserve" | "range") ]
        ]
        type: ("interfaceAddress" | "address" | "pool")
    ]
    [ type: ("masquerade" | "snat") ]
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
| `spec.type` | **Conditionally required** | string; enum=["masquerade", "snat"] | Required in runtime shape: masquerade uses the egress address, snat uses a supplied address. No default; prohibited with alternative source-NAT fields. |
| `spec.egressInterface` | **Conditionally required** | string | Optional bare configured logical interface/tunnel name; if supplied, must resolve in the interface/DSLite index. Takes precedence over egressPolicyRef. |
| `spec.egressPolicyRef` | **Conditionally required** | string | Bare EgressRoutePolicy name when egressInterface is absent. The controller requires its status.phase=Applied and selectedDevice nonempty. Reference existence is not checked by the NAT44 validator, so an unresolved/misnamed policy leaves Pending. |
| `spec.sourceRanges` | **Conditionally required** | array | Required nonempty IPv4 CIDR alternatives in runtime shape. No implicit LAN discovery; mutually exclusive with sourceCIDRs/translation shape. |
| `spec.destinationCIDRs` | *Optional* | array | Optional IPv4 destination alternatives, default empty. Linux unions with destinationSetRefs; PF uses these static CIDRs. |
| `spec.destinationSetRefs` | *Optional* | array | Optional existing IPAddressSet/<name> references; duplicates rejected. Default empty. Effective on Linux, ignored by PF. |
| `spec.excludeDestinationCIDRs` | *Optional* | array | Optional IPv4 destinations exempt from this NAT selection. Default empty. PF emits no-nat entries before this rule’s translations. |
| `spec.excludeDestinationSetRefs` | *Optional* | array | Optional existing IPAddressSet/<name> exclusions, default empty. Effective only on Linux; PF ignores them. |
| `spec.snatAddress` | **Conditionally required** | string | Bare IPv4 translation address for type=snat; exactly one of this and snatAddressFrom is required. Prohibited with masquerade. Does not assign the address. |
| `spec.snatAddressFrom` | **Conditionally required** | object | Optional common resource/field/optional source, only for snat. Reads status first, then a supported declared resource field; unusable value stays Pending. Specify an IPv4 address-providing field. |
| `spec.outboundInterface` | **Conditionally required** | string | Required bare configured interface name for the alternative validated shape; no runtime-controller support as described above. Do not combine with egressInterface/egressPolicyRef. |
| `spec.sourceCIDRs` | **Conditionally required** | array | Required nonempty IPv4 prefixes for the alternative shape; no default. Not consumed by the runtime controller. |
| `spec.translation` | **Conditionally required** | object | Alternative-shape translation object; needed with outboundInterface/sourceCIDRs. Validation/planning support is not proof of runtime installation. |
| `spec.translation.type` | **Conditionally required** | string; enum=["interfaceAddress", "address", "pool"] | Required interfaceAddress, address or pool in the alternative shape. Static PF supports interfaceAddress/address only; runtime controller does not apply this shape. |
| `spec.translation.address` | **Conditionally required** | string | Bare IPv4 required when translation.type=address; default empty. Other translation types do not consume it. |
| `spec.translation.addresses` | **Conditionally required** | array | Nonempty bare IPv4 list required when type=pool. Accepted/planned but not implemented by the current runtime controller or static PF renderer. |
| `spec.translation.portMapping` | *Optional* | object | Optional alternative-shape port policy, validated and planned only. Current Linux runtime and PF renderer do not enforce these port settings. |
| `spec.translation.portMapping.type` | *Optional* | string; enum=["auto", "preserve", "range"] | auto (default), preserve or range. auto/preserve prohibit start/end; range requires both endpoints. Currently no renderer effect. |
| `spec.translation.portMapping.start` | *Optional* | integer; minimum=1; maximum=65535 | Range start, required only for range: 1–65535 and <= end. Default 0 means absent. Currently validated only. |
| `spec.translation.portMapping.end` | *Optional* | integer; minimum=1; maximum=65535 | Range end, required only for range: 1–65535 and >= start. Default 0 means absent. Currently validated only. |
| `spec.when` | *Optional* | object | Optional common activation condition; default unconditional. Inactive/filtered rules are omitted from effective NAT generation. See common fields for predicate semantics and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `egressInterface`, `snatAddress`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
