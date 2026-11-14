# OverlayPeer

[Resource configuration reference](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Describe an existing transport used by HybridRoute and path-MTU policy derivation. This resource does not establish WireGuard, Tailscale, IPsec or a tunnel by itself.

## Conditions and references

- role, nodeID and underlay.type are required. wireguard/ipip/gre/fou/gue require underlay.interface in normal validation; using any type through HybridRoute requires it at expansion time, including tailscale/ipsec/route.
- For ipip/gre/fou/gue the bare underlay.interface must reference a declared TunnelInterface; validation does not check that its mode matches underlay.type. WireGuard may reference an externally managed device (warning rather than rejection). Other underlay devices must already be usable.
- nodeID/remote.nodeID and role are descriptors, not discovery, authentication, HA election or automatic topology matching. No when or dependsOn exists on this resource.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: <string>
    [ pathMTU:
        [ forceFragmentIPv4: <boolean> ]
    ]
    [ remote:
        [ address: <string> ]
        [ nodeID: <string> ]
    ]
    role: ("onprem" | "cloud")
    underlay:
      [ address: <string> ]
      [ interface: <string> ]
      type: ("wireguard" | "tailscale" | "ipsec" | "route" | "ipip" | "gre" | "fou" | "gue")
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.role` | **Required** | string; enum=["onprem", "cloud"] | Required onprem or cloud. No default; does not create cloud resources or choose a provider. |
| `spec.nodeID` | **Required** | string | Required nonblank local node identity label. Not resolved to a host or external account. |
| `spec.underlay` | **Required** | object | Required transport descriptor. Configure the actual network transport separately. |
| `spec.underlay.type` | **Required** | string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"] | Required wireguard/tailscale/ipsec/route/ipip/gre/fou/gue. Selects route lowering and MTU-overhead estimation, not daemon creation. |
| `spec.underlay.interface` | *Optional* | string | Device/logical interface used by generated IPv4Route; required for route expansion. Bare TunnelInterface name for tunnel modes; use actual OS device name for externally managed devices. No automatic interface discovery. |
| `spec.underlay.address` | *Optional* | string | Optional bare IPv4/IPv6 descriptor, validated as an IP. Current HybridRoute target derivation does not use it as gateway/local bind or assign it to the interface. |
| `spec.remote` | *Optional* | object | Optional remote descriptor, default empty. No peer registration or negotiation. |
| `spec.remote.nodeID` | *Optional* | string | Optional remote identity label; default empty, not used for live peer discovery or authorization. |
| `spec.remote.address` | *Optional* | string | Optional IP string. For ipsec route lowering, a parseable IPv4 value becomes gateway; invalid/IPv6/empty yields a device-only route. Other types do not use it as a gateway, and this field is not separately validated as an IP by OverlayPeer validation. |
| `spec.pathMTU` | *Optional* | object | Optional forwarded-path MTU options, default no force-fragment request. Does not encrypt the underlay. |
| `spec.pathMTU.forceFragmentIPv4` | *Optional* | boolean | Default false; true accepted only for wireguard/ipip/gre/fou/gue. Requests DF clearing for oversized IPv4 on matching derived forwarded paths using Linux nftables; not a global tunnel flag or IPv6 fragmentation. FreeBSD skips this nftables controller and has no equivalent DF-clear action in this path. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `nodeID`, `role`, `underlayType`, `underlayInterface`

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
