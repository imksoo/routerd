# WebConsole

[Resource configuration reference](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # optional; string
    enabled: true # optional; boolean
    links: # optional; array
      - # object
        description: "<string>" # optional; string
        label: "<string>" # Required; string
        url: "<string>" # Required; string
    listenAddress: "<string>" # optional; string
    listenAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    title: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
