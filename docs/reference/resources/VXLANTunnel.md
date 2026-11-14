# VXLANTunnel

[Resource configuration reference](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Manage a Linux VXLAN device with static remote flood destinations and optional bridge attachment. No multicast discovery or dynamic MAC learning is configured.

## Conditions and references

- The operational controller returns without applying on non-Linux systems. Configure a Linux target for this resource; do not assume schema acceptance implies FreeBSD operation.
- when is fail-closed: false, unresolved, stale or pre-boot state tears down the device (down then delete), rather than only skipping updates. Freshness defaults to 30s per referenced state, overridden by maxAge. Unknown-accepting predicates are rejected by validation.
- tcpMSSClamp requires bridge plus explicit mtu >=1280. Rendering also requires that bridge to be an actual Bridge resource, with resolvable member interfaces; global reference validation alone only checks the shared interface index.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    [ bridge: <string> ]
    [ ifname: <string> ]
    localAddress: <string>
    [ mtu: <integer> ]
    [ outerDF: ("" | "inherit" | "set" | "unset") ]
    [ peers:
        - <string>
    ]
    [ tcpMSSClamp: <boolean> ]
    [ udpPort: <integer> ]
    underlayInterface: <string>
    vni: <integer>
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
| `spec.ifname` | *Optional* | string | OS VXLAN name; defaults to metadata.name. Must be <=15 characters without whitespace or slash. |
| `spec.vni` | **Required** | integer; minimum=1; maximum=16777215 | Required VXLAN network identifier, integer 1–16777215. Must match the intended remote layer-2 segment. |
| `spec.localAddress` | **Required** | string | Required local outer IP address; no dynamic status source. Validator accepts an IP address but does not verify local assignment or peer-family consistency. |
| `spec.peers` | *Optional* | array | Optional list of remote outer IP addresses, default empty. Reconciled as permanent self flood FDB entries for 00:00:00:00:00:00; undesired flood entries are removed. Empty provides no static remote flood destinations. |
| `spec.underlayInterface` | **Required** | string | Required declared logical interface in the same Router; resolved to its OS device name. No status-value source or auto-selection. |
| `spec.udpPort` | *Optional* | integer; minimum=1; maximum=65535 | Outer UDP destination port, default 4789 when omitted/0; explicit range 1–65535. |
| `spec.mtu` | **Conditionally required** | integer; minimum=0 | Optional inner-device MTU 576–9216; omitted/0 leaves OS/current MTU. tcpMSSClamp requires an explicit value >=1280 even though schema shows minimum 0. |
| `spec.bridge` | **Conditionally required** | string | Optional declared bridge name, resolved to its OS name and attached as the VXLAN master. Default detached. Required for tcpMSSClamp; use a Bridge resource rather than another interface kind. |
| `spec.outerDF` | *Optional* | string; enum=["", "inherit", "set", "unset"] | Outer IPv4 DF policy: inherit (default), set or unset, passed to ip link VXLAN df. Does not set an inner address or route. |
| `spec.when` | *Optional* | object | Optional common state/all/any predicate with additional fail-closed freshness rules above. Omitted means unconditional; conditions that accept unknown cannot enable this tunnel. |
| `spec.tcpMSSClamp` | *Optional* | boolean | Default false. True creates Linux bridge-family IPv4/IPv6 TCP SYN MSS rules between this VXLAN and Bridge member ports. Uses the minimum positive VXLAN/bridge/member MTU, minus 40 for IPv4 or 60 for IPv6; does not clamp arbitrary routed traffic. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `ifname`, `vni`
