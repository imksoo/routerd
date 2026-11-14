# ControlAPI

[Resource configuration reference](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # optional; array
      - "<string>" # string
    enabled: true # optional; boolean
    listenAddress: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    tls: # optional; object
      certFile: "<string>" # optional; string
      clientCAFile: "<string>" # optional; string
      keyFile: "<string>" # optional; string
    tokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
