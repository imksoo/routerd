# OverlayPeer

[Resource configuration reference](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Describe an existing transport used by HybridRoute and path-MTU policy derivation. This resource does not establish WireGuard, Tailscale, IPsec or a tunnel by itself.

## Conditions and references

- role, nodeID and underlay.type are required. wireguard/ipip/gre/fou/gue require underlay.interface in normal validation; using any type through HybridRoute requires it at expansion time, including tailscale/ipsec/route.
- For ipip/gre/fou/gue the bare underlay.interface must reference a declared TunnelInterface; validation does not check that its mode matches underlay.type. WireGuard may reference an externally managed device (warning rather than rejection). Other underlay devices must already be usable.
- nodeID/remote.nodeID and role are descriptors, not discovery, authentication, HA election or automatic topology matching. No when or dependsOn exists on this resource.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # Required; string
    pathMTU: # optional; object
      forceFragmentIPv4: true # optional; boolean
    remote: # optional; object
      address: "<string>" # optional; string
      nodeID: "<string>" # optional; string
    role: "onprem" # Required; string; enum=["onprem", "cloud"]
    underlay: # Required; object
      address: "<string>" # optional; string
      interface: "<string>" # optional; string
      type: "wireguard" # Required; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.role` | Required onprem or cloud. No default; does not create cloud resources or choose a provider. |
| `spec.nodeID` | Required nonblank local node identity label. Not resolved to a host or external account. |
| `spec.underlay` | Required transport descriptor. Configure the actual network transport separately. |
| `spec.underlay.type` | Required wireguard/tailscale/ipsec/route/ipip/gre/fou/gue. Selects route lowering and MTU-overhead estimation, not daemon creation. |
| `spec.underlay.interface` | Device/logical interface used by generated IPv4Route; required for route expansion. Bare TunnelInterface name for tunnel modes; use actual OS device name for externally managed devices. No automatic interface discovery. |
| `spec.underlay.address` | Optional bare IPv4/IPv6 descriptor, validated as an IP. Current HybridRoute target derivation does not use it as gateway/local bind or assign it to the interface. |
| `spec.remote` | Optional remote descriptor, default empty. No peer registration or negotiation. |
| `spec.remote.nodeID` | Optional remote identity label; default empty, not used for live peer discovery or authorization. |
| `spec.remote.address` | Optional IP string. For ipsec route lowering, a parseable IPv4 value becomes gateway; invalid/IPv6/empty yields a device-only route. Other types do not use it as a gateway, and this field is not separately validated as an IP by OverlayPeer validation. |
| `spec.pathMTU` | Optional forwarded-path MTU options, default no force-fragment request. Does not encrypt the underlay. |
| `spec.pathMTU.forceFragmentIPv4` | Default false; true accepted only for wireguard/ipip/gre/fou/gue. Requests DF clearing for oversized IPv4 on matching derived forwarded paths using Linux nftables; not a global tunnel flag or IPv6 fragmentation. FreeBSD skips this nftables controller and has no equivalent DF-clear action in this path. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
