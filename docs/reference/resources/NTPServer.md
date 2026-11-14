# NTPServer

[Resource configuration reference](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Configures a time server and its upstream servers using chrony or FreeBSD ntpd.

## Conditions and references

- managed defaults to false. With true, server-source validation requires a declared upstream source; static additionally requires servers. listen/allow source arrays are merged, while upstream source alternatives follow source selection order.
- The ntpd renderer does not emit allowCIDRs restrictions. Do not assume that declaring this list enforces a client ACL on ntpd.
- In a dynamic source mode, unresolved upstream values do not themselves stop the server renderer; it may emit a configuration with no upstreams. Applied denotes configuration/service work, not proof of clock synchronization.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    [ allowCIDRFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ allowCIDRs:
        - <string>
    ]
    [ fallbackServers:
        - <string>
    ]
    [ listenAddressFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ listenAddresses:
        - <string>
    ]
    [ managed: <boolean> ]
    [ provider: ("chrony" | "ntpd") ]
    [ serverFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ servers:
        - <string>
    ]
    [ source: ("static" | "auto" | "dhcp" | "dhcpv6") ]
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
| `spec.managed` | *Optional* | boolean | Default false: record Observed without configuring or starting the time service. Set true to manage files and services. |
| `spec.source` | *Optional* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | Default static: use servers. auto/dhcp/dhcpv6 all use the same order: resolved serverFrom, then fallbackServers, then servers. They do not automatically discover DHCP sources. |
| `spec.servers` | **Conditionally required** | array | Upstream hostnames or IP addresses, default empty. Required when managed=true and source is omitted/static. Entries must be nonblank single tokens; they are not resource references. |
| `spec.serverFrom` | *Optional* | array | List of explicit value sources, default empty. Use for example DHCPv4Client/wan field ntpServers or DHCPv6PrefixDelegation/wan-pd field sntpServers. Used only for auto/dhcp/dhcpv6; each resource and field must exist. This resolver ignores the optional flag; missing values simply lead to the next source alternative. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |
| `spec.provider` | *Optional* | string; enum=["chrony", "ntpd"] | Server implementation, default chrony except on FreeBSD, where it is ntpd. ntpd is supported only on FreeBSD. |
| `spec.fallbackServers` | *Optional* | array | Fallback upstream list, default empty. Selected only in auto/dhcp/dhcpv6 mode when serverFrom yields nothing, before servers. No separate fallback directives are written. |
| `spec.listenAddresses` | *Optional* | array | Static listen IPs, default empty. CIDR values are reduced to their host address. Empty resolved listen settings produce no bindaddress/interface-listen directives, leaving backend defaults. |
| `spec.listenAddressFrom` | *Optional* | array | Additional listen-address sources, default empty, combined with listenAddresses. Both resource and field are required per source. If any listen setting was declared but the combined result is empty, status is Pending/NoListenAddresses. |
| `spec.allowCIDRs` | *Optional* | array | Allowed client CIDRs, default empty. chrony emits allow for each; ntpd does not consume this list in its generated configuration. Static entries are checked for nonblank text, not CIDR syntax, during validation. |
| `spec.allowCIDRFrom` | *Optional* | array | CIDR value sources combined with allowCIDRs, default empty. Missing values from a nonoptional source cause Pending/AllowCIDRFromPending; invalid derived CIDRs cause Pending/AllowCIDRFromInvalid. Bare addresses are not CIDRs. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `servers`, `listenAddresses`, `allowCIDRs`, `source`, `updatedAt`
