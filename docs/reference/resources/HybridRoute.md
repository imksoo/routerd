# HybridRoute

[Resource configuration reference](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Expand explicitly listed IPv4 destination networks into main-table IPv4Route resources through an OverlayPeer.

## Conditions and references

- Only nondefault IPv4 CIDRs are supported. default, 0.0.0.0/0, IPv6 and non-main tables are rejected. Each destination is masked and generates <resource-name>-<CIDR-with-nonalphanumeric-runs-replaced-by-hyphens>, such as branch-10-20-0-0-16.
- Generated route names must be unique and cannot collide with explicit IPv4Route names or destinations. Duplicate normalized destinations in one HybridRoute collide. An unhealthy HealthCheck changes HybridRoute status only: it does not gate installation, withdraw routes or select another peer.
- Each generated IPv4Route uses the peer underlay.interface as device. Only ipsec optionally supplies remote.address as an IPv4 gateway; other types use a device-only route. Existing tunnel/VPN setup and real reachability must be provided separately. Linux honors positive metric; the underlying FreeBSD IPv4Route application does not apply metric.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs:
      - <string>
    [ healthCheckRef: <string> ]
    [ install:
        [ metric: <integer> ]
        [ table: ("" | "main") ]
    ]
    peerRef: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.destinationCIDRs` | **Required** | array | Required nonempty IPv4 CIDR list, no default route. No implicit remote networks or discovery. |
| `spec.peerRef` | **Required** | string | Required bare existing OverlayPeer metadata.name. Although expansion can normalize OverlayPeer/name, normal cross-resource validation requires the bare name. |
| `spec.install` | *Optional* | object | Optional route-install settings; default main table, metric 0. No policy routing or automatic HA selection. |
| `spec.install.table` | *Optional* | string; enum=["", "main"] | Empty or main only; both lower to main-table IPv4Route. Other table names/numbers are rejected. |
| `spec.install.metric` | *Optional* | integer; minimum=0 | Nonnegative integer, default 0, copied to generated IPv4Route. Linux emits positive metrics only; FreeBSD path ignores metric. |
| `spec.healthCheckRef` | *Optional* | string | Optional bare existing HealthCheck name. Empty has no health-status augmentation. Unhealthy/failed/error status marks Degraded but does not change the routes; missing/unknown runtime status is not treated as unhealthy by this function. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/controller/chain/dynamic_effective.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dynamic_effective.go) · [pkg/controller/chain/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hybrid.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `routes`, `peerRef`, `defaultRouteUntouched`, `estimatedMTU`

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
