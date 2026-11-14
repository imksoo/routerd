# Hostname

[Resource configuration reference](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Manages the OS hostname, separately from Router.metadata.name and resource names.

## Conditions and references

- Only one managed Hostname may be applied; multiple managed entries fail at reconcile. Unmanaged entries are ignored by the controller. Linux uses hostnamectl, FreeBSD uses sysrc and hostname.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # Required; string
    managed: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.hostname` | Required nonempty OS hostname, with no default. Spaces, tabs, newlines and / are rejected. This field does not refer to a DNSZone or Interface. |
| `spec.managed` | Default false: no hostname changes. True observes the current/static hostname and changes it when needed; dry-run does not invoke the changing commands. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `hostname`
