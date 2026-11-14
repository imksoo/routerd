# CloudProviderProfile

[Resource configuration reference](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Describe a provider identity and default target metadata used when planning provider actions. This declaration does not authenticate to a cloud or create resources.

## Conditions and references

- provider, nonempty capabilities, auth.mode and auth.command are required. Auth settings are validated declarations: current runtime does not execute auth.command. Provider mutations are performed by separate executor Plugins under ProviderActionPolicy.
- Consumers such as mobility capture select the profile by its metadata.name; this is not a common status-value source. subscriptionID/resourceGroup fill missing action-target values rather than overriding explicit target values.

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

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.provider` | Required azure/aws/oci/gcp; no default. Selects the provider label for planning/executor matching, not automatic credential discovery. |
| `spec.subscriptionID` | Optional target subscription ID, default empty. Copied as subscriptionId only when that target value is absent. No existence check or login. |
| `spec.resourceGroup` | Optional default resourceGroup action-target value, default empty. Does not create or validate a resource group. |
| `spec.capabilities` | Required nonempty string list with no blank entries. The current validator does not restrict it to a known capability vocabulary, and the executor policy does not treat it as its action allowlist. |
| `spec.auth` | Required authentication descriptor. Current implementation validates it without invoking the command. |
| `spec.auth.mode` | Required external-command; only accepted mode. It does not make auth.command a running service. |
| `spec.auth.command` | Required nonblank declaration; no default. Current runtime does not execute it or pass credentials obtained from it. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `provider`, `capabilities`
