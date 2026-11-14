# BGPDynamicPeer

[Resource configuration reference](../resources.md#bgpdynamicpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Define a GoBGP inbound dynamic-neighbor admission group, not an individual peer instance or a WireGuard/SAM authorization identity.

## Conditions and references

- Peer import policies replace router import policy as a whole when considered configured; their fields are not merged individually. Export prefixes, in contrast, are unioned with router and runtime-derived advertisement prefixes.
- Both IPv4 and IPv6 unicast families are enabled by the current controller. An Established session does not by itself authorize all routes for FIB installation or prove data-plane connectivity.
- Ready means the peer group and source-prefix listener rules were applied. It does not mean that any dynamic neighbor is Established. The router convergence/graceful-restart configuration supplies dynamic-peer GR defaults.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    [ ebgpMultihop: <integer> ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
    ]
    [ importPolicy:
        [ allowedPrefixLengthMax: <integer> ]
        [ allowedPrefixLengthMin: <integer> ]
        [ allowedPrefixes:
            - <string>
        ]
        [ forbiddenCommunities:
            - <string>
        ]
        [ localPreference: <integer> ]
        [ nextHopRewrite: ("" | "peer-address" | "unchanged") ]
        [ requiredCommunities:
            - <string>
        ]
    ]
    listen:
      sourcePrefixes:
        - <string>
    [ password: <string> ]
    [ passwordFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    peerASN: <integer>
    [ routeReflectorClient: <boolean> ]
    [ routeReflectorClusterID: <string> ]
    routerRef: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
    ]
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
| `spec.importPolicy` | *Optional* | object | Effective import must have a nonempty IPv4 allowlist and min=max=32 for dynamic leaf route admission. If the peer allowlist is empty, validation uses the router import policy as a whole; a peer nextHopRewrite without a peer allowlist is rejected. Specify the full policy here rather than only ancillary overrides. |
| `spec.importPolicy.allowedPrefixes` | *Optional* | array | Optional IPv4/IPv6 CIDR allowlist. Import matches the prefix and its permitted more-specific lengths; masked duplicate prefixes are rejected. Empty adds no prefix match condition to an otherwise configured import policy. |
| `spec.importPolicy.allowedPrefixLengthMin` | *Optional* | integer; minimum=0; maximum=128 | Integer 0–128; 0/omitted uses each allowlist prefix length. A positive value must be >= each listed prefix length, <= address-family maximum and <= a positive maximum. |
| `spec.importPolicy.allowedPrefixLengthMax` | *Optional* | integer; minimum=0; maximum=128 | Integer 0–128; 0/omitted uses 32 for IPv4 or 128 for IPv6. A positive value must cover every listed prefix length and not exceed its family maximum. Dynamic leaf admission requires min=max=32. |
| `spec.importPolicy.requiredCommunities` | *Optional* | array | Optional community list; an accepted path must contain ALL listed communities. Empty adds no required-community condition. |
| `spec.importPolicy.forbiddenCommunities` | *Optional* | array | Optional community list; ANY matching community rejects the path before the accept rule. Empty adds no prohibition. |
| `spec.importPolicy.nextHopRewrite` | *Optional* | string; enum=["", "peer-address", "unchanged"] | peer-address by default; unchanged preserves the advertised next hop. Applied on accepted paths before selection/FIB projection. Setting only this field on a peer creates a peer-specific import policy instead of inheriting the router allowlist. |
| `spec.importPolicy.localPreference` | *Optional* | integer | Optional uint32 LOCAL_PREF for accepted paths before best-path selection. Default 0 leaves the BGP implementation default unchanged. |
| `spec.exportPolicy` | *Optional* | object | Optional export prefix filter. Per-peer prefixes are combined with router static and dynamic advertisements; route-reflector clients also receive router import prefixes. It is not a restrictive replacement for all router-derived export prefixes. |
| `spec.exportPolicy.allowedPrefixes` | *Optional* | array | Optional CIDR list, default empty. Effective nonempty export lists allow the listed prefixes and their more-specifics (through /32 or /128), rejecting unmatched routes. An effective empty list installs no export filter. |
| `spec.timers` | *Optional* | object | Optional timer-profile object. Router-level timers are not inherited by peers in the current controller; specify each peer timer profile explicitly when needed. |
| `spec.timers.profile` | *Optional* | string; enum=["", "default", "fast", "slow"] | default/omitted: keepalive 30s, hold 90s, connectRetry 10s; fast: 3s/9s/1s; slow: 60s/180s/30s. Low-level keepalive/holdTime/connectRetry YAML fields are unsupported. |
| `spec.when` | *Optional* | object | Optional common activation predicate; omitted is unconditional. See common fields for state/all/any, reference validation and schema limitations. |
| `spec.routerRef` | **Required** | string | Required BGPRouter/name referencing a declared router in the same Router configuration. Plain names are not accepted. |
| `spec.peerASN` | **Required** | integer; minimum=1 | Required nonzero uint32 remote ASN. Matching the local ASN selects iBGP; routeReflectorClient requires this equality. |
| `spec.password` | *Optional* | string | Optional inline BGP TCP authentication secret, default none; mutually exclusive with a nonempty passwordFrom source. Not a SAM enrollment token. |
| `spec.passwordFrom` | *Optional* | object | Optional secret source; exactly one of file/env when used. Omitted means no external secret. Not a status-value or Kind/name reference. Missing runtime files/environment variables fail peer reconciliation. |
| `spec.passwordFrom.file` | *Optional* | string | Router-local file containing raw password text. Exclusive with env and inline password. Trailing CR/LF is removed, but surrounding spaces are otherwise retained. |
| `spec.passwordFrom.env` | *Optional* | string | Name of a routerd process environment variable containing the secret, not the secret itself. Exclusive with file and inline password. No automatic environment-file loading. |
| `spec.passwordFrom.base64` | *Optional* | boolean | Default false. True decodes standard Base64 from the selected file/env value, then removes trailing CR/LF. This is encoding, not encryption. |
| `spec.ebgpMultihop` | *Optional* | integer; minimum=0; maximum=255 | Integer 0–255, default 0. Only values >1 enable GoBGP eBGP multihop and set its TTL; 0/1 leave multihop disabled. |
| `spec.routeReflectorClient` | *Optional* | boolean | Default false. True marks an iBGP route-reflector client and requires peerASN=router ASN. It also expands effective export prefixes with router import prefixes. |
| `spec.routeReflectorClusterID` | *Optional* | string | Optional IPv4-form cluster ID, passed only when routeReflectorClient is true. Empty leaves selection to GoBGP; setting it alone does not enable reflection. |
| `spec.listen` | **Required** | object | Required inbound admission rules, not a discovered peer or SAM enrollment object. |
| `spec.listen.sourcePrefixes` | **Required** | array | Required nonempty IPv4/IPv6 source CIDR list for GoBGP dynamic neighbors; masked duplicates are rejected. Restricts the incoming TCP peer address, not the routes it may advertise. Route admission uses importPolicy separately. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `peerGroup`, `sourcePrefixes`, `sourcePrefixCount`, `discoveredPeers`, `discoveredPeerCount`, `acceptedRouteCount`, `rejectedRouteCount`, `rejectedRouteSummary`, `observedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
