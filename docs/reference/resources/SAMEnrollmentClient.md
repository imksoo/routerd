# SAMEnrollmentClient

[Resource configuration reference](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
