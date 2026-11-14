# NTPClient

[Resource configuration reference](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Manages or observes an upstream time-synchronization client.

## Conditions and references

- With managed=true, at least one of servers/serverFrom/fallbackServers must be declared. Static mode additionally requires servers itself. With no resolved servers the client remains Pending/NoServers. chrony management disables the stock systemd-timesyncd service.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    [ fallbackServers:
        - <string>
    ]
    [ interface: <string> ]
    [ managed: <boolean> ]
    [ provider: ("systemd-timesyncd" | "chrony" | "ntpd") ]
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
| `spec.provider` | *Optional* | string; enum=["systemd-timesyncd", "chrony", "ntpd"] | Client implementation. Default systemd-timesyncd except on FreeBSD, where it is ntpd. chrony is also accepted. ntpd requires FreeBSD rc.d support; systemd-timesyncd requires the platform feature. |
| `spec.managed` | *Optional* | boolean | Default false: record Observed without configuring or starting the time service. Set true to manage files and services. |
| `spec.source` | *Optional* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | Default static: use servers. auto/dhcp/dhcpv6 all use the same order: resolved serverFrom, then fallbackServers, then servers. They do not automatically discover DHCP sources. |
| `spec.interface` | *Optional* | string | Optional logical interface name, default empty. Router-wide validation requires it to exist when supplied. The NTP controller does not use it for source selection or service binding. |
| `spec.servers` | **Conditionally required** | array | Upstream hostnames or IP addresses, default empty. Required when managed=true and source is omitted/static. Entries must be nonblank single tokens; they are not resource references. |
| `spec.serverFrom` | *Optional* | array | List of explicit value sources, default empty. Use for example DHCPv4Client/wan field ntpServers or DHCPv6PrefixDelegation/wan-pd field sntpServers. Used only for auto/dhcp/dhcpv6; each resource and field must exist. This resolver ignores the optional flag; missing values simply lead to the next source alternative. |
| `spec.fallbackServers` | *Optional* | array | Fallback upstreams, default empty. For dynamic source modes they are selected when serverFrom yields no servers. systemd-timesyncd additionally receives FallbackNTP; chrony and ntpd do not receive a separate fallback directive. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `servers`, `source`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
