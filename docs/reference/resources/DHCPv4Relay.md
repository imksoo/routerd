# DHCPv4Relay

[Resource configuration reference](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Relay DHCPv4 requests through dnsmasq to one explicitly configured IPv4 server.

## Conditions and references

- At least one declared interface and a bare IPv4 upstream address are required. No server discovery, upstream list, per-interface local relay address or condition field is provided.
- Runtime emits dhcp-relay=0.0.0.0,<upstream>,<resolved-ifname> for each interface in the shared dnsmasq process on Linux and FreeBSD. The upstream must be reachable using the host routing configuration.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # Required; array
      - "<string>" # string
    upstream: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interfaces` | Required nonempty logical interface-name list; each name must exist in the same Router interface index. Runtime resolves Interface.ifname and supported generated-interface aliases. No default. |
| `spec.upstream` | Required bare IPv4 DHCP server address, without port, CIDR or resource reference. No default. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`
