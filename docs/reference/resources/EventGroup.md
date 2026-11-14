# EventGroup

[Resource configuration reference](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

Declare a shared federation event group and this router’s identity, peer discovery, receiver and local retention settings.

## Conditions and references

- nodeName is required. metadata.name is the group identifier; remote participants use the same group name and shared secret. Identity-only groups may omit secretFile, but the transport helper starts only when secretFile is supplied. Applied means runtime config was written, not proof that peers received events.
- secretFile is required by configuration validation when listen.address, peersFrom or a referencing EventPeer is present. auth.secretRef does not satisfy this requirement. The file must exist and contain nonblank secret bytes when eventd starts; surrounding whitespace is removed.
- Receiver startup is controlled by listen.port > 0, not by address alone. Always supply a specific address with a positive port; an empty address with a positive port can bind all addresses. Port 0 disables the TCP receiver; push and pruning can continue.
- peersFrom imports SAMNodeSet nodes with nonblank eventEndpoint, excluding nodeRef equal to nodeName. Peers are deduplicated by remote nodeName; later sources replace earlier ones, then static EventPeer entries override imported peers. A missing required source leaves Pending and preserves the last runtime config; optional missing sources are skipped.
- Retention is enforced locally by eventd at startup and periodically (default 1m). Age and count limits both apply; age uses observedAt, and the count limit keeps newest observed events. This is separate from each event’s expiresAt and from local EventRule journal retention.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    [ auth:
        [ mode: ("" | "hmac") ]
        [ secretFile: <string> ]
        [ secretRef: <string> ]
    ]
    [ listen:
        [ address: <string> ]
        [ port: <integer> ]
    ]
    nodeName: <string>
    [ peersFrom:
        -
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ replayWindow: <string> ]
    [ retention:
        [ maxAge: <string> ]
        [ maxEvents: <integer> ]
    ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.nodeName` | **Required** | string | Required nonblank local node identity, stamped on locally originated federation events. No hostname default; must match SAMNodeSet.nodeRef when used with peersFrom. |
| `spec.peersFrom` | *Optional* | array | Optional ordered discovery-source list, default empty. Supplying it requires auth.secretFile, even if all sources are optional or yield no peers. |
| `spec.peersFrom[].resource` | **Conditionally required** | string | Required SAMNodeSet/<name> reference. Format is validated; missing declared source is handled at runtime by optional/Pending rather than rejected as an ordinary status-value reference. |
| `spec.peersFrom[].optional` | *Optional* | boolean | Default false: missing source blocks new config. True skips a missing source. Malformed or undecodable sources still fail. |
| `spec.retention` | *Optional* | object | Optional local federation-event retention settings; defaults impose no age/count limit. Effective only while eventd pruning runs. |
| `spec.retention.maxEvents` | *Optional* | integer; minimum=0 | Nonnegative maximum event count per group, default 0 means unlimited. Positive values keep newest observedAt/id records after age pruning. |
| `spec.retention.maxAge` | *Optional* | string | Optional Go duration, default empty means unlimited. Zero/negative durations pass validation and are omitted from runtime config, also meaning no age limit. |
| `spec.auth` | *Optional* | object | Optional transport integrity settings. Current transport always uses HMAC; there is no unauthenticated transport mode. |
| `spec.auth.mode` | *Optional* | string; enum=["", "hmac"] | Accepted empty or hmac, default empty. Both use HMAC when transport runs; the controller does not use this value to select another scheme. |
| `spec.auth.secretRef` | *Optional* | string | Default empty. Accepted but not resolved by the current controller/helper; it does not replace secretFile and is not validated as a resource reference. |
| `spec.auth.secretFile` | **Conditionally required** | string | Filesystem path to the shared HMAC secret. Empty disables the transport helper; required for receiver address or peers as described above. No automatic secret distribution. |
| `spec.listen` | *Optional* | object | Optional HTTP receiver bind, default address empty/port 0. Explicitly configure both for reception. |
| `spec.listen.address` | *Optional* | string | Receiver bind address, default empty. Nonempty requires port 1–65535 and secretFile. The daemon does not use empty address alone to disable listening; see the port rule. |
| `spec.listen.port` | *Optional* | integer; minimum=0; maximum=65535 | Integer 0–65535, default 0 disables receiver. Positive values enable plain HTTP on address; this resource has no TLS certificate fields. |
| `spec.replayWindow` | *Optional* | string | Go duration bounding accepted signed-request timestamp skew. Omitted, zero or negative resolves to 5m. It is not event retention or a message deduplication TTL. |
| `spec.when` | *Optional* | object | Optional common activation condition, default unconditional. Effective-config filtering controls transport config and service lifecycle; see common fields for predicates and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `group`, `nodeName`, `peers`, `peersFrom`, `pendingSources`, `listenAddress`, `listenPort`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
