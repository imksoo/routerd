# Sysctl

[Resource configuration reference](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Reconciles one kernel sysctl setting and can write a persistent sysctl.d file. This resource has no reference to another routerd resource.

## Conditions and references

- When runtime=false and persistent=false, the controller records Skipped/RuntimeDisabled and does not apply the value. Both runtime and persistent can be true.
- Duplicate Sysctl keys in one Router are rejected. Linux NAT44SessionSync in conntrackd mode additionally requires an explicit non-optional runtime Sysctl net.netfilter.nf_conntrack_tcp_be_liberal with value and effective expectedValue equal to 1.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    compare: "" # optional; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # optional; string
    key: "<string>" # Required; string
    optional: true # optional; boolean
    persistent: true # optional; boolean
    runtime: true # optional; boolean
    value: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.key` | Required sysctl key, e.g. net.ipv4.ip_forward. No default; spaces, tabs, newlines and / are forbidden. This is a kernel key, not a resource reference. |
| `spec.value` | Required nonempty string passed to sysctl -w and persisted when requested. Quote numeric-looking YAML values. No default. |
| `spec.expectedValue` | Runtime value used for drift comparison; default value. Allows the observed representation to differ from the written value. A supplied whitespace-only value is rejected. |
| `spec.compare` | Default exact: compare normalized whitespace-separated values as strings. atLeast compares corresponding signed integer components; component counts must match and every current component must be at least the expected value. |
| `spec.runtime` | Whether to apply the live kernel setting. Default true; false can still write persistent configuration when persistent=true. |
| `spec.persistent` | Write a routerd sysctl.d file. Default false. The file contains key=value; it is independent of the runtime switch. |
| `spec.optional` | Default false. When true, a failed runtime read records Skipped/ReadFailedOptional and skips the remaining apply for this entry. It does not suppress arbitrary write or persistence errors. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)
