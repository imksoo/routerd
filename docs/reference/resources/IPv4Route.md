# IPv4Route

[Resource configuration reference](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Install an IPv4 unicast or blackhole route, optionally resolving device and gateway from resource status.

## Conditions and references

- blackhole excludes device, deviceFrom, gateway, gatewayFrom and preferredSource. unicast needs a resolved device at runtime; a gateway is optional for a directly connected route.
- Dynamic values take precedence over literal device/gateway; unresolved values fall back to the literals. The source optional flag does not bypass DeviceMissing or GatewayMissing when no usable value remains. References must still identify an existing resource and an explicitly provided field.
- Linux uses ip route replace, with src for preferredSource and metric only when positive. FreeBSD uses route change/add and -ifa for preferredSource; this path does not apply metric. Installed status is not an end-to-end reachability check.
- ready_when and ${...} expressions in device/gateway were removed: use dependsOn and explicit deviceFrom/gatewayFrom. preferredSource does not support status expressions.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    destination: <string>
    [ device: <string> ]
    [ deviceFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ gateway: <string> ]
    [ gatewayFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ metric: <integer> ]
    [ preferredSource: <string> ]
    [ type: ("unicast" | "blackhole") ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.destination` | **Required** | string | Required IPv4 CIDR; use 0.0.0.0/0 for the default route, not the word default. The current validator parses prefixes without rejecting IPv6 here, but this resource/controller is for IPv4. |
| `spec.type` | *Optional* | string; enum=["unicast", "blackhole"] | unicast by default; blackhole discards traffic and prohibits the device/gateway/source fields. |
| `spec.device` | *Optional* | string | Optional literal logical interface or OS device name, used when deviceFrom has no value. Empty on unicast becomes Pending/DeviceMissing; a missing OS device becomes DeviceNotReady. Forbidden for blackhole. |
| `spec.deviceFrom` | *Optional* | object | Optional status-value source for the device; resource and field follow the common reference rules. First normalized value takes precedence over device. No source by default; forbidden for blackhole. |
| `spec.gateway` | *Optional* | string | Optional bare IPv4 next hop, default empty for a route through the device without a gateway. Used as fallback for gatewayFrom. Forbidden for blackhole. |
| `spec.gatewayFrom` | *Optional* | object | Optional status source following common resource/field rules. Its first normalized value precedes gateway. If configured and neither source nor literal yields a value, reconciliation stays Pending/GatewayMissing. Forbidden for blackhole. |
| `spec.preferredSource` | *Optional* | string | Optional bare IPv4 preferred source for locally originated traffic; default none. Must already be a local address when applied, otherwise Pending/PreferredSourceNotLocal. This does not assign an address or perform SNAT. Forbidden for blackhole. |
| `spec.metric` | *Optional* | integer; minimum=0 | Optional nonnegative integer, default 0. Linux passes it only when positive; FreeBSD apply ignores this field. Lower metrics normally win among otherwise matching routes. |
| `spec.dependsOn` | *Optional* | array | Optional AND-list of common resource dependency predicates; default empty. Unsatisfied predicates prevent installation with DependsOnFalse. See common fields for resource/field/phase/equals/notEmpty/optional. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `type`, `destination`, `device`, `gateway`, `preferredSource`, `metric`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
