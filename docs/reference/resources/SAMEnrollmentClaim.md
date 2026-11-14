# SAMEnrollmentClaim

[Resource configuration reference](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
