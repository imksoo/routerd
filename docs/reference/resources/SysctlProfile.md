# SysctlProfile

[Resource configuration reference](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Applies a named set of router-oriented sysctls, with optional per-key overrides. It does not refer to a Sysctl resource by name.

## Conditions and references

- Profile entries keep their comparison mode and optional flag when an override changes their value. New override keys are added with exact comparison and optional=false.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # optional; object
      "<key>": "<string>" # optional; string
    persistent: true # optional; boolean
    profile: "router-linux" # Required; string; enum=["router-linux", "router-freebsd"]
    runtime: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.profile` | Required selector: router-linux or router-freebsd. No automatic OS-based choice. The FreeBSD set enables IPv4/IPv6 forwarding; the Linux set also declares queue, socket, TCP and optional conntrack tuning. See the linked implementation for the exact key/value list. |
| `spec.runtime` | Apply profile entries to the live kernel; default true. |
| `spec.persistent` | Write per-entry persistent sysctl.d files; default false. Independent of runtime. |
| `spec.overrides` | Map from kernel sysctl key to nonblank string value; default empty. Existing keys are replaced and new keys are appended. Whitespace and / in keys, or blank values, are rejected. No resource references. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)
