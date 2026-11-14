# SAMEnrollmentClaim

[Resource configuration reference](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Declare the leaf-authored identity, transport and mobility intent submitted for SAM enrollment. Expiry and revocation are RR/controller administration state, not part of the leaf join HMAC.

## Conditions and references

- A locally served claim needs an existing SAMEnrollmentPolicy. A claim referenced by SAMEnrollmentClient may name a remote policy absent locally; the RR applies admission/HMAC/authorization checks when submitted. Local shape validation is not admission success.
- When joinTokenFrom is configured, nonce, timestamp and HMAC are required, audience must match any configured audience, and nonces cannot collide within a policy. HMAC is SHA-256 over the canonical leaf payload, including directMesh:true when opted in; changing signed identity/intent requires a new signature.
- Revoked or expired claims are excluded from derived topology. Direct claims with no owned addresses may retain transport adjacency but receive no permitted mobility-route imports; a policy-wide address range is not an unrestricted direct /32 authorization.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    [ bgp:
        [ asn: <integer> ]
        [ routerID: <string> ]
    ]
    [ directMesh: <boolean> ]
    [ endpoint: <string> ]
    [ expiresAt: <string> ]
    [ joinAudience: <string> ]
    [ joinHMAC: <string> ]
    [ joinNonce: <string> ]
    [ joinTimestamp: <string> ]
    leafID: <string>
    [ mobility:
        [ ownedAddresses:
            - <string>
        ]
    ]
    policyRef: <string>
    [ revoked: <boolean> ]
    [ rrSetRef: <string> ]
    tunnelAddress: <string>
    [ wireGuard:
        [ allowedIPs:
            - <string>
        ]
        [ endpoint: <string> ]
        [ persistentKeepalive: <integer> ]
        publicKey: <string>
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.policyRef` | **Required** | string | Required SAMEnrollmentPolicy/name. Must exist locally unless this is a leaf claim referenced by SAMEnrollmentClient for remote admission. |
| `spec.rrSetRef` | *Optional* | string | Optional in shape, but SAMEnrollmentClient requires SAMRRSet/name at runtime. Must equal policy.rrSetRef when that policy output is configured. Names runtime RR topology, not a static resource to author. |
| `spec.leafID` | **Required** | string | Required stable node identity, matched against allowedLeafIDs.pattern and unique within the policy. Used as generated nodeRef. |
| `spec.joinAudience` | **Conditionally required** | string | Optional signed audience; required to equal the nonempty policy audience when join-token authentication is active. |
| `spec.joinNonce` | **Conditionally required** | string | Optional signed nonce; required with joinHMAC or a join-token policy. Same-policy duplicate nonce use is rejected. No automatic generation by this YAML field. |
| `spec.joinTimestamp` | **Conditionally required** | string | Optional signed RFC3339/RFC3339Nano timestamp. Required with join-token policy, and when explicit expiresAt is checked against policy TTL. A configured TTL also expires a claim based on this timestamp when present. |
| `spec.joinHMAC` | **Conditionally required** | string | Optional hexadecimal HMAC-SHA256 signature of the canonical claim, mandatory for a join-token policy. It is a signature, not the join secret or ControlAPI Bearer token. |
| `spec.tunnelAddress` | **Required** | string | Required bare IPv4 address or /32 CIDR; authorized by policy.tunnelAddressPrefixes and unique in that policy. Not an automatically allocated address. |
| `spec.endpoint` | *Optional* | string | Optional outer tunnel IP endpoint. If policy.endpointPrefixes is nonempty, it must lie inside that IPv4 allowlist. Separate from the WireGuard UDP endpoint. |
| `spec.wireGuard` | *Optional* | object | Optional public WireGuard material. Any nonempty subfield requires publicKey. Does not contain a private key. |
| `spec.wireGuard.publicKey` | **Conditionally required** | string | Public key required when WireGuard data is present; nonempty keys must be unique within the policy. No key auto-generation here. |
| `spec.wireGuard.endpoint` | *Optional* | string | Optional host:port (1–65535) for WireGuard UDP. Prefix admission requires a parseable endpoint IP and uses wireGuard.endpointPrefixes or policy.endpointPrefixes fallback; do not assume a hostname passes admission. |
| `spec.wireGuard.allowedIPs` | *Optional* | array | Optional additional IPv4 CIDRs, each contained by policy.wireGuard.allowedExtraIPPrefixes. Empty grants no extras; generated peer independently includes tunnelAddress /32. |
| `spec.wireGuard.persistentKeepalive` | *Optional* | integer; minimum=0; maximum=65535 | Integer seconds 0–65535, default 0 uses policy WireGuard keepalive in generated topology. Nonzero overrides that default. |
| `spec.directMesh` | *Optional* | boolean | Default false keeps this leaf on RR peers only. True requests participation in the policy direct peer group and is signed; it does not bypass policy enablement or owned-/32 admission. |
| `spec.mobility` | *Optional* | object | Optional declared mobility ownership, default no owned addresses. Does not itself discover clients. |
| `spec.mobility.ownedAddresses` | *Optional* | array | Bare IPv4 addresses or /32s authorized by policy mobilityPrefixes/MobilityPool refs. Duplicates across active same-policy claims are rejected. Direct BGP imports are narrowed to exactly the admitted owned /32s. |
| `spec.bgp` | *Optional* | object | Optional signed BGP identity intent. It is not a BGPRouter resource reference. |
| `spec.bgp.asn` | *Optional* | integer; minimum=1 | Optional uint32 ASN carried in the signed claim, decoded default 0. Schema requires >=1 when present, but shape validation does not otherwise require a nonzero ASN. Not an override of policy transport peerASN. |
| `spec.bgp.routerID` | *Optional* | string | Optional IPv4 BGP identifier, no derived default. Nonempty IDs must be unique within a policy; does not create a BGPRouter. |
| `spec.expiresAt` | *Optional* | string | Optional RR/admin-owned RFC3339 expiry, excluded from join HMAC. Admission normally assigns a lease expiry (policy TTL, otherwise 5m). Explicit expiry cannot exceed joinTimestamp+policy TTL when both are supplied. Expired values exclude the claim. |
| `spec.revoked` | *Optional* | boolean | Default false; true is RR/admin revocation state, excluded from join HMAC, and prevents topology admission. A leaf cannot use a replayed signature to clear stored revocation. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `leafID`, `rrSetRef`, `tunnelAddress`, `endpoint`, `revoked`, `expiresAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
