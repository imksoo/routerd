# NAT44SessionSync

[Resource configuration reference](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Replicate Linux conntrack state using either an SSH event stream or a continuously running conntrackd FTFW peer link. This is not FreeBSD PF state synchronization.

## Conditions and references

- mode defaults event-stream. Both modes require a nonempty snatAddresses or natRules list in YAML validation. Event-stream also requires targets; conntrackd requires conntrackd.interface/localAddress/peerAddress and an empty when condition.
- Event-stream initially reconciles a dump, then consumes conntrack -E extended events, batching changes (normally up to 5s or 512 operations) and applying insert/delete scripts over SSH. It supports IPv4 TCP/UDP/ICMP records; TCP inserts lacking state are skipped. It does not replicate complete TCP window/sequence tracking, so it is not the full established-TCP failover mechanism.
- In conntrackd mode both peers replicate continuously; the legacy SSH worker is stopped. snatAddresses/natRules/excludeNatRules are not used to filter the conntrackd configuration despite being validated. Its renderer permits TCP/UDP/ICMP and excludes loopback plus ignoreIPv4. targets/conntrackCommand/interval do not control conntrackd.
- Linux conntrackd validation additionally requires an explicit nonoptional Sysctl net.netfilter.nf_conntrack_tcp_be_liberal with value=1, expectedValue effectively 1 and runtime enabled. A SysctlProfile alone does not satisfy it. Conntrackd addresses/ignoreIPv4 are bare IPv4 addresses; no automatic peer discovery or key distribution.
- Use a single conntrackd resource: generated instances share /run/routerd/conntrackd.ctl and lock paths. The current routerd-vrrp-vmac promotion integration explicitly targets /etc/conntrackd/routerd-dslite-sessions.conf, so that integration expects metadata.name=dslite-sessions; an arbitrary resource name does not automatically wire its cache into VRRP promotion.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    [ conntrackCommand: <string> ]
    [ conntrackd:
        [ ignoreIPv4:
            - <string>
        ]
        [ interface: <string> ]
        [ localAddress: <string> ]
        [ peerAddress: <string> ]
        [ port: <integer> ]
    ]
    [ excludeNatRules:
        - <string>
    ]
    [ interval: <string> ]
    [ mode: ("event-stream" | "conntrackd") ]
    [ natRules:
        - <string>
    ]
    [ snatAddresses:
        - <string>
    ]
    [ targets:
        -
          host: <string>
          [ name: <string> ]
          [ restoreCommand:
              - <string>
          ]
          [ sshOptions:
              - <string>
          ]
          [ user: <string> ]
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
| `spec.mode` | *Optional* | string; enum=["event-stream", "conntrackd"] | event-stream (default) or conntrackd. The former relays selected tuples via SSH; the latter runs native FTFW replication via systemd on Linux. |
| `spec.interval` | *Optional* | string | No supported polling interval. Any nonempty value is rejected in event-stream mode; in conntrackd mode accepted but unused. |
| `spec.conntrackCommand` | *Optional* | string | Event-stream executable name/path, default conntrack. Treated as one executable argument, not a shell command line. Ignored by conntrackd mode. |
| `spec.snatAddresses` | **Conditionally required** | array | Bare IPv4 addresses selecting event-stream tuples, default empty. Combined with addresses resolved from natRules, deduplicated and sorted. At least one address or natRules entry is required; conntrackd does not use this list as a replication filter. |
| `spec.natRules` | **Conditionally required** | array | Optional NAT44Rule/name or bare rule names. Reference syntax only is validated, not existence. Event-stream reads declared snatAddress then NAT44Rule status.snatAddress; any unresolved nonexcluded rule blocks the worker with Pending. Alternative translation.address/pool is not resolved here. |
| `spec.excludeNatRules` | *Optional* | array | Optional NAT44Rule/name or bare names skipped while resolving natRules. Does not subtract explicit snatAddresses or filter conntrackd. Default empty. |
| `spec.targets` | **Conditionally required** | array | Required nonempty in event-stream mode. Each target receives SSH restoration operations; the same targets are unused by conntrackd. |
| `spec.targets[].name` | *Optional* | string | Optional descriptive target/status name, default empty; host determines the actual destination. |
| `spec.targets[].host` | **Conditionally required** | string | Required nonblank SSH host (or SSH config alias). Remote user must be able to execute the restoration command with conntrack privileges. No automatic credentials are provisioned. |
| `spec.targets[].user` | *Optional* | string | Optional remote SSH user, default empty leaves selection to SSH configuration. Nonempty produces user@host. |
| `spec.targets[].sshOptions` | *Optional* | array | Optional SSH argument list, default adds BatchMode=yes and ConnectTimeout=10 unless overridden by explicit options. Each list item is one argument; no embedded newlines. Command operations also have an implementation timeout of 2m. |
| `spec.targets[].restoreCommand` | *Optional* | array | Optional remote conntrack command argv, default [conntrack]. Can supply a privilege wrapper as separate arguments, for example [sudo, -n, conntrack]. It is not a periodic full-state transfer command. |
| `spec.conntrackd` | **Conditionally required** | object | Required object for conntrackd mode; otherwise unused by event-stream. Both HA peers must run continuously and have the replication link allowed. |
| `spec.conntrackd.interface` | **Conditionally required** | string | Required replication interface. A configured logical alias is resolved when available; otherwise used as a kernel name. No ordinary interface-resource existence check here; renderer rejects whitespace. |
| `spec.conntrackd.localAddress` | **Conditionally required** | string | Required bare IPv4 local replication address; must already be configured on the host. Not an address assignment. |
| `spec.conntrackd.peerAddress` | **Conditionally required** | string | Required bare IPv4 remote replication destination. No discovery or multicast membership is configured. |
| `spec.conntrackd.port` | *Optional* | integer; minimum=1; maximum=65535 | UDP replication port; omitted/0 resolves to 3780 in the renderer. Valid positive values 1–65535. JSON Schema marks minimum 1 even though the Go runtime accepts zero as default. |
| `spec.conntrackd.ignoreIPv4` | *Optional* | array | Optional bare IPv4 exclusions, default empty in YAML. Renderer always adds 127.0.0.1 and excludes IPv6 ::1, deduplicates/sorts IPv4 values, and rejects invalid values when rendering. |
| `spec.when` | *Optional* | object | Optional common activation condition for event-stream, default unconditional; may gate the active SSH worker. Must be completely empty for conntrackd so both peers continue replication while standby. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/nat44sessionsync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/nat44sessionsync.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/render/conntrackd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/conntrackd.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `mode`, `streamState`, `sessionCount`, `remoteSessionCount`, `targetCount`, `snatAddresses`, `targets`, `deleteOK`, `deleteMissing`, `deleteFailed`, `pruneOK`, `pruneMissing`, `pruneFailed`, `pruneCandidateCount`, `insertOK`, `insertExisting`, `insertFailed`, `queuedEventCount`, `lastBatchEvents`, `resyncCount`, `syncedAt`, `lastEventAt`, `lastBatchAt`, `lastResyncAt`, `dryRun`
