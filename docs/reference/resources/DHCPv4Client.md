# DHCPv4Client

[Resource configuration reference](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Runs the managed routerd-dhcpv4-client daemon and applies its current lease address, optional default route and optional host DNS settings.

## Conditions and references

- The interface must exist in the declared Router. DHCP identity fields are sent as protocol options, not resource references. Lease expiry/release removes the previously applied address, route and host DNS state; useRoutes/useDNS do not stop lease acquisition.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # optional; string
    clientID: "<string>" # optional; string
    hostname: "<string>" # optional; string
    interface: "<string>" # Required; string
    requestedAddress: "<string>" # optional; string
    routeMetric: 0 # optional; integer; minimum=0
    useDNS: true # optional; boolean
    useRoutes: true # optional; boolean
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
| `spec.interface` | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.hostname` | DHCP option 12 hostname. Default empty: omit the option; it is not automatically filled from the OS hostname. Whitespace is rejected. |
| `spec.requestedAddress` | Optional preferred IPv4 address, default absent. Must be a bare IPv4 address without prefix. Sent as option 50; it is a request, not a guaranteed/static assignment. |
| `spec.classID` | Vendor class identifier sent as option 60 string bytes. Default empty: omit it. No hexadecimal decoding. |
| `spec.clientID` | Option 61 client identity. An explicit value is sent as string bytes, not decoded hex. Default is byte 1 followed by the interface hardware address. Keep the value stable when the DHCP server identifies leases by client ID. |
| `spec.useRoutes` | Default true: apply the lease defaultGateway as a default route when present. False removes the prior managed default route but retains lease acquisition and address application. It does not suppress the gateway value in status. |
| `spec.useDNS` | Default true: apply leased DNS servers to the host via resolvectl when /etc/resolv.conf points to systemd-resolved, otherwise write resolv.conf. False clears prior managed DNS state; DNS addresses remain available in status for DNSResolver-related references. |
| `spec.routeMetric` | Default 0. Used for the applied default-route metric and platform-specific lease-address reconciliation. Schema requires a nonnegative integer. It is not a routing-table ID. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
