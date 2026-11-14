# EventPeer

[Resource configuration reference](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Declare one remote delivery target for an EventGroup. Transport authentication and local identity come from that group.

## Conditions and references

- groupRef and nodeName must be nonblank; endpoint is required. groupRef matches the bare EventGroup metadata.name, not EventGroup/name. Its existence is not checked by the EventPeer validator; unmatched peers are not included in a group config.
- Static peers override imported peers by nodeName; later static entries with the same nodeName replace earlier ones. types and subjectPrefixes narrow delivery together (AND between categories, OR within each list). Matching is case-sensitive; types are exact strings, not topic wildcards.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    [ direction: ("push") ]
    endpoint: <string>
    groupRef: <string>
    nodeName: <string>
    [ subjectPrefixes:
        - <string>
    ]
    [ types:
        - <string>
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.groupRef` | **Required** | string | Required bare EventGroup name; the referenced group needs auth.secretFile to run peer transport. No default. |
| `spec.nodeName` | **Required** | string | Required remote identity and deduplication/delivery-record key. Distinct YAML names do not make independent peers if nodeName is the same. |
| `spec.endpoint` | **Required** | string | Required base URL, for example http://192.0.2.10:8787. The pusher trims trailing slashes and appends /v1/events; do not include that path yourself. URL/scheme reachability is not checked by local YAML validation. |
| `spec.direction` | *Optional* | string; enum=["push"] | Only push or empty accepted; default push. The renderer always creates an outbound push target, not a pull client. |
| `spec.types` | *Optional* | array | Optional exact event-type allowlist, default empty permits all types. Nonempty entries cannot be blank. |
| `spec.subjectPrefixes` | *Optional* | array | Optional case-sensitive subject prefix allowlist, default empty permits all subjects. Any prefix match passes this category. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
