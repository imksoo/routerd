# BGPPeer

[Resource configuration reference](../resources.md#bgppeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Configure one or more static BGP neighbors on a declared BGPRouter.

## Conditions and references

- Peer import policies replace router import policy as a whole when considered configured; their fields are not merged individually. Export prefixes, in contrast, are unioned with router and runtime-derived advertisement prefixes.
- Both IPv4 and IPv6 unicast families are enabled by the current controller. An Established session does not by itself authorize all routes for FIB installation or prove data-plane connectivity.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # optional; string
    communities: # optional; object
      accept: # optional; array
        - "<string>" # string
      send: "" # optional; string; enum=["", "standard", "extended", "both"]
      set: # optional; object
        in: # optional; array
          - "<string>" # string
        out: # optional; array
          - "<string>" # string
    convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
    exportPolicy: # optional; object
      allowedPrefixes: # optional; array
        - "<string>" # string
    importPolicy: # optional; object
      allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixes: # optional; array
        - "<string>" # string
      forbiddenCommunities: # optional; array
        - "<string>" # string
      localPreference: 0 # optional; integer
      nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # optional; array
        - "<string>" # string
    passiveMode: true # optional; boolean
    password: "<string>" # optional; string
    passwordFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    peerASN: 1 # Required; integer; minimum=1
    peers: # optional; array
      - "<string>" # string
    routeReflectorClient: true # optional; boolean
    routeReflectorClusterID: "<string>" # optional; string
    routerRef: "<string>" # Required; string
    timers: # optional; object
      profile: "" # optional; string; enum=["", "default", "fast", "slow"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.importPolicy` | Optional import policy. A peer with no nonempty prefixes, community constraints, next-hop rewrite or localPreference inherits the router policy as a whole; prefix-length bounds alone do not count as an override. |
| `spec.importPolicy.allowedPrefixes` | Optional IPv4/IPv6 CIDR allowlist. Import matches the prefix and its permitted more-specific lengths; masked duplicate prefixes are rejected. Empty adds no prefix match condition to an otherwise configured import policy. |
| `spec.importPolicy.allowedPrefixLengthMin` | Integer 0–128; 0/omitted uses each allowlist prefix length. A positive value must be >= each listed prefix length, <= address-family maximum and <= a positive maximum. |
| `spec.importPolicy.allowedPrefixLengthMax` | Integer 0–128; 0/omitted uses 32 for IPv4 or 128 for IPv6. A positive value must cover every listed prefix length and not exceed its family maximum. Dynamic leaf admission requires min=max=32. |
| `spec.importPolicy.requiredCommunities` | Optional community list; an accepted path must contain ALL listed communities. Empty adds no required-community condition. |
| `spec.importPolicy.forbiddenCommunities` | Optional community list; ANY matching community rejects the path before the accept rule. Empty adds no prohibition. |
| `spec.importPolicy.nextHopRewrite` | peer-address by default; unchanged preserves the advertised next hop. Applied on accepted paths before selection/FIB projection. Setting only this field on a peer creates a peer-specific import policy instead of inheriting the router allowlist. |
| `spec.importPolicy.localPreference` | Optional uint32 LOCAL_PREF for accepted paths before best-path selection. Default 0 leaves the BGP implementation default unchanged. |
| `spec.exportPolicy` | Optional export prefix filter. Per-peer prefixes are combined with router static and dynamic advertisements; route-reflector clients also receive router import prefixes. It is not a restrictive replacement for all router-derived export prefixes. |
| `spec.exportPolicy.allowedPrefixes` | Optional CIDR list, default empty. Effective nonempty export lists allow the listed prefixes and their more-specifics (through /32 or /128), rejecting unmatched routes. An effective empty list installs no export filter. |
| `spec.timers` | Optional timer-profile object. Router-level timers are not inherited by peers in the current controller; specify each peer timer profile explicitly when needed. |
| `spec.timers.profile` | default/omitted: keepalive 30s, hold 90s, connectRetry 10s; fast: 3s/9s/1s; slow: 60s/180s/30s. Low-level keepalive/holdTime/connectRetry YAML fields are unsupported. |
| `spec.when` | Optional common activation predicate; omitted is unconditional. See common fields for state/all/any, reference validation and schema limitations. |
| `spec.routerRef` | Required BGPRouter/name referencing a declared router in the same Router configuration. Plain names are not accepted. |
| `spec.peerASN` | Required nonzero uint32 remote ASN. Matching the local ASN selects iBGP; routeReflectorClient requires this equality. |
| `spec.peers` | Required nonempty address/hostname list, unique after trimming and without whitespace. Hostnames are passed to GoBGP, not resolved by configuration validation; protocol acceptance is checked at application. Overlapping addresses across separate BGPPeer resources are not rejected here and share one runtime address key. |
| `spec.passiveMode` | Default false initiates connections normally; true sets GoBGP passive transport and waits for inbound connections. Not a dynamic source-prefix admission rule. |
| `spec.password` | Optional inline BGP TCP authentication secret, default none; mutually exclusive with a nonempty passwordFrom source. Not a SAM enrollment token. |
| `spec.passwordFrom` | Optional secret source; exactly one of file/env when used. Omitted means no external secret. Not a status-value or Kind/name reference. Missing runtime files/environment variables fail peer reconciliation. |
| `spec.passwordFrom.file` | Router-local file containing raw password text. Exclusive with env and inline password. Trailing CR/LF is removed, but surrounding spaces are otherwise retained. |
| `spec.passwordFrom.env` | Name of a routerd process environment variable containing the secret, not the secret itself. Exclusive with file and inline password. No automatic environment-file loading. |
| `spec.passwordFrom.base64` | Default false. True decodes standard Base64 from the selected file/env value, then removes trailing CR/LF. This is encoding, not encryption. |
| `spec.ebgpMultihop` | Integer 0–255, default 0. Only values >1 enable GoBGP eBGP multihop and set its TTL; 0/1 leave multihop disabled. |
| `spec.routeReflectorClient` | Default false. True marks an iBGP route-reflector client and requires peerASN=router ASN. It also expands effective export prefixes with router import prefixes. |
| `spec.routeReflectorClusterID` | Optional IPv4-form cluster ID, passed only when routeReflectorClient is true. Empty leaves selection to GoBGP; setting it alone does not enable reflection. |
| `spec.convergenceProfile` | Optional default/fast/stable; omitted inherits BGPRouter convergenceProfile. Selects the graceful-restart default, not timers.profile; router gracefulRestart.enabled still overrides it. |
| `spec.bfd` | Optional BFD/name of a declared BFD resource. Its peer must identify this BGPPeer or one of these peer addresses; BFD Down after Up/Established triggers BGP reset. Omitted means no BFD linkage. |
| `spec.communities` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |
| `spec.communities.send` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |
| `spec.communities.accept` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |
| `spec.communities.set` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |
| `spec.communities.set.in` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |
| `spec.communities.set.out` | Decoded and validated but not used by the current BGPPeer runtime. GoBGP SendCommunity is fixed to both; configure actual import matching under importPolicy. Peer community sets do not inherit or alter router local-advertisement communities. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
