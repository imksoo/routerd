# IPv4StaticAddress

[Resource configuration reference](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares a static IPv4 address on a logical interface. The controller converges configured addresses; this is not a DHCP lease.

## Conditions and references

- An interface reference and address/prefix are required. Router-wide indexing rejects two static resources with the same logical interface and masked network prefix, even if the host address differs. VirtualAddress overlap on that same interface/prefix is also rejected.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: <string>
    [ allowOverlap: <boolean> ]
    [ allowOverlapReason: <string> ]
    [ exclusive: <boolean> ]
    interface: <string>
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
| `spec.address` | **Required** | string | Required IPv4 host address with prefix length, e.g. 192.0.2.1/24. The parser checks prefix syntax; runtime operations use IPv4. No default or implicit /32. |
| `spec.exclusive` | *Optional* | boolean | Default false. The current observer warns that exclusive cleanup is not implemented; the runtime address controller does not read this flag. Global reconciliation of extra addresses is independent, so false does not promise to preserve manual addresses. |
| `spec.allowOverlap` | *Optional* | boolean | Default false. Retained as a readable field but not used by current config validation or address reconciliation; it does not bypass duplicate/conflict checks. |
| `spec.allowOverlapReason` | *Optional* | string | Default empty. Stored declaration only; it does not authorize overlap or alter the effect of allowOverlap. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `ifname`, `address`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
