# EventPeer

[Resource configuration reference](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Declare one remote delivery target for an EventGroup. Transport authentication and local identity come from that group.

## Conditions and references

- groupRef and nodeName must be nonblank; endpoint is required. groupRef matches the bare EventGroup metadata.name, not EventGroup/name. Its existence is not checked by the EventPeer validator; unmatched peers are not included in a group config.
- Static peers override imported peers by nodeName; later static entries with the same nodeName replace earlier ones. types and subjectPrefixes narrow delivery together (AND between categories, OR within each list). Matching is case-sensitive; types are exact strings, not topic wildcards.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # optional; string; enum=["push"]
    endpoint: "<string>" # optional; string
    groupRef: "<string>" # Required; string
    nodeName: "<string>" # Required; string
    subjectPrefixes: # optional; array
      - "<string>" # string
    types: # optional; array
      - "<string>" # string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.groupRef` | Required bare EventGroup name; the referenced group needs auth.secretFile to run peer transport. No default. |
| `spec.nodeName` | Required remote identity and deduplication/delivery-record key. Distinct YAML names do not make independent peers if nodeName is the same. |
| `spec.endpoint` | Required base URL, for example http://192.0.2.10:8787. The pusher trims trailing slashes and appends /v1/events; do not include that path yourself. URL/scheme reachability is not checked by local YAML validation. |
| `spec.direction` | Only push or empty accepted; default push. The renderer always creates an outbound push target, not a pull client. |
| `spec.types` | Optional exact event-type allowlist, default empty permits all types. Nonempty entries cannot be blank. |
| `spec.subjectPrefixes` | Optional case-sensitive subject prefix allowlist, default empty permits all subjects. Any prefix match passes this category. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
