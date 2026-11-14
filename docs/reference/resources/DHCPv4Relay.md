# DHCPv4Relay

[Resource configuration reference](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Relay DHCPv4 requests through dnsmasq to one explicitly configured IPv4 server.

## Conditions and references

- At least one declared interface and a bare IPv4 upstream address are required. No server discovery, upstream list, per-interface local relay address or condition field is provided.
- Runtime emits dhcp-relay=0.0.0.0,<upstream>,<resolved-ifname> for each interface in the shared dnsmasq process on Linux and FreeBSD. The upstream must be reachable using the host routing configuration.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    upstream: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interfaces` | **Required** | array | Required nonempty logical interface-name list; each name must exist in the same Router interface index. Runtime resolves Interface.ifname and supported generated-interface aliases. No default. |
| `spec.upstream` | **Required** | string | Required bare IPv4 DHCP server address, without port, CIDR or resource reference. No default. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`
