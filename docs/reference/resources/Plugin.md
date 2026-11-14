# Plugin

[Resource configuration reference](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

Registers an executable implementing the routerd plugin protocol. Invoke it explicitly or through a configured consumer such as EventSubscription.

## Conditions and references

- The executable must be an absolute path. At execution it must exist, be a regular file, and have at least one executable permission bit.
- Declaring triggers does not start an interval/event scheduler in the current runtime. The values are validated and displayed by plugin list; a real caller must invoke the plugin.
- Context references use the full apiVersion/kind/name identity. Missing targets yield no context entry at execution; they are not a configuration-validation error. Secrets are redacted.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    [ capabilities:
        - ("observe.cloud" | "observe.providerPrivateIPs" | "propose.dynamicConfig" | "propose.providerAction" | "execute.providerAction")
    ]
    [ context:
        [ resources:
            -
              apiVersion: <string>
              kind: <string>
              name: <string>
        ]
    ]
    [ env:
        [ <name>: <string> ]
    ]
    executable: <string>
    [ timeout: <string> ]
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.executable` | **Required** | string | Absolute executable path; required, with no default. |
| `spec.timeout` | *Optional* | string | Maximum duration of one invocation. Default 10s; an explicit value must be a positive Go duration, such as 30s. |
| `spec.env` | *Optional* | object | Additional environment variables, keyed by variable name. Default empty; the runner supplies PATH from its own environment, or its built-in path if absent, and overlays these entries. Other parent variables are not inherited. Empty keys and keys containing = are ignored. |
| `spec.capabilities` | *Optional* | array | Declared capabilities; default empty. Allowed values are observe.cloud, observe.providerPrivateIPs, propose.dynamicConfig, propose.providerAction and execute.providerAction. Inventory execution requires observe.providerPrivateIPs; provider action execution requires execute.providerAction. This list does not itself schedule execution. |
| `spec.context` | *Optional* | object | Allowlist controlling which configuration resources are passed on stdin. Default empty, so no configuration is passed. |
| `spec.context.resources` | *Optional* | array | List of allowed resource identities. Default empty; no wildcard expansion. |
| `spec.context.resources[].apiVersion` | **Conditionally required** | string | API group and version of the referenced configuration resource; required in every entry. |
| `spec.context.resources[].kind` | **Conditionally required** | string | Kind of the referenced configuration resource; required in every entry. |
| `spec.context.resources[].name` | **Conditionally required** | string | metadata.name of the referenced configuration resource; required in every entry. |
| `spec.triggers` | *Optional* | array | Declared trigger list, default empty. The current runtime only validates/displays this list; see the resource rules. |
| `spec.triggers[].type` | **Conditionally required** | string; enum=["interval", "event"] | Required trigger category: interval or event. There is no default. |
| `spec.triggers[].every` | *Optional* | string | Required for interval; positive Go duration. For event, this field has no scheduling effect. No default. |
| `spec.triggers[].topic` | *Optional* | string | Event-topic declaration; optional and empty by default. The validator does not require it even for event. It is not a resource name. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
