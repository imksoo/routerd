# VRF

[Resource configuration reference](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares a Linux VRF device, routing table and member intentions. The current implementation renders plans/artifact identities and protects its routing table, but has no VRF creation/member-attachment controller. Do not treat plan output as proof that a VRF was created.

## Conditions and references

- members uses logical interface names already declared in the Router. Self-membership is rejected. routeTable is also used to preserve the table during policy-route cleanup and may be referenced by BGP VRF configuration.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    members: # optional; array
      - "<string>" # string
    routeTable: 1 # Required; integer; minimum=1; maximum=4294967295
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.ifname` | Kernel VRF name, default metadata.name. Must be at most 15 characters and contain no whitespace or /. |
| `spec.routeTable` | Required positive routing-table ID, no default. The schema limits it to 1–4294967295; the current Go validator checks the lower bound. |
| `spec.members` | Logical interface-name list, default empty. No Kind/ prefix. Each member must exist and must not equal this VRF metadata.name; actual attachment is not implemented. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
