# SAMEnrollmentPolicy

[Resource configuration reference](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Define RR-side admission boundaries for SAM leaf identity, transport addresses, optional join authentication and authorized mobility ownership. Leaves consume the admitted topology instead of copying this policy.

## Conditions and references

- RR output names are unique per policy; rrSetRef/rrNodeSetRef form a pair. directMesh additionally requires pair-stable transport addressing and an RR fallback.
- Claim IDs, tunnel addresses, nonempty WireGuard keys, owned addresses and nonempty BGP router IDs are checked for collisions within the same policy. Endpoint, WireGuard extra-prefix and mobility authorization checks are separate.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # optional; object
      pattern: "<string>" # optional; string
    directMesh: # optional; object
      peerGroupRef: "<string>" # optional; string
    endpointPrefixes: # optional; array
      - "<string>" # string
    joinAudience: "<string>" # optional; string
    joinTokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    mobilityPoolRefs: # optional; array
      - "<string>" # string
    mobilityPrefixes: # optional; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # optional; string
    rrSetRef: "<string>" # optional; string
    transportProfileRef: "<string>" # Required; string
    ttl: "<string>" # optional; string
    tunnelAddressPrefixes: # Required; array
      - "<string>" # string
    wireGuard: # optional; object
      allowedExtraIPPrefixes: # optional; array
        - "<string>" # string
      endpointPrefixes: # optional; array
        - "<string>" # string
      interface: "<string>" # Required; string
      persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.transportProfileRef` | Required existing SAMTransportProfile/name in this Router. Binds admitted leaves to that transport profile; not a status source. |
| `spec.rrSetRef` | Optional runtime output name SAMRRSet/name. Must be paired with rrNodeSetRef and unique across enrollment policies. Names a generated snapshot, not a statically authored SAMRRSet. Required for direct mesh and leaf-client topology fetching. |
| `spec.rrNodeSetRef` | Existing SAMNodeSet/name supplying RR identities; required exactly when rrSetRef is supplied. Every node in that NodeSet must have routeReflector:true. No duplicate RR registry in the policy. |
| `spec.joinTokenFrom` | Optional shared HMAC secret source. If absent, no join HMAC is required; the protected ControlAPI becomes the trust boundary. If present, exactly one file/env and valid claim nonce/timestamp/HMAC are required. Separate from ControlAPI Bearer authentication. |
| `spec.joinTokenFrom.file` | Router-local raw HMAC secret file, exclusive with env. Surrounding whitespace is removed; unavailable secrets fail actual submission even where offline validation cannot read them. |
| `spec.joinTokenFrom.env` | Name of routerd process environment variable holding the HMAC secret; exclusive with file. No automatic environment-file loading. |
| `spec.joinTokenFrom.base64` | Default false; true standard-Base64-decodes the trimmed secret source. Decoded bytes become the HMAC key; Base64 is not encryption. |
| `spec.joinAudience` | Optional expected claim audience when joinTokenFrom is configured; nonempty must equal claim.joinAudience. No default audience. |
| `spec.allowedLeafIDs` | Optional leaf-identity restriction object; omission imposes no regex restriction. |
| `spec.allowedLeafIDs.pattern` | Go regular expression matched against leafID after trimming. Empty accepts any otherwise valid ID; patterns are not implicitly anchored, so use ^...$ for a whole-ID match. |
| `spec.tunnelAddressPrefixes` | Required nonempty IPv4 CIDR list authorizing claim.tunnelAddress /32 identities. No address auto-allocation is performed by this field. |
| `spec.endpointPrefixes` | Optional IPv4 CIDR allowlist for supplied outer endpoint addresses. Empty imposes no endpoint-prefix restriction. Used as WireGuard endpoint fallback allowlist when wireGuard.endpointPrefixes is empty. |
| `spec.wireGuard` | Optional admitted-leaf WireGuard controls. interface is required when other operational WireGuard settings are used; endpoint-prefix validation alone does not supply a WireGuard device. |
| `spec.wireGuard.interface` | Optional existing WireGuardInterface metadata.name, not Kind/name. Limits which local WireGuard interface imports this policy; no default creation. |
| `spec.wireGuard.endpointPrefixes` | Optional IPv4 CIDR allowlist for claim WireGuard outer endpoint IPs. If empty, endpointPrefixes applies; if both empty, no such prefix restriction. |
| `spec.wireGuard.allowedExtraIPPrefixes` | Optional IPv4 CIDRs authorizing additional claim.wireGuard.allowedIPs. Tunnel /32 is added independently to generated peers. Empty authorizes no additional prefixes. |
| `spec.wireGuard.persistentKeepalive` | Seconds 0–65535, default 0. Used for generated WireGuard peers when a claim leaves its keepalive at 0; explicit claim nonzero wins. |
| `spec.directMesh` | Optional policy-scoped direct leaf mesh output. Does not eliminate the RR fallback. |
| `spec.directMesh.peerGroupRef` | SAMPeerGroup/name runtime output; empty disables direct mesh. Requires rrSetRef and a referenced SAMTransportProfile with addressingMode: pair-stable; must be unique across policies. Not a statically authored SAMPeerGroup. |
| `spec.mobilityPoolRefs` | Optional existing MobilityPool/name list supplying authorized ownership prefixes. Combined with mobilityPrefixes, not a request to create pools or cloud resources. |
| `spec.mobilityPrefixes` | Optional additional CIDRs authorizing claimed owned IPv4 /32 addresses. Combined with referenced pools; empty plus no pool refs authorizes no owned addresses. |
| `spec.ttl` | Optional Go duration. Actual submitted admission defaults to a 5m lease if omitted. A configured TTL also bounds claim lifetime from joinTimestamp and any explicit expiresAt; invalid/nonpositive lifetimes expire fail-closed at runtime even though shape validation only parses duration. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `acceptedClaims`, `skippedClaims`, `leafIDs`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
