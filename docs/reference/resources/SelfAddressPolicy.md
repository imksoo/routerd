# SelfAddressPolicy

[Resource configuration reference](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Describe an ordered self-address selection policy used by supporting static DHCPv6 rendering. It does not assign an address to an interface.

## Conditions and references

- Current live DHCPv6 LAN generation does not consume DHCPv6Server.selfAddressPolicy or dnsSource: self. This policy can affect the separate static renderer, but declaring it does not currently make the running server advertise self DNS. Use explicit dnsServers/dnsServerFrom for the live path.
- References are bare declared Interface/IPv6DelegatedAddress names and are validated even when their source branch does not use them. No when or dependsOn fields exist on this resource.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: ("ipv4" | "ipv6")
    candidates:
      -
        [ address: <string> ]
        [ addressSuffix: <string> ]
        [ delegatedAddress: <string> ]
        [ interface: <string> ]
        [ matchSuffix: <string> ]
        [ ordinal: <integer> ]
        source: ("delegatedAddress" | "interfaceAddress" | "static")
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.addressFamily` | **Required** | string; enum=["ipv4", "ipv6"] | Required ipv4 or ipv6. Validation accepts both, but the current selection consumer is the older DHCPv6 static renderer; ipv4 does not provide an independent live address-selection service. |
| `spec.candidates` | **Required** | array | Required nonempty ordered list. The supporting static renderer tries candidates in order and picks the first successful result; failure of all candidates is an error. No automatic candidates for an explicitly referenced policy. |
| `spec.candidates[].source` | **Required** | string; enum=["delegatedAddress", "interfaceAddress", "static"] | Required delegatedAddress/interfaceAddress/static. delegatedAddress is IPv6-only and prohibits address/interface. interfaceAddress requires interface; static requires an address matching addressFamily. Unused extra fields on the latter two sources are not generally rejected. |
| `spec.candidates[].interface` | *Optional* | string | Required declared logical interface for interfaceAddress, forbidden for delegatedAddress. Any supplied value is checked for existence even when the chosen source does not consume it. |
| `spec.candidates[].delegatedAddress` | *Optional* | string | Required bare IPv6DelegatedAddress name for delegatedAddress source. Existing resource required; supporting renderer derives from observed prefixes for that resource interface, not a generic status-value field. |
| `spec.candidates[].address` | *Optional* | string | Required bare address matching addressFamily for static source. Forbidden for delegatedAddress; no automatic OS assignment is performed. |
| `spec.candidates[].addressSuffix` | *Optional* | string | For delegatedAddress, optional IPv6 suffix; omitted inherits the referenced resource addressSuffix. Other sources do not use it for selection. |
| `spec.candidates[].matchSuffix` | *Optional* | string | For interfaceAddress, optional IPv6 pattern. Each nonzero byte must match the address; zero bytes act as unconstrained positions, not a conventional contiguous suffix mask. If set, first matching global IPv6 wins and ordinal is ignored. |
| `spec.candidates[].ordinal` | *Optional* | integer; minimum=1 | One-based interfaceAddress index when matchSuffix is empty; omitted/0 becomes 1. Schema minimum 1; negative validation rejected. Ordering follows the observed-address list, not a promised stable address identity. Out-of-range tries the next candidate. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `source`
