# BFD

[Resource configuration reference](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure FRR bfdd sessions through vtysh and observe their status for BGPPeer failure handling. This is a separate backend from GoBGP; BFD Down can trigger a BGP peer reset.

## Conditions and references

- Every declared BFD must be referenced by at least one BGPPeer.bfd. The BFD peer must match that BGPPeer name or one of its configured peer addresses. BGPDynamicPeer has no bfd field.
- Supported operating systems are Linux and FreeBSD with FRR bfdd/vtysh available. No interface means a multihop session using the referenced BGPRouter.routerID as local address; a direct IP peer without an interface requires exactly one BGPRouter. An interface selects a single-hop session.
- BFD detects its own forwarding/session failure; its Up status does not prove every routed destination is reachable. The GoBGP controller acts on observed Down after a previously Up BFD or established BGP session, rather than treating every unknown/startup state as Down.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    [ detectMultiplier: <integer> ]
    [ interface: <string> ]
    [ minRx: <string> ]
    [ minTx: <string> ]
    peer: <string>
    [ profile: ("" | "fast" | "normal" | "slow") ]
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
| `spec.peer` | **Required** | string | Required bare IP address or BGPPeer/name reference. A BGPPeer reference expands to all its peer addresses and takes the local address from its routerRef routerID. No hostname form for a direct peer. |
| `spec.interface` | *Optional* | string | Optional single-hop interface. Prefer Interface/name so the controller resolves the declared Interface.ifname; a plain name is validated as a declared logical interface but is passed literally to FRR. Omitted uses multihop. |
| `spec.profile` | *Optional* | string; enum=["", "fast", "normal", "slow"] | fast/omitted: minRx=minTx=300ms, multiplier=3; normal: 500ms/500ms/3; slow: 1000ms/1000ms/5. Explicit individual settings override these defaults. |
| `spec.minRx` | *Optional* | string | Optional Go duration, 50ms–60000ms, overriding profile receive interval. Converted to integer milliseconds; omitted uses profile. This is a BFD protocol timer, not a router reboot deadline. |
| `spec.minTx` | *Optional* | string | Optional Go duration, 50ms–60000ms, overriding profile transmit interval; emitted as integer milliseconds. |
| `spec.detectMultiplier` | *Optional* | integer; minimum=1; maximum=50 | Optional integer 1–50. Decoded 0/omission selects profile default (3, or 5 for slow). Multiplies the negotiated detection interval; not a count of routerd reconcile attempts. |
| `spec.when` | *Optional* | object | Optional common activation predicate; omitted is unconditional. See common fields for state/all/any, reference validation and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `peer`

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
