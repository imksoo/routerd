# IPv6DelegatedAddress

[Resource configuration reference](../resources.md#ipv6delegatedaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Derive and install a local IPv6 address from a DHCPv6PrefixDelegation prefix.

## Conditions and references

- prefixDelegation must be the bare name of an existing DHCPv6PrefixDelegation and interface must exist in the Router interface index. Normal active installation needs a usable Bound prefix, a ready link and satisfied dependencies. HA VMAC staging can use a fresh synchronized snapshot before active acquisition.
- The derived prefix length is max(64, delegated length). subnetID is hexadecimal, shifted to that boundary and ORed into the base; the suffix is masked to remaining host bits and ORed in. Current derivation does not check that subnetID fits the delegated block, so choose an in-range value.
- Linux uses ip address operations and FreeBSD ifconfig. Active application reads the address back before publishing Applied; tentative/DAD state can stay Pending. Prefix changes can retain previous addresses as deprecated until their valid lifetime ends. /128 is handled as a host address rather than an on-link subnet.
- sendRA/announce do not cause the current address controller to run an RA service. Configure IPv6RouterAdvertisement or DHCPv6Server separately. Removed prefixSource and ready_when are rejected.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # Required; string
    announce: true # optional; boolean
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    prefixDelegation: "<string>" # Required; string
    prefixLength: 1 # optional; integer; minimum=1; maximum=128
    sendRA: true # optional; boolean
    subnetID: "<string>" # optional; string
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
| `spec.prefixDelegation` | Required bare DHCPv6PrefixDelegation metadata.name, not Kind/name or a status expression. No default. |
| `spec.interface` | Required declared logical target interface. The final OS name is resolved from the Router. |
| `spec.subnetID` | Optional hexadecimal string, default 0; "10" means hex 0x10, not decimal ten. Optional 0x prefix. Use an identifier that fits the delegated block; strict fit checking is absent. |
| `spec.addressSuffix` | Required IPv6 suffix such as ::1. Only host bits below the derived prefix boundary are used, not the entire literal as an absolute address. No automatic ::1 default. |
| `spec.prefixLength` | Omitted/0 retains the derived length, normally /64. Runtime accepts that same length or 128; other explicit lengths can pass schema (1–128) but fail apply. /128 avoids creating the usual on-link prefix route. |
| `spec.sendRA` | Boolean default false. Used by host dependency/forwarding setup in some paths, but not an automatic RA-service switch in the live address controller. |
| `spec.announce` | Boolean default false; same live RA-service limitation as sendRA. Do not treat it as an unsolicited neighbor/RA transmission guarantee. |
| `spec.when` | Optional common condition, default unconditional. False can remove the derived address through the explicit cleanup path. |
| `spec.dependsOn` | Optional common AND-list, default empty. Unsatisfied dependencies keep the address Pending/DependsOnFalse. HA staging has specialized handling for its PD/VMAC prerequisites; use actual resource phases. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/pdclient/derive.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/derive.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `interface`, `prefixSource`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
