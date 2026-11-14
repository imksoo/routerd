# RouterdCluster

[Resource configuration reference](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Coordinates which routerd process may perform mutations through a file lease and OS file locking. It is not VRRP election or peer network discovery.

## Conditions and references

- Participants need a leasePath with shared, reliable locking semantics. peers are identities, not endpoints: the lease implementation only uses the nonempty list to enable coordination and does not contact peer addresses or validate membership. Standby controllers perform dry-run mutation paths.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    [ identity: <string> ]
    leasePath: <string>
    [ leaseTTL: <string> ]
    peers:
      - <string>
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
| `spec.peers` | **Required** | array | Required list with at least two distinct nonblank entries after compaction. No default; entries are not Kind/name references or network addresses to dial. |
| `spec.leaseTTL` | *Optional* | string | Lease expiry interval, default 30s. Explicit values must be Go durations between 5s and 10m. The lock still controls exclusive mutation; expiry alone is not a network failure detector. |
| `spec.leasePath` | **Required** | string | Required absolute lease-file path without line breaks. No default; a separate file on each host does not coordinate those hosts. |
| `spec.identity` | *Optional* | string | Local lease-holder identity; defaults to os.Hostname() after trimming. It is not derived from Router.metadata.name. |
| `spec.when` | *Optional* | object | Activation condition; omitted means no gate. See common reference conditions. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `leader`, `leaseExpiresAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
