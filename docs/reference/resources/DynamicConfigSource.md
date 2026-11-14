# DynamicConfigSource

[Resource configuration reference](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

Declares a plugin-backed dynamic configuration source. The current implementation decodes and validates this kind, but contains no runtime consumer of DynamicConfigSourceSpec; declaring it does not invoke the plugin.

## Conditions and references

- pluginRef is a name declaration, not a validated existence check. TTL is required here, but the actual plugin-result merge uses PluginResult.status.ttl independently. Do not rely on this resource to override a result TTL.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # optional; object
      conflict: "" # optional; string; enum=["", "reject"]
    pluginRef: "<string>" # Required; string
    triggers: # optional; array
      - # object
        every: "<string>" # optional; string
        topic: "<string>" # optional; string
        type: "interval" # Required; string; enum=["interval", "event"]
    ttl: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.pluginRef` | Intended Plugin metadata.name; required nonblank string, no default. Do not prefix Plugin/; the declaration has no runtime resolver. |
| `spec.ttl` | Required positive Go duration, with no default. This declaration is validated but is not applied to plugin execution or result expiry by a runtime consumer. |
| `spec.mergePolicy` | Optional policy object; no runtime consumer for this source declaration. |
| `spec.mergePolicy.conflict` | Only empty or reject passes validation. Empty is accepted; neither value enables replacement of existing resources through this declaration. |
| `spec.triggers` | Declared trigger list, default empty. The current runtime only validates/displays this list; see the resource rules. |
| `spec.triggers[].type` | Required trigger category: interval or event. There is no default. |
| `spec.triggers[].every` | Required for interval; positive Go duration. For event, this field has no scheduling effect. No default. |
| `spec.triggers[].topic` | Event-topic declaration; optional and empty by default. The validator does not require it even for event. It is not a resource name. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)
