# IPv6DelegatedAddress

[Resource configuration reference](../resources.md#ipv6delegatedaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Derive and install a local IPv6 address from a DHCPv6PrefixDelegation prefix.

## Conditions and references

- prefixDelegation must be the bare name of an existing DHCPv6PrefixDelegation and interface must exist in the Router interface index. Normal active installation needs a usable Bound prefix, a ready link and satisfied dependencies. HA VMAC staging can use a fresh synchronized snapshot before active acquisition.
- The derived prefix length is max(64, delegated length). subnetID is hexadecimal, shifted to that boundary and ORed into the base; the suffix is masked to remaining host bits and ORed in. Current derivation does not check that subnetID fits the delegated block, so choose an in-range value.
- Linux uses ip address operations and FreeBSD ifconfig. Active application reads the address back before publishing Applied; tentative/DAD state can stay Pending. Prefix changes can retain previous addresses as deprecated until their valid lifetime ends. /128 is handled as a host address rather than an on-link subnet.
- sendRA/announce do not cause the current address controller to run an RA service. Configure IPv6RouterAdvertisement or DHCPv6Server separately. Removed prefixSource and ready_when are rejected.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: <string>
    [ announce: <boolean> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    interface: <string>
    prefixDelegation: <string>
    [ prefixLength: <integer> ]
    [ sendRA: <boolean> ]
    [ subnetID: <string> ]
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
| `spec.prefixDelegation` | **Required** | string | Required bare DHCPv6PrefixDelegation metadata.name, not Kind/name or a status expression. No default. |
| `spec.interface` | **Required** | string | Required declared logical target interface. The final OS name is resolved from the Router. |
| `spec.subnetID` | *Optional* | string | Optional hexadecimal string, default 0; "10" means hex 0x10, not decimal ten. Optional 0x prefix. Use an identifier that fits the delegated block; strict fit checking is absent. |
| `spec.addressSuffix` | **Required** | string | Required IPv6 suffix such as ::1. Only host bits below the derived prefix boundary are used, not the entire literal as an absolute address. No automatic ::1 default. |
| `spec.prefixLength` | *Optional* | integer; minimum=1; maximum=128 | Omitted/0 retains the derived length, normally /64. Runtime accepts that same length or 128; other explicit lengths can pass schema (1–128) but fail apply. /128 avoids creating the usual on-link prefix route. |
| `spec.sendRA` | *Optional* | boolean | Boolean default false. Used by host dependency/forwarding setup in some paths, but not an automatic RA-service switch in the live address controller. |
| `spec.announce` | *Optional* | boolean | Boolean default false; same live RA-service limitation as sendRA. Do not treat it as an unsolicited neighbor/RA transmission guarantee. |
| `spec.when` | *Optional* | object | Optional common condition, default unconditional. False can remove the derived address through the explicit cleanup path. |
| `spec.dependsOn` | *Optional* | array | Optional common AND-list, default empty. Unsatisfied dependencies keep the address Pending/DependsOnFalse. HA staging has specialized handling for its PD/VMAC prerequisites; use actual resource phases. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/pdclient/derive.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/derive.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `interface`, `prefixSource`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
