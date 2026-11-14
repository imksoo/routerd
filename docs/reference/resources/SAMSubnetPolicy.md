# SAMSubnetPolicy

[Resource configuration reference](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Publish shard assignments as federation events so each node can narrow MobilityPool ownership discovery to its assigned IPv4 subnets. This is a discovery-scope assignment, not an IP routing resource.

## Conditions and references

- sourcePrefix and all shards must be IPv4 CIDRs. Shards must be contained in sourcePrefix and cannot overlap; full coverage of the source prefix is not required. Each shard needs at least one distinct nonempty assigned node.
- Assignment events expire after 10 minutes and are refreshed by reconciliation. Missing/expired assignments or an event-store read error produce no shard filter, not a deny-all scope. Do not use this resource as the sole authorization boundary.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    poolRef: "<string>" # Required; string
    shards: # Required; array
      - # object
        assignedNodes: # Required; array
          - "<string>" # string
        prefix: "<string>" # Required; string
    sourcePrefix: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.sourcePrefix` | Required IPv4 CIDR bounding all shards. No default. It is validated as containment input, not programmed as an address/route. |
| `spec.poolRef` | Required MobilityPool metadata.name used as an exact event payload key; not Kind/name. The current validator checks nonempty text but does not verify pool existence. It must match the consuming pool name. |
| `spec.groupRef` | Required EventGroup reference for publishing and reading assignments. Group lookup must resolve at runtime; shape validation only checks nonempty. Use the same reference spelling as the target MobilityPool.groupRef. |
| `spec.shards` | Required nonempty list of disjoint subnets and responsible nodeRefs. Each node may appear in multiple different shards. |
| `spec.shards[].prefix` | Required IPv4 CIDR within sourcePrefix, including equal-length whole-prefix assignment. Duplicates/overlaps across shards are rejected. |
| `spec.shards[].assignedNodes` | Required nonempty nodeRef list, unique within the shard. Current validation does not check SAMNodeSet membership; values must match the consuming EventGroup local-node identity. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.
