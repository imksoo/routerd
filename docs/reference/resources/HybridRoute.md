# HybridRoute

[Resource configuration reference](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Expand explicitly listed IPv4 destination networks into main-table IPv4Route resources through an OverlayPeer.

## Conditions and references

- Only nondefault IPv4 CIDRs are supported. default, 0.0.0.0/0, IPv6 and non-main tables are rejected. Each destination is masked and generates <resource-name>-<CIDR-with-nonalphanumeric-runs-replaced-by-hyphens>, such as branch-10-20-0-0-16.
- Generated route names must be unique and cannot collide with explicit IPv4Route names or destinations. Duplicate normalized destinations in one HybridRoute collide. An unhealthy HealthCheck changes HybridRoute status only: it does not gate installation, withdraw routes or select another peer.
- Each generated IPv4Route uses the peer underlay.interface as device. Only ipsec optionally supplies remote.address as an IPv4 gateway; other types use a device-only route. Existing tunnel/VPN setup and real reachability must be provided separately. Linux honors positive metric; the underlying FreeBSD IPv4Route application does not apply metric.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # Required; array
      - "<string>" # string
    healthCheckRef: "<string>" # optional; string
    install: # optional; object
      metric: 0 # optional; integer; minimum=0
      table: "" # optional; string; enum=["", "main"]
    peerRef: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.destinationCIDRs` | Required nonempty IPv4 CIDR list, no default route. No implicit remote networks or discovery. |
| `spec.peerRef` | Required bare existing OverlayPeer metadata.name. Although expansion can normalize OverlayPeer/name, normal cross-resource validation requires the bare name. |
| `spec.install` | Optional route-install settings; default main table, metric 0. No policy routing or automatic HA selection. |
| `spec.install.table` | Empty or main only; both lower to main-table IPv4Route. Other table names/numbers are rejected. |
| `spec.install.metric` | Nonnegative integer, default 0, copied to generated IPv4Route. Linux emits positive metrics only; FreeBSD path ignores metric. |
| `spec.healthCheckRef` | Optional bare existing HealthCheck name. Empty has no health-status augmentation. Unhealthy/failed/error status marks Degraded but does not change the routes; missing/unknown runtime status is not treated as unhealthy by this function. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/controller/chain/dynamic_effective.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dynamic_effective.go) · [pkg/controller/chain/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hybrid.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
