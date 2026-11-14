# DHCPv6Address

[Resource configuration reference](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares an IPv6 address acquired by a host DHCPv6 client. It is separate from DHCPv6PrefixDelegation and does not allocate LAN prefixes.

## Conditions and references

- The current code validates/indexes this declaration and uses it in observation and host network-adoption decisions. It does not create a dedicated DHCPv6Address daemon. Host client setup must not be inferred solely from a successful plan.
- An external prefix-delegation client on the same interface must match the DHCPv6Address client selection. The configuration index treats an omitted client as networkd, whereas the current observe/plan display uses dhcpcd as its omitted default. Specify the client explicitly rather than relying on that inconsistent omission.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # optional; string
    interface: "<string>" # Required; string
    required: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interface` | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.client` | Host DHCPv6 client/program selector, default empty in the decoded config. networkd is recognized by adoption logic; other explicit names disable networkd DHCPv6 for that interface. The validator does not restrict this to a known enum. No resource-name reference. |
| `spec.required` | Default false. Decoded but not consumed by the current address observation/adoption implementation; it does not introduce a wait-for-address requirement. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)
