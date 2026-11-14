# BGPRouter

[Resource configuration reference](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure the local GoBGP instance, its admitted routes and locally originated advertisements. Actual kernel installation and remote reachability are separate from a BGP Established state.

## Conditions and references

- Live operation currently supports one BGPRouter and no VRF. Validator acceptance of distinct ASN/VRF/listen instances does not imply runtime support.
- Router import, connected redistribution and static redistribution prefix lists cannot overlap across these three groups. Masked duplicates within prefix lists are rejected.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: <integer>
    [ backend: ("" | "gobgp") ]
    [ communities:
        [ accept:
            - <string>
        ]
        [ send: ("" | "standard" | "extended" | "both") ]
        [ set:
            [ in:
                - <string>
            ]
            [ out:
                - <string>
            ]
        ]
    ]
    [ convergenceProfile: ("" | "default" | "fast" | "stable") ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
    ]
    [ gracefulRestart:
        [ enabled: <boolean> ]
        [ restartTime: <string> ]
        [ stalePathTime: <string> ]
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
    [ listen:
        [ address: <string> ]
        [ port: <integer> ]
    ]
    [ redistribute:
        [ connected:
            [ allowedPrefixes:
                - <string>
            ]
        ]
        [ static:
            [ allowedPrefixes:
                - <string>
            ]
        ]
    ]
    routerID: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
    ]
    [ vrf: <string> ]
    [ watcher:
        [ maxPrefixes: <integer> ]
        [ peerStateChangeThrottle: <string> ]
        [ pollInterval: <string> ]
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
| `spec.importPolicy` | *Optional* | object | Optional router-wide import policy inherited by peers without an override. Empty installs no router import filter, but does not itself authorize every received route for kernel FIB installation. |
| `spec.importPolicy.allowedPrefixes` | *Optional* | array | Optional IPv4/IPv6 CIDR allowlist. Import matches the prefix and its permitted more-specific lengths; masked duplicate prefixes are rejected. Empty adds no prefix match condition to an otherwise configured import policy. |
| `spec.importPolicy.allowedPrefixLengthMin` | *Optional* | integer; minimum=0; maximum=128 | Integer 0–128; 0/omitted uses each allowlist prefix length. A positive value must be >= each listed prefix length, <= address-family maximum and <= a positive maximum. |
| `spec.importPolicy.allowedPrefixLengthMax` | *Optional* | integer; minimum=0; maximum=128 | Integer 0–128; 0/omitted uses 32 for IPv4 or 128 for IPv6. A positive value must cover every listed prefix length and not exceed its family maximum. Dynamic leaf admission requires min=max=32. |
| `spec.importPolicy.requiredCommunities` | *Optional* | array | Optional community list; an accepted path must contain ALL listed communities. Empty adds no required-community condition. |
| `spec.importPolicy.forbiddenCommunities` | *Optional* | array | Optional community list; ANY matching community rejects the path before the accept rule. Empty adds no prohibition. |
| `spec.importPolicy.nextHopRewrite` | *Optional* | string; enum=["", "peer-address", "unchanged"] | peer-address by default; unchanged preserves the advertised next hop. Applied on accepted paths before selection/FIB projection. Setting only this field on a peer creates a peer-specific import policy instead of inheriting the router allowlist. |
| `spec.importPolicy.localPreference` | *Optional* | integer | Optional uint32 LOCAL_PREF for accepted paths before best-path selection. Default 0 leaves the BGP implementation default unchanged. |
| `spec.exportPolicy` | *Optional* | object | Optional export prefix filter. Per-peer prefixes are combined with router static and dynamic advertisements; route-reflector clients also receive router import prefixes. It is not a restrictive replacement for all router-derived export prefixes. |
| `spec.exportPolicy.allowedPrefixes` | *Optional* | array | Explicit local advertisement CIDRs, default empty. Combined with redistribute.connected/static and dynamic advertisements, and also used in peer export filtering; these prefixes are originated without an OS-route existence check. |
| `spec.timers` | *Optional* | object | Optional timer-profile object. Router-level timers are not inherited by peers in the current controller; specify each peer timer profile explicitly when needed. |
| `spec.timers.profile` | *Optional* | string; enum=["", "default", "fast", "slow"] | Accepted default/fast/slow, but router-level timer settings are not used to build peer timers in the current runtime. Configure BGPPeer.timers.profile or BGPDynamicPeer.timers.profile instead. |
| `spec.when` | *Optional* | object | Optional common activation predicate; omitted is unconditional. See common fields for state/all/any, reference validation and schema limitations. |
| `spec.asn` | **Required** | integer; minimum=1 | Required nonzero uint32 local ASN. Configuration rejects reuse across BGPRouter resources; runtime currently supports one BGPRouter only. |
| `spec.routerID` | **Required** | string | Required IPv4-form BGP router identifier; independent of the transport address. BFD without an interface also uses it as the local multihop address. |
| `spec.vrf` | *Optional* | string | Optional VRF/name or plain VRF name referencing a declared VRF. Validation supports it, but the operational GoBGP controller currently rejects any nonempty vrf as GoBGPVRFUnsupported. Omit for live use. |
| `spec.listen` | *Optional* | object | Optional local TCP listener configuration; default all addresses, port 179. Does not select the remote peer TCP port. |
| `spec.listen.address` | *Optional* | string | Optional local IPv4/IPv6 bind address. Empty leaves GoBGP listening on all addresses; must be an IP, not a resource reference or hostname. |
| `spec.listen.port` | *Optional* | integer; minimum=1; maximum=65535 | TCP listen port 1–65535, default 179 when omitted/0. Conflicting configured address/port claims are rejected. |
| `spec.redistribute` | *Optional* | object | Optional explicitly advertised prefix groups. Current runtime unions connected/static lists with exportPolicy.allowedPrefixes; it does not discover matching routes from the OS. |
| `spec.redistribute.connected` | *Optional* | object | Optional connected-labelled advertisement group; default empty. The label does not gate advertisements on kernel connected-route presence. |
| `spec.redistribute.connected.allowedPrefixes` | *Optional* | array | Explicit IPv4/IPv6 CIDRs to originate, default empty. Cannot overlap router import or static redistribution prefixes. Does not require a corresponding connected route. |
| `spec.redistribute.static` | *Optional* | object | Optional static-labelled advertisement group; default empty. Not an automatic scan of IPv4StaticRoute/IPv6StaticRoute resources. |
| `spec.redistribute.static.allowedPrefixes` | *Optional* | array | Explicit IPv4/IPv6 CIDRs to originate, default empty. Cannot overlap router import or connected redistribution prefixes; no kernel static-route existence check. |
| `spec.communities` | *Optional* | object | Optional community settings. Only router set.out affects locally originated advertisements in the present runtime; send, accept and set.in are decoded/validated but unused. |
| `spec.communities.send` | *Optional* | string; enum=["", "standard", "extended", "both"] | Accepted values standard, extended, both or empty; no operational effect. GoBGP peers use SendCommunity=3 regardless of this setting. |
| `spec.communities.accept` | *Optional* | array | Optional validated standard community list, default empty; currently does not filter import. Use importPolicy.requiredCommunities/forbiddenCommunities for actual matching. |
| `spec.communities.set` | *Optional* | object | Optional inbound/outbound community lists; only router out is consumed by the present controller. |
| `spec.communities.set.in` | *Optional* | array | Optional validated list, default empty; currently not applied to imported paths. |
| `spec.communities.set.out` | *Optional* | array | Standard communities attached to router-created local advertisements, default empty. Does not rewrite every transit route. Use asn:value (standard 16-bit components) or supported well-known names; extended communities are not encoded here. |
| `spec.convergenceProfile` | *Optional* | string; enum=["", "default", "fast", "stable"] | default/omitted or stable keeps graceful restart enabled by default; fast disables it unless gracefulRestart.enabled explicitly overrides. It does not automatically select timers.profile. |
| `spec.gracefulRestart` | *Optional* | object | Optional graceful restart settings shared with peers after convergence-profile selection. Explicit enabled overrides the profile default. |
| `spec.gracefulRestart.enabled` | *Optional* | boolean | Optional boolean; default true except fast convergence. False disables GR even if durations are specified. |
| `spec.gracefulRestart.restartTime` | *Optional* | string | Positive Go duration, default 120s, converted to whole seconds by truncation. Subsecond values can therefore become 0; use whole-second values. |
| `spec.gracefulRestart.stalePathTime` | *Optional* | string | Positive Go duration for stale-route retention, default 360s; converted to whole seconds. Ignored when GR is disabled. |
| `spec.watcher` | *Optional* | object | Optional status-observation settings; these are not BGP keepalive/hold timers or peer admission limits. |
| `spec.watcher.pollInterval` | *Optional* | string | Go duration >=3s. Effective polling starts at 15s and only decreases for smaller configured values, so values above 15s do not lengthen the interval. |
| `spec.watcher.maxPrefixes` | *Optional* | integer; minimum=1; maximum=999999 | Observation prefix limit, default 4096; explicit positive values must be <1000000. Does not configure a remote session maximum-prefix shutdown policy. |
| `spec.watcher.peerStateChangeThrottle` | *Optional* | string | Nonnegative Go duration limiting repeated peer-state event publication; omitted/0 has no throttling. Does not delay the BGP protocol state machine. |
| `spec.backend` | *Optional* | string; enum=["", "gobgp"] | gobgp by default and the only accepted backend. Uses the routerd-bgp daemon/control client, not an FRR BGP configuration file. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/bgp/watcher.go](https://github.com/imksoo/routerd/blob/main/pkg/bgp/watcher.go) · [pkg/controller/bgp/fib_linux.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_linux.go) · [pkg/controller/bgp/fib_bsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_bsd.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `peers`, `prefixes`, `establishedPeers`, `acceptedPrefixes`, `observedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
