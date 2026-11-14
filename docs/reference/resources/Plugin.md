# Plugin

[Resource configuration reference](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

Registers an executable implementing the routerd plugin protocol. Invoke it explicitly or through a configured consumer such as EventSubscription.

## Conditions and references

- The executable must be an absolute path. At execution it must exist, be a regular file, and have at least one executable permission bit.
- Declaring triggers does not start an interval/event scheduler in the current runtime. The values are validated and displayed by plugin list; a real caller must invoke the plugin.
- Context references use the full apiVersion/kind/name identity. Missing targets yield no context entry at execution; they are not a configuration-validation error. Secrets are redacted.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # optional; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # optional; object
      resources: # optional; array
        - # object
          apiVersion: "<string>" # Required; string
          kind: "<string>" # Required; string
          name: "<string>" # Required; string
    env: # optional; object
      "<key>": "<string>" # optional; string
    executable: "<string>" # Required; string
    timeout: "<string>" # optional; string
    triggers: # optional; array
      - # object
        every: "<string>" # optional; string
        topic: "<string>" # optional; string
        type: "interval" # Required; string; enum=["interval", "event"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.executable` | Absolute executable path; required, with no default. |
| `spec.timeout` | Maximum duration of one invocation. Default 10s; an explicit value must be a positive Go duration, such as 30s. |
| `spec.env` | Additional environment variables, keyed by variable name. Default empty; the runner supplies PATH from its own environment, or its built-in path if absent, and overlays these entries. Other parent variables are not inherited. Empty keys and keys containing = are ignored. |
| `spec.capabilities` | Declared capabilities; default empty. Allowed values are observe.cloud, observe.providerPrivateIPs, propose.dynamicConfig, propose.providerAction and execute.providerAction. Inventory execution requires observe.providerPrivateIPs; provider action execution requires execute.providerAction. This list does not itself schedule execution. |
| `spec.context` | Allowlist controlling which configuration resources are passed on stdin. Default empty, so no configuration is passed. |
| `spec.context.resources` | List of allowed resource identities. Default empty; no wildcard expansion. |
| `spec.context.resources[].apiVersion` | API group and version of the referenced configuration resource; required in every entry. |
| `spec.context.resources[].kind` | Kind of the referenced configuration resource; required in every entry. |
| `spec.context.resources[].name` | metadata.name of the referenced configuration resource; required in every entry. |
| `spec.triggers` | Declared trigger list, default empty. The current runtime only validates/displays this list; see the resource rules. |
| `spec.triggers[].type` | Required trigger category: interval or event. There is no default. |
| `spec.triggers[].every` | Required for interval; positive Go duration. For event, this field has no scheduling effect. No default. |
| `spec.triggers[].topic` | Event-topic declaration; optional and empty by default. The validator does not require it even for event. It is not a resource name. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
