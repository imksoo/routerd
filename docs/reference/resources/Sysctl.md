# Sysctl

[Resource configuration reference](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Reconciles one kernel sysctl setting and can write a persistent sysctl.d file. This resource has no reference to another routerd resource.

## Conditions and references

- When runtime=false and persistent=false, the controller records Skipped/RuntimeDisabled and does not apply the value. Both runtime and persistent can be true.
- Duplicate Sysctl keys in one Router are rejected. Linux NAT44SessionSync in conntrackd mode additionally requires an explicit non-optional runtime Sysctl net.netfilter.nf_conntrack_tcp_be_liberal with value and effective expectedValue equal to 1.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    [ compare: ("" | "exact" | "atLeast") ]
    [ expectedValue: <string> ]
    key: <string>
    [ optional: <boolean> ]
    [ persistent: <boolean> ]
    [ runtime: <boolean> ]
    value: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.key` | **Required** | string | Required sysctl key, e.g. net.ipv4.ip_forward. No default; spaces, tabs, newlines and / are forbidden. This is a kernel key, not a resource reference. |
| `spec.value` | **Required** | string | Required nonempty string passed to sysctl -w and persisted when requested. Quote numeric-looking YAML values. No default. |
| `spec.expectedValue` | *Optional* | string | Runtime value used for drift comparison; default value. Allows the observed representation to differ from the written value. A supplied whitespace-only value is rejected. |
| `spec.compare` | *Optional* | string; enum=["", "exact", "atLeast"] | Default exact: compare normalized whitespace-separated values as strings. atLeast compares corresponding signed integer components; component counts must match and every current component must be at least the expected value. |
| `spec.runtime` | *Optional* | boolean | Whether to apply the live kernel setting. Default true; false can still write persistent configuration when persistent=true. |
| `spec.persistent` | *Optional* | boolean | Write a routerd sysctl.d file. Default false. The file contains key=value; it is independent of the runtime switch. |
| `spec.optional` | *Optional* | boolean | Default false. When true, a failed runtime read records Skipped/ReadFailedOptional and skips the remaining apply for this entry. It does not suppress arbitrary write or persistence errors. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `key`, `value`, `dryRun`
