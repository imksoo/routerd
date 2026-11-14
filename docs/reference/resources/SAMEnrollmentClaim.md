# SAMEnrollmentClaim

[Resource configuration reference](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Declare the leaf-authored identity, transport and mobility intent submitted for SAM enrollment. Expiry and revocation are RR/controller administration state, not part of the leaf join HMAC.

## Conditions and references

- A locally served claim needs an existing SAMEnrollmentPolicy. A claim referenced by SAMEnrollmentClient may name a remote policy absent locally; the RR applies admission/HMAC/authorization checks when submitted. Local shape validation is not admission success.
- When joinTokenFrom is configured, nonce, timestamp and HMAC are required, audience must match any configured audience, and nonces cannot collide within a policy. HMAC is SHA-256 over the canonical leaf payload, including directMesh:true when opted in; changing signed identity/intent requires a new signature.
- Revoked or expired claims are excluded from derived topology. Direct claims with no owned addresses may retain transport adjacency but receive no permitted mobility-route imports; a policy-wide address range is not an unrestricted direct /32 authorization.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # optional; object
      asn: 1 # optional; integer; minimum=1
      routerID: "<string>" # optional; string
    directMesh: true # optional; boolean
    endpoint: "<string>" # optional; string
    expiresAt: "<string>" # optional; string
    joinAudience: "<string>" # optional; string
    joinHMAC: "<string>" # optional; string
    joinNonce: "<string>" # optional; string
    joinTimestamp: "<string>" # optional; string
    leafID: "<string>" # Required; string
    mobility: # optional; object
      ownedAddresses: # optional; array
        - "<string>" # string
    policyRef: "<string>" # Required; string
    revoked: true # optional; boolean
    rrSetRef: "<string>" # optional; string
    tunnelAddress: "<string>" # Required; string
    wireGuard: # optional; object
      allowedIPs: # optional; array
        - "<string>" # string
      endpoint: "<string>" # optional; string
      persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
      publicKey: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.policyRef` | Required SAMEnrollmentPolicy/name. Must exist locally unless this is a leaf claim referenced by SAMEnrollmentClient for remote admission. |
| `spec.rrSetRef` | Optional in shape, but SAMEnrollmentClient requires SAMRRSet/name at runtime. Must equal policy.rrSetRef when that policy output is configured. Names runtime RR topology, not a static resource to author. |
| `spec.leafID` | Required stable node identity, matched against allowedLeafIDs.pattern and unique within the policy. Used as generated nodeRef. |
| `spec.joinAudience` | Optional signed audience; required to equal the nonempty policy audience when join-token authentication is active. |
| `spec.joinNonce` | Optional signed nonce; required with joinHMAC or a join-token policy. Same-policy duplicate nonce use is rejected. No automatic generation by this YAML field. |
| `spec.joinTimestamp` | Optional signed RFC3339/RFC3339Nano timestamp. Required with join-token policy, and when explicit expiresAt is checked against policy TTL. A configured TTL also expires a claim based on this timestamp when present. |
| `spec.joinHMAC` | Optional hexadecimal HMAC-SHA256 signature of the canonical claim, mandatory for a join-token policy. It is a signature, not the join secret or ControlAPI Bearer token. |
| `spec.tunnelAddress` | Required bare IPv4 address or /32 CIDR; authorized by policy.tunnelAddressPrefixes and unique in that policy. Not an automatically allocated address. |
| `spec.endpoint` | Optional outer tunnel IP endpoint. If policy.endpointPrefixes is nonempty, it must lie inside that IPv4 allowlist. Separate from the WireGuard UDP endpoint. |
| `spec.wireGuard` | Optional public WireGuard material. Any nonempty subfield requires publicKey. Does not contain a private key. |
| `spec.wireGuard.publicKey` | Public key required when WireGuard data is present; nonempty keys must be unique within the policy. No key auto-generation here. |
| `spec.wireGuard.endpoint` | Optional host:port (1–65535) for WireGuard UDP. Prefix admission requires a parseable endpoint IP and uses wireGuard.endpointPrefixes or policy.endpointPrefixes fallback; do not assume a hostname passes admission. |
| `spec.wireGuard.allowedIPs` | Optional additional IPv4 CIDRs, each contained by policy.wireGuard.allowedExtraIPPrefixes. Empty grants no extras; generated peer independently includes tunnelAddress /32. |
| `spec.wireGuard.persistentKeepalive` | Integer seconds 0–65535, default 0 uses policy WireGuard keepalive in generated topology. Nonzero overrides that default. |
| `spec.directMesh` | Default false keeps this leaf on RR peers only. True requests participation in the policy direct peer group and is signed; it does not bypass policy enablement or owned-/32 admission. |
| `spec.mobility` | Optional declared mobility ownership, default no owned addresses. Does not itself discover clients. |
| `spec.mobility.ownedAddresses` | Bare IPv4 addresses or /32s authorized by policy mobilityPrefixes/MobilityPool refs. Duplicates across active same-policy claims are rejected. Direct BGP imports are narrowed to exactly the admitted owned /32s. |
| `spec.bgp` | Optional signed BGP identity intent. It is not a BGPRouter resource reference. |
| `spec.bgp.asn` | Optional uint32 ASN carried in the signed claim, decoded default 0. Schema requires >=1 when present, but shape validation does not otherwise require a nonzero ASN. Not an override of policy transport peerASN. |
| `spec.bgp.routerID` | Optional IPv4 BGP identifier, no derived default. Nonempty IDs must be unique within a policy; does not create a BGPRouter. |
| `spec.expiresAt` | Optional RR/admin-owned RFC3339 expiry, excluded from join HMAC. Admission normally assigns a lease expiry (policy TTL, otherwise 5m). Explicit expiry cannot exceed joinTimestamp+policy TTL when both are supplied. Expired values exclude the claim. |
| `spec.revoked` | Default false; true is RR/admin revocation state, excluded from join HMAC, and prevents topology admission. A leaf cannot use a replayed signature to clear stored revocation. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
