# DHCPv4Reservation

[Resource configuration reference](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Reserve one IPv4 address for a MAC address on a declared DHCPv4Server.

## Conditions and references

- server, macAddress and ipAddress are semantically required even though the schema marks server optional. server must be the bare name of a DHCPv4Server with nonempty interface; there is no implicit single-server fallback in normal validation. scope is rejected.
- Current runtime places reservations in the shared dnsmasq hosts sidecar with a per-resource tag. Server association controls validity/status and custom-option rendering; it is not a separate dnsmasq process or independent host-file namespace. Applied does not prove that a client obtained the lease.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    [ hostname: <string> ]
    ipAddress: <string>
    [ leaseTime: <string> ]
    macAddress: <string>
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ scope: <string> ]
    server: <string>
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
| `spec.scope` | *Optional* | string | Removed field; any nonempty value is rejected. Use server. |
| `spec.server` | **Required** | string | Required bare metadata.name of an existing direct DHCPv4Server with interface set; Kind/name is not stripped here. |
| `spec.macAddress` | **Required** | string | Required MAC address parsed by net.ParseMAC; emitted lowercase. No default. |
| `spec.hostname` | *Optional* | string | Optional hostname without whitespace or comma; default omitted. Can feed the DNSZone DHCP-derived host snapshot when enabled. |
| `spec.ipAddress` | **Required** | string | Required bare IPv4 reservation address. Current validation checks family but does not require membership in the server pool or reject duplicate reservation IP/MAC pairs here. |
| `spec.leaseTime` | *Optional* | string | Optional backend duration string, default empty. Commas are rejected; current runtime reservation host-line writer does not emit this field, so it does not override the lease lifetime there. |
| `spec.options` | *Optional* | array | Optional DHCP option list emitted using this reservation tag. Each item needs exactly one of code/name and a nonempty value. Default empty. |
| `spec.options[].code` | **Conditionally required** | integer; minimum=1; maximum=65535 | Numeric option identifier, exclusive with name; schema range 1–65535. No default identifier. |
| `spec.options[].name` | **Conditionally required** | string | dnsmasq option name, exclusive with code; no whitespace or commas. |
| `spec.options[].value` | *Optional* | string | Required nonempty backend-formatted value; no CR/LF. Passed through, so commas may separate multiple DHCP option values. |
| `spec.when` | *Optional* | object | Optional common state condition; default unconditional. False excludes the reservation and reports Pending/WhenFalse; an unavailable server reports ServerPending. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `hostname`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
