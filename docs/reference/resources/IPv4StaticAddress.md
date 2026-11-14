# IPv4StaticAddress

[Resource configuration reference](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Declares a static IPv4 address on a logical interface. The controller converges configured addresses; this is not a DHCP lease.

## Conditions and references

- An interface reference and address/prefix are required. Router-wide indexing rejects two static resources with the same logical interface and masked network prefix, even if the host address differs. VirtualAddress overlap on that same interface/prefix is also rejected.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # Required; string
    allowOverlap: true # optional; boolean
    allowOverlapReason: "<string>" # optional; string
    exclusive: true # optional; boolean
    interface: "<string>" # Required; string
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
| `spec.interface` | Required logical interface name from the Router interface index, without Interface/ prefix. It must exist in the declared config. No default. |
| `spec.address` | Required IPv4 host address with prefix length, e.g. 192.0.2.1/24. The parser checks prefix syntax; runtime operations use IPv4. No default or implicit /32. |
| `spec.exclusive` | Default false. The current observer warns that exclusive cleanup is not implemented; the runtime address controller does not read this flag. Global reconciliation of extra addresses is independent, so false does not promise to preserve manual addresses. |
| `spec.allowOverlap` | Default false. Retained as a readable field but not used by current config validation or address reconciliation; it does not bypass duplicate/conflict checks. |
| `spec.allowOverlapReason` | Default empty. Stored declaration only; it does not authorize overlap or alter the effect of allowOverlap. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
