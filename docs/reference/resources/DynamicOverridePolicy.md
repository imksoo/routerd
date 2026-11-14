# DynamicOverridePolicy

[Resource configuration reference](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

Authorizes selected dynamic sources to mask exact startup resources while the dynamic part is active. It does not rewrite the saved startup configuration.

## Conditions and references

- Policies match the exact source plus operation plus apiVersion/kind/name target. With no matching allow entry, masking is denied. Multiple allowed active masks on one target are combined until the latest expiry.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # Required; array
      - # object
        operations: # Required; array
          - "<string>" # string
        source: "<string>" # Required; string
        targets: # Required; array
          - # object
            apiVersion: "<string>" # Required; string
            kind: "<string>" # Required; string
            name: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.allow` | Allow-rule list. Empty has no effect and grants no permission. |
| `spec.allow[].source` | Required exact DynamicConfigPart.spec.source identity, not necessarily a Plugin or DynamicConfigSource resource name. No wildcard/default. |
| `spec.allow[].operations` | Allowed operations; only mask is accepted. Empty grants no operation. |
| `spec.allow[].targets` | Exact startup-resource identities eligible for masking. Empty grants no targets. |
| `spec.allow[].targets[].apiVersion` | Required API group/version of the exact target; no default. |
| `spec.allow[].targets[].kind` | Required target kind; no wildcard expansion. |
| `spec.allow[].targets[].name` | Required exact target metadata.name; no wildcard expansion. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
