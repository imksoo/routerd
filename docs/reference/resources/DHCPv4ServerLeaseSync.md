# DHCPv4ServerLeaseSync

[Resource configuration reference](../resources.md#dhcpv4serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Copy a DHCPv4 server lease file and available sticky-state files to peer hosts using rsync over SSH. This is file replication, not the DHCP failover protocol.

## Conditions and references

- source.resource and at least one target host are required. Unlike status-value references, normal validation checks the source spelling/type but does not verify that this DHCPv4Server exists. A missing named source therefore falls back to the platform default lease path.
- Target paths are the same absolute paths as the local sources; YAML provides no target path field. When stickyHoldDays is positive on the named source, dhcp-sticky.db and its -wal file are copied if present; -shm is not copied. The main lease file is mandatory.
- Each invocation uses rsync -a --checksum --delay-updates, a default rsync inactivity timeout of 60s and a 2m command execution limit. SSH defaults BatchMode=yes and ConnectTimeout=10, overridable via sshOptions. A Synced status records file-copy success, not peer lease reload or client continuity.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    [ command: <string> ]
    [ interval: <string> ]
    source:
      resource: <string>
    targets:
      -
        host: <string>
        [ name: <string> ]
        [ options:
            - <string>
        ]
        [ sshOptions:
            - <string>
        ]
        [ user: <string> ]
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
| `spec.source` | **Required** | object | Required source descriptor with resource; no filesystem path override here. |
| `spec.source.resource` | **Required** | string | Required bare DHCPv4Server name or DHCPv4Server/name. Uses its nonblank leaseFile, otherwise <StateDir>/dnsmasq/dnsmasq.leases. This is not a status-value reference and has no field/optional members. |
| `spec.command` | *Optional* | string | Optional executable name/path, default rsync; receives rsync arguments directly, not a shell command string. |
| `spec.interval` | *Optional* | string | Go duration, default 30s. Suppresses work until this long after the last successful syncedAt; controller itself is polled every 30s and also on subscriptions. Zero/negative values disable this skip, not accelerate the periodic scheduler. |
| `spec.targets` | **Required** | array | Required nonempty peer-target list. Files are sent serially to each target; first failed transfer records Error/SyncFailed. No remote routerd API call is made. |
| `spec.targets[].name` | *Optional* | string | Optional status label, default empty; does not select the destination host or source. |
| `spec.targets[].host` | **Required** | string | Required SSH host/address. CR/LF prohibited; must already be reachable and configured for noninteractive SSH. |
| `spec.targets[].user` | *Optional* | string | Optional SSH user; empty omits user@ and lets SSH choose its configured user. CR/LF prohibited. |
| `spec.targets[].sshOptions` | *Optional* | array | Optional SSH argument list assembled into rsync -e. Defaults add -o BatchMode=yes and -o ConnectTimeout=10 unless those keys are supplied. No CR/LF in entries. |
| `spec.targets[].options` | *Optional* | array | Optional extra rsync arguments, appended after defaults. A supplied --timeout or --timeout=... suppresses the default --timeout=60. Does not override the separate 2m process limit; no CR/LF. |
| `spec.when` | *Optional* | object | Optional common state condition, default unconditional. Use actual active-role status to prevent standby writers; this resource does not itself negotiate which peer should own the lease file. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
