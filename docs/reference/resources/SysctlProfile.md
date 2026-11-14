# SysctlProfile

[Resource configuration reference](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Applies a named set of router-oriented sysctls, with optional per-key overrides. It does not refer to a Sysctl resource by name.

## Conditions and references

- Profile entries keep their comparison mode and optional flag when an override changes their value. New override keys are added with exact comparison and optional=false.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    [ overrides:
        [ <name>: <string> ]
    ]
    [ persistent: <boolean> ]
    profile: ("router-linux" | "router-freebsd")
    [ runtime: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.profile` | **Required** | string; enum=["router-linux", "router-freebsd"] | Required selector: router-linux or router-freebsd. No automatic OS-based choice. The FreeBSD set enables IPv4/IPv6 forwarding; the Linux set also declares queue, socket, TCP and optional conntrack tuning. See the linked implementation for the exact key/value list. |
| `spec.runtime` | *Optional* | boolean | Apply profile entries to the live kernel; default true. |
| `spec.persistent` | *Optional* | boolean | Write per-entry persistent sysctl.d files; default false. Independent of runtime. |
| `spec.overrides` | *Optional* | object | Map from kernel sysctl key to nonblank string value; default empty. Existing keys are replaced and new keys are appended. Whitespace and / in keys, or blank values, are rejected. No resource references. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `profile`, `dryRun`
