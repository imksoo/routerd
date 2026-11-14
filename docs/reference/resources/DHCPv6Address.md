# DHCPv6Address

[Resource configuration reference](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares an IPv6 address acquired by a host DHCPv6 client. It is separate from DHCPv6PrefixDelegation and does not allocate LAN prefixes.

## Conditions and references

- The current code validates/indexes this declaration and uses it in observation and host network-adoption decisions. It does not create a dedicated DHCPv6Address daemon. Host client setup must not be inferred solely from a successful plan.
- An external prefix-delegation client on the same interface must match the DHCPv6Address client selection. The configuration index treats an omitted client as networkd, whereas the current observe/plan display uses dhcpcd as its omitted default. Specify the client explicitly rather than relying on that inconsistent omission.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    [ client: <string> ]
    interface: <string>
    [ required: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interface` | **Required** | string | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.client` | *Optional* | string | Host DHCPv6 client/program selector, default empty in the decoded config. networkd is recognized by adoption logic; other explicit names disable networkd DHCPv6 for that interface. The validator does not restrict this to a known enum. No resource-name reference. |
| `spec.required` | *Optional* | boolean | Default false. Decoded but not consumed by the current address observation/adoption implementation; it does not introduce a wait-for-address requirement. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `address`
