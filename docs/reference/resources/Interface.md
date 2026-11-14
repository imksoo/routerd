# Interface

[Resource configuration reference](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Names an existing OS interface so other resources can refer to its logical metadata.name. It does not create a physical interface.

## Conditions and references

- Use the logical metadata.name in interface fields of dependent resources and put the real device name in ifname. managed=false does not suppress LinkController admin-up/MTU operations when those settings are requested.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    [ adminUp: <boolean> ]
    ifname: <string>
    managed: <boolean>
    [ mtu: <integer> ]
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
| `spec.ifname` | **Required** | string | Required existing kernel device name, e.g. ens18; no default. Missing devices produce a Down/InterfaceNotFound status rather than being created. |
| `spec.managed` | **Required** | boolean | Management declaration, decoded default false; JSON Schema asks for explicit presence. It affects management/observation metadata and surrounding logic, but is not an observe-only guarantee for adminUp or MTU. |
| `spec.adminUp` | *Optional* | boolean | Default false: no request to bring the link up. True brings an existing down link up. False does not mean administratively force the link down. |
| `spec.mtu` | *Optional* | integer; minimum=576; maximum=9216 | Default zero means preserve the current MTU. Explicit nonzero values must be 576–9216 and are applied when different. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
