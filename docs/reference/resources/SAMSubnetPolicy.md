# SAMSubnetPolicy

[Resource configuration reference](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Publish shard assignments as federation events so each node can narrow MobilityPool ownership discovery to its assigned IPv4 subnets. This is a discovery-scope assignment, not an IP routing resource.

## Conditions and references

- sourcePrefix and all shards must be IPv4 CIDRs. Shards must be contained in sourcePrefix and cannot overlap; full coverage of the source prefix is not required. Each shard needs at least one distinct nonempty assigned node.
- Assignment events expire after 10 minutes and are refreshed by reconciliation. Missing/expired assignments or an event-store read error produce no shard filter, not a deny-all scope. Do not use this resource as the sole authorization boundary.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    poolRef: <string>
    shards:
      -
        assignedNodes:
          - <string>
        prefix: <string>
    sourcePrefix: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.sourcePrefix` | **Required** | string | Required IPv4 CIDR bounding all shards. No default. It is validated as containment input, not programmed as an address/route. |
| `spec.poolRef` | **Required** | string | Required MobilityPool metadata.name used as an exact event payload key; not Kind/name. The current validator checks nonempty text but does not verify pool existence. It must match the consuming pool name. |
| `spec.groupRef` | **Required** | string | Required EventGroup reference for publishing and reading assignments. Group lookup must resolve at runtime; shape validation only checks nonempty. Use the same reference spelling as the target MobilityPool.groupRef. |
| `spec.shards` | **Required** | array | Required nonempty list of disjoint subnets and responsible nodeRefs. Each node may appear in multiple different shards. |
| `spec.shards[].prefix` | **Required** | string | Required IPv4 CIDR within sourcePrefix, including equal-length whole-prefix assignment. Duplicates/overlaps across shards are rejected. |
| `spec.shards[].assignedNodes` | **Required** | array | Required nonempty nodeRef list, unique within the shard. Current validation does not check SAMNodeSet membership; values must match the consuming EventGroup local-node identity. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
