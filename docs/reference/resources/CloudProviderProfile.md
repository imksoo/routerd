# CloudProviderProfile

[Resource configuration reference](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # Required; object
      command: "<string>" # optional; string
      mode: "external-command" # Required; string; enum=["external-command"]
    capabilities: # Required; array
      - "<string>" # string
    provider: "azure" # Required; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # optional; string
    subscriptionID: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
