# DHCPv6PrefixDelegation

[Resource configuration reference](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Acquire an IPv6 delegated prefix on an uplink. Use IPv6DelegatedAddress to derive and apply downstream or host addresses; obtaining a prefix alone does not advertise it to clients.

## Conditions and references

- interface must name an existing logical interface. The default client is routerd-dhcpv6-client. networkd, dhcp6c and dhcpcd are recognized alternatives with platform-specific generation, but the live supervisor/status socket path is built around the routerd daemon; do not assume equivalent lifecycle support.
- profile and prefixLength influence generated external-client configuration and restored-snapshot matching. The supervised routerd-dhcpv6-client command does not receive a prefix-length or profile flag: its acquisition does not use these fields as a length hint. clientDUID is passed to it.
- IAID/duidType YAML controls are not supported; the routerd daemon defaults IA_PD IAID to 1 and derives DUID-LL from the uplink MAC unless clientDUID is specified. required does not implement a boot-blocking requirement.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    [ client: <string> ]
    [ clientDUID: <string> ]
    interface: <string>
    [ prefixLength: <integer> ]
    [ profile: ("default" | "ntt-ngn-direct-hikari-denwa" | "ntt-hgw-lan-pd") ]
    [ required: <boolean> ]
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
| `spec.interface` | **Required** | string | Required logical uplink interface in this Router. If DHCPv6Address also uses the same interface, an external dhcp6c/dhcpcd PD client must match that resource client. |
| `spec.client` | *Optional* | string | Default routerd-dhcpv6-client; accepted alternatives networkd/dhcp6c/dhcpcd. Current supervision can also synthesize a routerd DHCPv6 systemd unit, so selecting an external client is not a general guarantee that the built-in daemon is disabled. |
| `spec.profile` | *Optional* | string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"] | Empty/default, ntt-ngn-direct-hikari-denwa or ntt-hgw-lan-pd. NTT profiles supply /60 and link-layer DUID defaults to supporting generation paths; they do not configure provider credentials or change the built-in daemon command. |
| `spec.prefixLength` | *Optional* | integer; minimum=1; maximum=128 | Optional 1–128. Omitted/0 means /60 for NTT profiles and no configured hint for default; it is not a guarantee of assigned size and is not passed to built-in acquisition. HA staged-snapshot reuse requires exact equality with this effective length: default profile with omitted length evaluates to 0 and does not match normal delegated prefixes. Specify the actual length for that path. |
| `spec.clientDUID` | *Optional* | string | Optional DUID as unseparated even-length hexadecimal. Colons/whitespace are rejected. Empty lets the built-in client derive DUID-LL from MAC; identity affects whether a restored lease can be reused. |
| `spec.required` | *Optional* | boolean | Boolean default false, decoded but not consumed as a required/startup wait policy by current runtime. Dependents should use actual Bound status and dependency predicates. |
| `spec.when` | *Optional* | object | Optional common state condition, default unconditional. Supervised client generation filters resolved conditions; false can stop acquisition while the declared resource remains. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `currentPrefix`, `dnsServers`, `sntpServers`, `domainSearch`, `aftrName`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
