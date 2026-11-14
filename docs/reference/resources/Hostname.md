# Hostname

[Resource configuration reference](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Manages the OS hostname, separately from Router.metadata.name and resource names.

## Conditions and references

- Only one managed Hostname may be applied; multiple managed entries fail at reconcile. Unmanaged entries are ignored by the controller. Linux uses hostnamectl, FreeBSD uses sysrc and hostname.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: <string>
    [ managed: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.hostname` | **Required** | string | Required nonempty OS hostname, with no default. Spaces, tabs, newlines and / are rejected. This field does not refer to a DNSZone or Interface. |
| `spec.managed` | *Optional* | boolean | Default false: no hostname changes. True observes the current/static hostname and changes it when needed; dry-run does not invoke the changing commands. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `hostname`
