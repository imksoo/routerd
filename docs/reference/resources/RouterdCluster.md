# RouterdCluster

[Resource configuration reference](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Coordinates which routerd process may perform mutations through a file lease and OS file locking. It is not VRRP election or peer network discovery.

## Conditions and references

- Participants need a leasePath with shared, reliable locking semantics. peers are identities, not endpoints: the lease implementation only uses the nonempty list to enable coordination and does not contact peer addresses or validate membership. Standby controllers perform dry-run mutation paths.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # optional; string
    leasePath: "<string>" # optional; string
    leaseTTL: "<string>" # optional; string
    peers: # Required; array
      - "<string>" # string
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
| `spec.peers` | Required list with at least two distinct nonblank entries after compaction. No default; entries are not Kind/name references or network addresses to dial. |
| `spec.leaseTTL` | Lease expiry interval, default 30s. Explicit values must be Go durations between 5s and 10m. The lock still controls exclusive mutation; expiry alone is not a network failure detector. |
| `spec.leasePath` | Required absolute lease-file path without line breaks. No default; a separate file on each host does not coordinate those hosts. |
| `spec.identity` | Local lease-holder identity; defaults to os.Hostname() after trimming. It is not derived from Router.metadata.name. |
| `spec.when` | Activation condition; omitted means no gate. See common reference conditions. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `leader`, `leaseExpiresAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
