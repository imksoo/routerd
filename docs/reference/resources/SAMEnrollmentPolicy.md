# SAMEnrollmentPolicy

[Resource configuration reference](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
