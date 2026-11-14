# CloudProviderProfile

[Resource configuration reference](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

Describe a provider identity and default target metadata used when planning provider actions. This declaration does not authenticate to a cloud or create resources.

## Conditions and references

- provider, nonempty capabilities, auth.mode and auth.command are required. Auth settings are validated declarations: current runtime does not execute auth.command. Provider mutations are performed by separate executor Plugins under ProviderActionPolicy.
- Consumers such as mobility capture select the profile by its metadata.name; this is not a common status-value source. subscriptionID/resourceGroup fill missing action-target values rather than overriding explicit target values.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth:
      command: <string>
      mode: ("external-command")
    capabilities:
      - <string>
    provider: ("azure" | "aws" | "oci" | "gcp")
    [ resourceGroup: <string> ]
    [ subscriptionID: <string> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.provider` | **Required** | string; enum=["azure", "aws", "oci", "gcp"] | Required azure/aws/oci/gcp; no default. Selects the provider label for planning/executor matching, not automatic credential discovery. |
| `spec.subscriptionID` | *Optional* | string | Optional target subscription ID, default empty. Copied as subscriptionId only when that target value is absent. No existence check or login. |
| `spec.resourceGroup` | *Optional* | string | Optional default resourceGroup action-target value, default empty. Does not create or validate a resource group. |
| `spec.capabilities` | **Required** | array | Required nonempty string list with no blank entries. The current validator does not restrict it to a known capability vocabulary, and the executor policy does not treat it as its action allowlist. |
| `spec.auth` | **Required** | object | Required authentication descriptor. Current implementation validates it without invoking the command. |
| `spec.auth.mode` | **Required** | string; enum=["external-command"] | Required external-command; only accepted mode. It does not make auth.command a running service. |
| `spec.auth.command` | **Required** | string | Required nonblank declaration; no default. Current runtime does not execute it or pass credentials obtained from it. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `provider`, `capabilities`
