# DynamicOverridePolicy

[Resource configuration reference](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

Authorizes selected dynamic sources to mask exact startup resources while the dynamic part is active. It does not rewrite the saved startup configuration.

## Conditions and references

- Policies match the exact source plus operation plus apiVersion/kind/name target. With no matching allow entry, masking is denied. Multiple allowed active masks on one target are combined until the latest expiry.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow:
      -
        operations:
          - <string>
        source: <string>
        targets:
          -
            apiVersion: <string>
            kind: <string>
            name: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.allow` | **Required** | array | Allow-rule list. Empty has no effect and grants no permission. |
| `spec.allow[].source` | **Required** | string | Required exact DynamicConfigPart.spec.source identity, not necessarily a Plugin or DynamicConfigSource resource name. No wildcard/default. |
| `spec.allow[].operations` | **Required** | array | Allowed operations; only mask is accepted. Empty grants no operation. |
| `spec.allow[].targets` | **Required** | array | Exact startup-resource identities eligible for masking. Empty grants no targets. |
| `spec.allow[].targets[].apiVersion` | **Required** | string | Required API group/version of the exact target; no default. |
| `spec.allow[].targets[].kind` | **Required** | string | Required target kind; no wildcard expansion. |
| `spec.allow[].targets[].name` | **Required** | string | Required exact target metadata.name; no wildcard expansion. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
