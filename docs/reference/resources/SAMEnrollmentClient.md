# SAMEnrollmentClient

[Resource configuration reference](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Submit a local enrollment claim to bootstrap RR control APIs, fetch admitted topology and persist it as dynamic SAMRRSet/optional direct peer-group state for existing controllers. No cloud infrastructure is created.

## Conditions and references

- claimRef must resolve to a local SAMEnrollmentClaim, whose rrSetRef must name SAMRRSet/name for runtime use. A leaf claim may refer to a remote policy without duplicating that SAMEnrollmentPolicy locally.
- At least one bootstrapEndpoints entry or rrSocket is required; they are not mutually exclusive. The Unix socket client precedes listed HTTP(S) endpoints. Ordinary admission can use a successful RR response, while direct topology requires agreement across configured endpoints and keeps the RR fallback when direct admission is unsafe.
- Direct-mesh opt-out removes cached direct topology before contacting an unreachable RR. Client retry/renewal state is persisted; refreshing topology does not imply that tunnels or BGP sessions are already operational.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # optional; array
      - "<string>" # string
    claimRef: "<string>" # Required; string
    controlAPITLS: # optional; object
      caFile: "<string>" # optional; string
      certFile: "<string>" # optional; string
      insecureSkipVerify: true # optional; boolean
      keyFile: "<string>" # optional; string
      serverName: "<string>" # optional; string
    controlAPITokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    retryBackoff: # optional; object
      max: "<string>" # optional; string
      min: "<string>" # optional; string
    rrSocket: "<string>" # optional; string
    stateTTLRefreshBefore: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.claimRef` | Required SAMEnrollmentClaim/name declared in this Router. No status field selector. |
| `spec.bootstrapEndpoints` | Optional ordered absolute HTTP(S) ControlAPI URLs with hosts; at least one required unless rrSocket is supplied. No automatic discovery. Each enrollment request currently uses a 10s request context. |
| `spec.controlAPITokenFrom` | Optional Bearer-token source used only for HTTP(S) clients; omitted means no Bearer header. Exactly one file/env if supplied. Separate from the claim join HMAC credential. |
| `spec.controlAPITokenFrom.file` | Router-local raw token file, exclusive with env. Whitespace is trimmed; missing/unreadable/empty values fail when HTTP endpoints are configured. |
| `spec.controlAPITokenFrom.env` | Name of a routerd process environment variable holding the token, exclusive with file; not an environment-file path. |
| `spec.controlAPITokenFrom.base64` | Default false; true applies standard Base64 decoding and trims the decoded token. It is not encryption. |
| `spec.controlAPITLS` | Optional HTTPS client settings, unused for Unix sockets. Setting these does not turn an http URL into https. |
| `spec.controlAPITLS.caFile` | Optional PEM CA file. When supplied it replaces the client root pool with certificates in this file; omission uses the normal trust configuration. |
| `spec.controlAPITLS.certFile` | Optional client certificate file; must be paired with keyFile. No default certificate or generated credentials. |
| `spec.controlAPITLS.keyFile` | Optional client private-key file, required together with certFile. |
| `spec.controlAPITLS.serverName` | Optional TLS SNI/verification name override; empty uses URL host behavior. It does not change endpoint routing. |
| `spec.controlAPITLS.insecureSkipVerify` | Default false. True disables server certificate-chain/hostname verification; no effect on a plain HTTP URL. |
| `spec.rrSocket` | Optional absolute local RR Unix-socket path; may accompany HTTP(S) endpoints. Unix client does not use Bearer token or TLS settings. |
| `spec.stateTTLRefreshBefore` | Renewal lead-time Go duration, default 10m. If >= an observed positive RR lease duration, runtime uses half that lease to avoid immediate renewal loops. Validator checks parseability, not positivity; use a positive value. |
| `spec.retryBackoff` | Optional exponential retry settings; failures double min up to max. Direct-topology recovery has an additional short convergence retry path. |
| `spec.retryBackoff.min` | Go duration, default 10s. Parseability is checked, but zero/negative are accepted; choose a positive value to avoid immediate/past-due retry scheduling. |
| `spec.retryBackoff.max` | Go duration, default 15m. When both min and max are explicit, max must be >=min. Use positive values; defaults are not included in that comparison during validation. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go) · [pkg/controller/mobility/core_helpers.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/core_helpers.go) · [pkg/controlapi/client.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/client.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `claimRef`, `observedRRSet`, `observedDirectPeerGroup`, `lastAttempt`, `lastSuccess`, `nextAttempt`, `backoff`, `failureCount`, `directTopologyPending`, `reason`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
