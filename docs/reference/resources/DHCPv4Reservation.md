# DHCPv4Reservation

[Resource configuration reference](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Reserve one IPv4 address for a MAC address on a declared DHCPv4Server.

## Conditions and references

- server, macAddress and ipAddress are semantically required even though the schema marks server optional. server must be the bare name of a DHCPv4Server with nonempty interface; there is no implicit single-server fallback in normal validation. scope is rejected.
- Current runtime places reservations in the shared dnsmasq hosts sidecar with a per-resource tag. Server association controls validity/status and custom-option rendering; it is not a separate dnsmasq process or independent host-file namespace. Applied does not prove that a client obtained the lease.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # optional; string
    ipAddress: "<string>" # Required; string
    leaseTime: "<string>" # optional; string
    macAddress: "<string>" # Required; string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    scope: "<string>" # optional; string
    server: "<string>" # optional; string
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
| `spec.scope` | Removed field; any nonempty value is rejected. Use server. |
| `spec.server` | Required bare metadata.name of an existing direct DHCPv4Server with interface set; Kind/name is not stripped here. |
| `spec.macAddress` | Required MAC address parsed by net.ParseMAC; emitted lowercase. No default. |
| `spec.hostname` | Optional hostname without whitespace or comma; default omitted. Can feed the DNSZone DHCP-derived host snapshot when enabled. |
| `spec.ipAddress` | Required bare IPv4 reservation address. Current validation checks family but does not require membership in the server pool or reject duplicate reservation IP/MAC pairs here. |
| `spec.leaseTime` | Optional backend duration string, default empty. Commas are rejected; current runtime reservation host-line writer does not emit this field, so it does not override the lease lifetime there. |
| `spec.options` | Optional DHCP option list emitted using this reservation tag. Each item needs exactly one of code/name and a nonempty value. Default empty. |
| `spec.options[].code` | Numeric option identifier, exclusive with name; schema range 1–65535. No default identifier. |
| `spec.options[].name` | dnsmasq option name, exclusive with code; no whitespace or commas. |
| `spec.options[].value` | Required nonempty backend-formatted value; no CR/LF. Passed through, so commas may separate multiple DHCP option values. |
| `spec.when` | Optional common state condition; default unconditional. False excludes the reservation and reports Pending/WhenFalse; an unavailable server reports ServerPending. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `hostname`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
