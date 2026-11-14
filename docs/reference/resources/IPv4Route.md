# IPv4Route

[Resource configuration reference](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Install an IPv4 unicast or blackhole route, optionally resolving device and gateway from resource status.

## Conditions and references

- blackhole excludes device, deviceFrom, gateway, gatewayFrom and preferredSource. unicast needs a resolved device at runtime; a gateway is optional for a directly connected route.
- Dynamic values take precedence over literal device/gateway; unresolved values fall back to the literals. The source optional flag does not bypass DeviceMissing or GatewayMissing when no usable value remains. References must still identify an existing resource and an explicitly provided field.
- Linux uses ip route replace, with src for preferredSource and metric only when positive. FreeBSD uses route change/add and -ifa for preferredSource; this path does not apply metric. Installed status is not an end-to-end reachability check.
- ready_when and ${...} expressions in device/gateway were removed: use dependsOn and explicit deviceFrom/gatewayFrom. preferredSource does not support status expressions.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    destination: "<string>" # Required; string
    device: "<string>" # optional; string
    deviceFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    gateway: "<string>" # optional; string
    gatewayFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    metric: 0 # optional; integer; minimum=0
    preferredSource: "<string>" # optional; string
    type: "unicast" # optional; string; enum=["unicast", "blackhole"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.destination` | Required IPv4 CIDR; use 0.0.0.0/0 for the default route, not the word default. The current validator parses prefixes without rejecting IPv6 here, but this resource/controller is for IPv4. |
| `spec.type` | unicast by default; blackhole discards traffic and prohibits the device/gateway/source fields. |
| `spec.device` | Optional literal logical interface or OS device name, used when deviceFrom has no value. Empty on unicast becomes Pending/DeviceMissing; a missing OS device becomes DeviceNotReady. Forbidden for blackhole. |
| `spec.deviceFrom` | Optional status-value source for the device; resource and field follow the common reference rules. First normalized value takes precedence over device. No source by default; forbidden for blackhole. |
| `spec.gateway` | Optional bare IPv4 next hop, default empty for a route through the device without a gateway. Used as fallback for gatewayFrom. Forbidden for blackhole. |
| `spec.gatewayFrom` | Optional status source following common resource/field rules. Its first normalized value precedes gateway. If configured and neither source nor literal yields a value, reconciliation stays Pending/GatewayMissing. Forbidden for blackhole. |
| `spec.preferredSource` | Optional bare IPv4 preferred source for locally originated traffic; default none. Must already be a local address when applied, otherwise Pending/PreferredSourceNotLocal. This does not assign an address or perform SNAT. Forbidden for blackhole. |
| `spec.metric` | Optional nonnegative integer, default 0. Linux passes it only when positive; FreeBSD apply ignores this field. Lower metrics normally win among otherwise matching routes. |
| `spec.dependsOn` | Optional AND-list of common resource dependency predicates; default empty. Unsatisfied predicates prevent installation with DependsOnFalse. See common fields for resource/field/phase/equals/notEmpty/optional. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `type`, `destination`, `device`, `gateway`, `preferredSource`, `metric`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
