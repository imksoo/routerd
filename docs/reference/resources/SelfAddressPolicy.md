# SelfAddressPolicy

[Resource configuration reference](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Describe an ordered self-address selection policy used by supporting static DHCPv6 rendering. It does not assign an address to an interface.

## Conditions and references

- Current live DHCPv6 LAN generation does not consume DHCPv6Server.selfAddressPolicy or dnsSource: self. This policy can affect the separate static renderer, but declaring it does not currently make the running server advertise self DNS. Use explicit dnsServers/dnsServerFrom for the live path.
- References are bare declared Interface/IPv6DelegatedAddress names and are validated even when their source branch does not use them. No when or dependsOn fields exist on this resource.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    candidates: # Required; array
      - # object
        address: "<string>" # optional; string
        addressSuffix: "<string>" # optional; string
        delegatedAddress: "<string>" # optional; string
        interface: "<string>" # optional; string
        matchSuffix: "<string>" # optional; string
        ordinal: 1 # optional; integer; minimum=1
        source: "delegatedAddress" # Required; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.addressFamily` | Required ipv4 or ipv6. Validation accepts both, but the current selection consumer is the older DHCPv6 static renderer; ipv4 does not provide an independent live address-selection service. |
| `spec.candidates` | Required nonempty ordered list. The supporting static renderer tries candidates in order and picks the first successful result; failure of all candidates is an error. No automatic candidates for an explicitly referenced policy. |
| `spec.candidates[].source` | Required delegatedAddress/interfaceAddress/static. delegatedAddress is IPv6-only and prohibits address/interface. interfaceAddress requires interface; static requires an address matching addressFamily. Unused extra fields on the latter two sources are not generally rejected. |
| `spec.candidates[].interface` | Required declared logical interface for interfaceAddress, forbidden for delegatedAddress. Any supplied value is checked for existence even when the chosen source does not consume it. |
| `spec.candidates[].delegatedAddress` | Required bare IPv6DelegatedAddress name for delegatedAddress source. Existing resource required; supporting renderer derives from observed prefixes for that resource interface, not a generic status-value field. |
| `spec.candidates[].address` | Required bare address matching addressFamily for static source. Forbidden for delegatedAddress; no automatic OS assignment is performed. |
| `spec.candidates[].addressSuffix` | For delegatedAddress, optional IPv6 suffix; omitted inherits the referenced resource addressSuffix. Other sources do not use it for selection. |
| `spec.candidates[].matchSuffix` | For interfaceAddress, optional IPv6 pattern. Each nonzero byte must match the address; zero bytes act as unconstrained positions, not a conventional contiguous suffix mask. If set, first matching global IPv6 wins and ordinal is ignored. |
| `spec.candidates[].ordinal` | One-based interfaceAddress index when matchSuffix is empty; omitted/0 becomes 1. Schema minimum 1; negative validation rejected. Ordering follows the observed-address list, not a promised stable address identity. Out-of-range tries the next candidate. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)
