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

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    egressInterface: "<string>" # optional; string
    egressPolicyRef: "<string>" # optional; string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    outboundInterface: "<string>" # optional; string
    snatAddress: "<string>" # optional; string
    snatAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    sourceCIDRs: # optional; array
      - "<string>" # string
    sourceRanges: # optional; array
      - "<string>" # string
    translation: # optional; object
      address: "<string>" # optional; string
      addresses: # optional; array
        - "<string>" # string
      portMapping: # optional; object
        end: 1 # optional; integer; minimum=1; maximum=65535
        start: 1 # optional; integer; minimum=1; maximum=65535
        type: "auto" # optional; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # Required; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # optional; string; enum=["masquerade", "snat"]
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
| `spec.type` | Required in runtime shape: masquerade uses the egress address, snat uses a supplied address. No default; prohibited with alternative source-NAT fields. |
| `spec.egressInterface` | Optional bare configured logical interface/tunnel name; if supplied, must resolve in the interface/DSLite index. Takes precedence over egressPolicyRef. |
| `spec.egressPolicyRef` | Bare EgressRoutePolicy name when egressInterface is absent. The controller requires its status.phase=Applied and selectedDevice nonempty. Reference existence is not checked by the NAT44 validator, so an unresolved/misnamed policy leaves Pending. |
| `spec.sourceRanges` | Required nonempty IPv4 CIDR alternatives in runtime shape. No implicit LAN discovery; mutually exclusive with sourceCIDRs/translation shape. |
| `spec.destinationCIDRs` | Optional IPv4 destination alternatives, default empty. Linux unions with destinationSetRefs; PF uses these static CIDRs. |
| `spec.destinationSetRefs` | Optional existing IPAddressSet/<name> references; duplicates rejected. Default empty. Effective on Linux, ignored by PF. |
| `spec.excludeDestinationCIDRs` | Optional IPv4 destinations exempt from this NAT selection. Default empty. PF emits no-nat entries before this rule’s translations. |
| `spec.excludeDestinationSetRefs` | Optional existing IPAddressSet/<name> exclusions, default empty. Effective only on Linux; PF ignores them. |
| `spec.snatAddress` | Bare IPv4 translation address for type=snat; exactly one of this and snatAddressFrom is required. Prohibited with masquerade. Does not assign the address. |
| `spec.snatAddressFrom` | Optional common resource/field/optional source, only for snat. Reads status first, then a supported declared resource field; unusable value stays Pending. Specify an IPv4 address-providing field. |
| `spec.outboundInterface` | Required bare configured interface name for the alternative validated shape; no runtime-controller support as described above. Do not combine with egressInterface/egressPolicyRef. |
| `spec.sourceCIDRs` | Required nonempty IPv4 prefixes for the alternative shape; no default. Not consumed by the runtime controller. |
| `spec.translation` | Alternative-shape translation object; needed with outboundInterface/sourceCIDRs. Validation/planning support is not proof of runtime installation. |
| `spec.translation.type` | Required interfaceAddress, address or pool in the alternative shape. Static PF supports interfaceAddress/address only; runtime controller does not apply this shape. |
| `spec.translation.address` | Bare IPv4 required when translation.type=address; default empty. Other translation types do not consume it. |
| `spec.translation.addresses` | Nonempty bare IPv4 list required when type=pool. Accepted/planned but not implemented by the current runtime controller or static PF renderer. |
| `spec.translation.portMapping` | Optional alternative-shape port policy, validated and planned only. Current Linux runtime and PF renderer do not enforce these port settings. |
| `spec.translation.portMapping.type` | auto (default), preserve or range. auto/preserve prohibit start/end; range requires both endpoints. Currently no renderer effect. |
| `spec.translation.portMapping.start` | Range start, required only for range: 1–65535 and <= end. Default 0 means absent. Currently validated only. |
| `spec.translation.portMapping.end` | Range end, required only for range: 1–65535 and >= start. Default 0 means absent. Currently validated only. |
| `spec.when` | Optional common activation condition; default unconditional. Inactive/filtered rules are omitted from effective NAT generation. See common fields for predicate semantics and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `egressInterface`, `snatAddress`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
