# DHCPv4Client

[Resource configuration reference](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Runs the managed routerd-dhcpv4-client daemon and applies its current lease address, optional default route and optional host DNS settings.

## Conditions and references

- The interface must exist in the declared Router. DHCP identity fields are sent as protocol options, not resource references. Lease expiry/release removes the previously applied address, route and host DNS state; useRoutes/useDNS do not stop lease acquisition.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    [ classID: <string> ]
    [ clientID: <string> ]
    [ hostname: <string> ]
    interface: <string>
    [ requestedAddress: <string> ]
    [ routeMetric: <integer> ]
    [ useDNS: <boolean> ]
    [ useRoutes: <boolean> ]
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
| `spec.interface` | **Required** | string | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.hostname` | *Optional* | string | DHCP option 12 hostname. Default empty: omit the option; it is not automatically filled from the OS hostname. Whitespace is rejected. |
| `spec.requestedAddress` | *Optional* | string | Optional preferred IPv4 address, default absent. Must be a bare IPv4 address without prefix. Sent as option 50; it is a request, not a guaranteed/static assignment. |
| `spec.classID` | *Optional* | string | Vendor class identifier sent as option 60 string bytes. Default empty: omit it. No hexadecimal decoding. |
| `spec.clientID` | *Optional* | string | Option 61 client identity. An explicit value is sent as string bytes, not decoded hex. Default is byte 1 followed by the interface hardware address. Keep the value stable when the DHCP server identifies leases by client ID. |
| `spec.useRoutes` | *Optional* | boolean | Default true: apply the lease defaultGateway as a default route when present. False removes the prior managed default route but retains lease acquisition and address application. It does not suppress the gateway value in status. |
| `spec.useDNS` | *Optional* | boolean | Default true: apply leased DNS servers to the host via resolvectl when /etc/resolv.conf points to systemd-resolved, otherwise write resolv.conf. False clears prior managed DNS state; DNS addresses remain available in status for DNSResolver-related references. |
| `spec.routeMetric` | *Optional* | integer; minimum=0 | Default 0. Used for the applied default-route metric and platform-specific lease-address reconciliation. Schema requires a nonnegative integer. It is not a routing-table ID. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
