# DynamicConfigSource

[Resource configuration reference](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

Declares a plugin-backed dynamic configuration source. The current implementation decodes and validates this kind, but contains no runtime consumer of DynamicConfigSourceSpec; declaring it does not invoke the plugin.

## Conditions and references

- pluginRef is a name declaration, not a validated existence check. TTL is required here, but the actual plugin-result merge uses PluginResult.status.ttl independently. Do not rely on this resource to override a result TTL.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    [ mergePolicy:
        [ conflict: ("" | "reject") ]
    ]
    pluginRef: <string>
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
    ttl: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.pluginRef` | **Required** | string | Intended Plugin metadata.name; required nonblank string, no default. Do not prefix Plugin/; the declaration has no runtime resolver. |
| `spec.ttl` | **Required** | string | Required positive Go duration, with no default. This declaration is validated but is not applied to plugin execution or result expiry by a runtime consumer. |
| `spec.mergePolicy` | *Optional* | object | Optional policy object; no runtime consumer for this source declaration. |
| `spec.mergePolicy.conflict` | *Optional* | string; enum=["", "reject"] | Only empty or reject passes validation. Empty is accepted; neither value enables replacement of existing resources through this declaration. |
| `spec.triggers` | *Optional* | array | Declared trigger list, default empty. The current runtime only validates/displays this list; see the resource rules. |
| `spec.triggers[].type` | **Conditionally required** | string; enum=["interval", "event"] | Required trigger category: interval or event. There is no default. |
| `spec.triggers[].every` | *Optional* | string | Required for interval; positive Go duration. For event, this field has no scheduling effect. No default. |
| `spec.triggers[].topic` | *Optional* | string | Event-topic declaration; optional and empty by default. The validator does not require it even for event. It is not a resource name. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
