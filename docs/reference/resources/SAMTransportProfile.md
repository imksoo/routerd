# SAMTransportProfile

[Resource configuration reference](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Derive SAM per-node tunnels, endpoint /32 routes and optional BGP/BFD peers from shared topology or admitted RR/direct snapshots.

## Conditions and references

- Direct requires a final direct SAMPeerGroup after mandatory SAMRRSet, pair-stable addressing, BGP generation, routeReflectorClient:false, IPv4 import prefixes with min=max=32, positive RR LOCAL_PREF below direct preference, and local node identity community in BGPRouter.communities.set.out. Unusable direct data retains RR paths.
- Generated RR-client/direct import enforces /32 bounds and peer-identity community, forbidding other known node identities. Direct import prefixes become exactly the admitted leaf owned /32s, with deny-all when empty and nextHopRewrite forced to peer-address. RR fallback next hop is also normalized to peer-address in direct profiles.
- An RR-client profile with no local MobilityPool requires explicit IPv4 import prefixes and bounds omitted or both 32. FreeBSD derives gifN/greN plus explicit point-to-point peerAddress; Linux uses compact tunnel names. Successful derivation is not tunnel/BGP connectivity proof.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # optional; string; enum=["", "edge-index", "pair-stable"]
    bgp: # Required; object
      bfd: # optional; object
        detectMultiplier: 1 # optional; integer; minimum=1; maximum=50
        enabled: true # optional; boolean
        interface: "<string>" # optional; string
        minRx: "<string>" # optional; string
        minTx: "<string>" # optional; string
        profile: "" # optional; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # optional; integer
      ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
      exportPolicy: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
      generatePeers: true # optional; boolean
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
      peerASN: 1 # Required; integer; minimum=1
      routeReflectorClient: true # optional; boolean
      routeReflectorClusterID: "<string>" # optional; string
      routerRef: "<string>" # Required; string
      timers: # optional; object
        profile: "" # optional; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # optional; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # optional; integer; minimum=1; maximum=65535
    encapSport: 1 # optional; integer; minimum=1; maximum=65535
    encryption: "" # optional; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # Required; string
    localEndpoint: "<string>" # optional; string
    localEndpointFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    mode: "ipip" # Required; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # optional; array
      - # object
        direct: true # optional; boolean
        nodeRefs: # optional; array
          - "<string>" # string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    publishPeerGroup: true # optional; boolean
    selfNodeRef: "<string>" # Required; string
    underlayInterface: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.selfNodeRef` | Required stable local nodeRef; no hostname/router-ID inference. |
| `spec.mode` | Required ipip/gre/fou/gue. FreeBSD supports only ipip/gif and gre; fou/gue require Linux. Derived tunnels are unencrypted by themselves. |
| `spec.encryption` | none/omitted or wireguard. wireguard prefers node.wireGuard.transportEndpoint; does not generate private keys or a local WireGuardInterface. Configure the WireGuard underlay separately. |
| `spec.innerPrefix` | Required IPv4 CIDR with prefix length <=31; subdivided into /31 edges, with enough slots for topology. Pair-stable collisions are errors rather than silently moving endpoints. |
| `spec.addressingMode` | edge-index by default: /31 slots follow sorted topology edges and can change when topology changes. pair-stable hashes innerPrefix and unordered node pair into a fixed slot; required for direct sources. |
| `spec.underlayInterface` | Required declared logical interface. Used by generated tunnels and remote endpoint /32 device routes; generated tunnels assert trustedUnderlay:true. |
| `spec.localEndpoint` | Local outer IPv4 literal; exactly one of localEndpoint/localEndpointFrom is required. No interface-address auto-selection. |
| `spec.localEndpointFrom` | Status source instead of literal localEndpoint. Common resource/field rules apply; unresolved endpoint leaves dependent transport pending. |
| `spec.localEndpointFrom.resource` | Required existing Kind/name when this source is used. |
| `spec.localEndpointFrom.field` | Required explicit provided field, for example currentAddress. |
| `spec.localEndpointFrom.optional` | Default false; does not waive reference validation or supply a usable endpoint when unresolved. |
| `spec.encapSport` | UDP source port 1–65535 required for fou/gue, forbidden for ipip/gre (omit/0). |
| `spec.encapDport` | UDP destination port 1–65535 required for fou/gue, forbidden for ipip/gre. No implicit port. |
| `spec.peersFrom` | Ordered topology sources; at least one required unless publishPeerGroup:true. Self is excluded as a peer but retained in shared topology; later duplicate nodes may replace earlier source material. |
| `spec.peersFrom[].resource` | Required SAMNodeSet/name, SAMPeerGroup/name, SAMEnrollmentPolicy/name or SAMRRSet/name. NodeSet is static; runtime sources supply admitted/shared snapshots. |
| `spec.peersFrom[].optional` | Default false. Missing nonoptional source is pending. Direct source unavailability/incompatibility does not remove usable RR fallback peers. |
| `spec.peersFrom[].direct` | Default false. True only for the final SAMPeerGroup source following a mandatory SAMRRSet; see direct-mesh constraints below. |
| `spec.peersFrom[].nodeRefs` | Optional unique nonempty nodeRef selectors; omitted selects all eligible source nodes. Narrows local adjacency, not shared authoritative topology. |
| `spec.publishPeerGroup` | Default false. True publishes the local endpoint as runtime SAMPeerGroup/<profile metadata.name> and permits empty peersFrom. Does not publish private credentials. |
| `spec.bgp` | Required generated-peer settings; referenced BGPRouter must already exist. |
| `spec.bgp.generatePeers` | Default true. False generates transport without BGP/BFD; prohibited for direct sources. |
| `spec.bgp.timersPreset` | Optional default/fast/slow shorthand; lower precedence than bgp.timers.profile. If both empty, generated peers inherit BGPRouter.timers. |
| `spec.bgp.directLocalPreference` | Direct-peer uint32 LOCAL_PREF; omitted/0 defaults to 200. Must exceed positive RR bgp.importPolicy.localPreference when direct is configured. |
| `spec.bgp.bfd` | Optional generated BFD settings, used only if generatePeers and bfd.enabled are true. |
| `spec.bgp.bfd.enabled` | Default false. True derives BFD resources linked to generated BGPPeers; requires FRR bfdd/vtysh. |
| `spec.bgp.routerRef` | Required BGPRouter/name referencing a declared router in the same Router configuration. Plain names are not accepted. |
| `spec.bgp.peerASN` | Required nonzero uint32 remote ASN. Matching the local ASN selects iBGP; routeReflectorClient requires this equality. |
| `spec.bgp.ebgpMultihop` | Integer 0–255, default 0. Only values >1 enable GoBGP eBGP multihop and set its TTL; 0/1 leave multihop disabled. |
| `spec.bgp.routeReflectorClient` | Default false. True marks an iBGP route-reflector client and requires peerASN=router ASN. It also expands effective export prefixes with router import prefixes. |
| `spec.bgp.routeReflectorClusterID` | Optional IPv4-form cluster ID, passed only when routeReflectorClient is true. Empty leaves selection to GoBGP; setting it alone does not enable reflection. |
| `spec.bgp.convergenceProfile` | Optional default/fast/stable; omitted inherits BGPRouter convergenceProfile. Selects the graceful-restart default, not timers.profile; router gracefulRestart.enabled still overrides it. |
| `spec.bgp.importPolicy` | Optional import policy. A peer with no nonempty prefixes, community constraints, next-hop rewrite or localPreference inherits the router policy as a whole; prefix-length bounds alone do not count as an override. |
| `spec.bgp.importPolicy.allowedPrefixes` | Optional IPv4/IPv6 CIDR allowlist. Import matches the prefix and its permitted more-specific lengths; masked duplicate prefixes are rejected. Empty adds no prefix match condition to an otherwise configured import policy. |
| `spec.bgp.importPolicy.allowedPrefixLengthMin` | Integer 0–128; 0/omitted uses each allowlist prefix length. A positive value must be >= each listed prefix length, <= address-family maximum and <= a positive maximum. |
| `spec.bgp.importPolicy.allowedPrefixLengthMax` | Integer 0–128; 0/omitted uses 32 for IPv4 or 128 for IPv6. A positive value must cover every listed prefix length and not exceed its family maximum. Dynamic leaf admission requires min=max=32. |
| `spec.bgp.importPolicy.requiredCommunities` | Optional community list; an accepted path must contain ALL listed communities. Empty adds no required-community condition. |
| `spec.bgp.importPolicy.forbiddenCommunities` | Optional community list; ANY matching community rejects the path before the accept rule. Empty adds no prohibition. |
| `spec.bgp.importPolicy.nextHopRewrite` | peer-address by default; unchanged preserves the advertised next hop. Applied on accepted paths before selection/FIB projection. Setting only this field on a peer creates a peer-specific import policy instead of inheriting the router allowlist. |
| `spec.bgp.importPolicy.localPreference` | Optional uint32 LOCAL_PREF for accepted paths before best-path selection. Default 0 leaves the BGP implementation default unchanged. |
| `spec.bgp.exportPolicy` | Optional export prefix filter. Per-peer prefixes are combined with router static and dynamic advertisements; route-reflector clients also receive router import prefixes. It is not a restrictive replacement for all router-derived export prefixes. |
| `spec.bgp.exportPolicy.allowedPrefixes` | Optional CIDR list, default empty. Effective nonempty export lists allow the listed prefixes and their more-specifics (through /32 or /128), rejecting unmatched routes. An effective empty list installs no export filter. |
| `spec.bgp.timers` | Generated-peer timers; priority is profile timers.profile > timersPreset > referenced BGPRouter.timers. |
| `spec.bgp.timers.profile` | default/omitted: keepalive 30s, hold 90s, connectRetry 10s; fast: 3s/9s/1s; slow: 60s/180s/30s. Low-level keepalive/holdTime/connectRetry YAML fields are unsupported. |
| `spec.bgp.bfd.interface` | Optional single-hop interface. Prefer Interface/name so the controller resolves the declared Interface.ifname; a plain name is validated as a declared logical interface but is passed literally to FRR. Omitted uses multihop. |
| `spec.bgp.bfd.profile` | fast/omitted: minRx=minTx=300ms, multiplier=3; normal: 500ms/500ms/3; slow: 1000ms/1000ms/5. Explicit individual settings override these defaults. |
| `spec.bgp.bfd.minRx` | Optional Go duration, 50ms–60000ms, overriding profile receive interval. Converted to integer milliseconds; omitted uses profile. This is a BFD protocol timer, not a router reboot deadline. |
| `spec.bgp.bfd.minTx` | Optional Go duration, 50ms–60000ms, overriding profile transmit interval; emitted as integer milliseconds. |
| `spec.bgp.bfd.detectMultiplier` | Optional integer 1–50. Decoded 0/omission selects profile default (3, or 5 for slow). Multiplies the negotiated detection interval; not a count of routerd reconcile attempts. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/transport_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport_support.go) · [pkg/mobilityconfig/sam_transport_slots.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/sam_transport_slots.go) · [pkg/mobilityconfig/transport_mesh.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/transport_mesh.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `reason`, `peersFrom`, `pendingSources`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
