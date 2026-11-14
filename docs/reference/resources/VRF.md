# VRF

[Resource configuration reference](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares a Linux VRF device, routing table and member intentions. The current implementation renders plans/artifact identities and protects its routing table, but has no VRF creation/member-attachment controller. Do not treat plan output as proof that a VRF was created.

## Conditions and references

- members uses logical interface names already declared in the Router. Self-membership is rejected. routeTable is also used to preserve the table during policy-route cleanup and may be referenced by BGP VRF configuration.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    [ ifname: <string> ]
    [ members:
        - <string>
    ]
    routeTable: <integer>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.ifname` | *Optional* | string | Kernel VRF name, default metadata.name. Must be at most 15 characters and contain no whitespace or /. |
| `spec.routeTable` | **Required** | integer; minimum=1; maximum=4294967295 | Required positive routing-table ID, no default. The schema limits it to 1–4294967295; the current Go validator checks the lower bound. |
| `spec.members` | *Optional* | array | Logical interface-name list, default empty. No Kind/ prefix. Each member must exist and must not equal this VRF metadata.name; actual attachment is not implemented. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `ifname`, `routeTable`, `members`

[Example](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
