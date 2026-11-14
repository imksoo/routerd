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

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # optional; object
      mode: "" # optional; string; enum=["", "hmac"]
      secretFile: "<string>" # optional; string
      secretRef: "<string>" # optional; string
    listen: # optional; object
      address: "<string>" # optional; string
      port: 0 # optional; integer; minimum=0; maximum=65535
    nodeName: "<string>" # Required; string
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    replayWindow: "<string>" # optional; string
    retention: # optional; object
      maxAge: "<string>" # optional; string
      maxEvents: 0 # optional; integer; minimum=0
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.nodeName` | Required nonblank local node identity, stamped on locally originated federation events. No hostname default; must match SAMNodeSet.nodeRef when used with peersFrom. |
| `spec.peersFrom` | Optional ordered discovery-source list, default empty. Supplying it requires auth.secretFile, even if all sources are optional or yield no peers. |
| `spec.peersFrom[].resource` | Required SAMNodeSet/<name> reference. Format is validated; missing declared source is handled at runtime by optional/Pending rather than rejected as an ordinary status-value reference. |
| `spec.peersFrom[].optional` | Default false: missing source blocks new config. True skips a missing source. Malformed or undecodable sources still fail. |
| `spec.retention` | Optional local federation-event retention settings; defaults impose no age/count limit. Effective only while eventd pruning runs. |
| `spec.retention.maxEvents` | Nonnegative maximum event count per group, default 0 means unlimited. Positive values keep newest observedAt/id records after age pruning. |
| `spec.retention.maxAge` | Optional Go duration, default empty means unlimited. Zero/negative durations pass validation and are omitted from runtime config, also meaning no age limit. |
| `spec.auth` | Optional transport integrity settings. Current transport always uses HMAC; there is no unauthenticated transport mode. |
| `spec.auth.mode` | Accepted empty or hmac, default empty. Both use HMAC when transport runs; the controller does not use this value to select another scheme. |
| `spec.auth.secretRef` | Default empty. Accepted but not resolved by the current controller/helper; it does not replace secretFile and is not validated as a resource reference. |
| `spec.auth.secretFile` | Filesystem path to the shared HMAC secret. Empty disables the transport helper; required for receiver address or peers as described above. No automatic secret distribution. |
| `spec.listen` | Optional HTTP receiver bind, default address empty/port 0. Explicitly configure both for reception. |
| `spec.listen.address` | Receiver bind address, default empty. Nonempty requires port 1–65535 and secretFile. The daemon does not use empty address alone to disable listening; see the port rule. |
| `spec.listen.port` | Integer 0–65535, default 0 disables receiver. Positive values enable plain HTTP on address; this resource has no TLS certificate fields. |
| `spec.replayWindow` | Go duration bounding accepted signed-request timestamp skew. Omitted, zero or negative resolves to 5m. It is not event retention or a message deduplication TTL. |
| `spec.when` | Optional common activation condition, default unconditional. Effective-config filtering controls transport config and service lifecycle; see common fields for predicates and schema limitations. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
