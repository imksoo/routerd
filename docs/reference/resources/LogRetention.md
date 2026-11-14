# LogRetention

[Resource configuration reference](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Prunes operational-log SQLite records and optionally archives them first. It does not control arbitrary system log files.

## Conditions and references

- Runs on the first reconciliation, then when at least 23 hours have elapsed since lastRunAt. Both omitted schedule and daily use this elapsed-time rule, not a midnight cron schedule. Dry-run does not open or prune external log databases.
- The event journal independently enforces a 24-hour age bound, plus its own row/payload bounds. A longer retention here cannot extend the event journal beyond that bound.
- When sinks are set, each row is sent before deletion. A sink error prevents successful deletion of that row. LogSink type otlp currently sends nothing, so it must not be treated as a working archive destination.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: <string>
    [ schedule: ("" | "daily") ]
    [ signals:
        - ("events" | "accessLogs" | "pluginRuns" | "dnsQueries" | "trafficFlows" | "firewallEvents" | "dhcpFingerprints")
    ]
    [ sinks:
        - <string>
    ]
    [ vacuum: <boolean> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.retention` | **Required** | string | Required retention interval, e.g. 7d or 24h. d means an integer number of 24-hour days; other values use Go duration syntax. No default. The validator parses the duration but does not require it to be positive. Use a positive interval for retention. |
| `spec.signals` | *Optional* | array | Log categories to prune. Default all seven: events, accessLogs, pluginRuns, dnsQueries, trafficFlows, firewallEvents, dhcpFingerprints. Storage files are resolved from the relevant logging resources or platform defaults. |
| `spec.sinks` | *Optional* | array | Archive destinations before deletion, default empty (no archive). Use LogSink/name or a plain name. Router-wide validation requires each LogSink to exist; disabled sinks are excluded at execution. |
| `spec.vacuum` | *Optional* | boolean | Whether to VACUUM the target SQLite files after pruning. Default false. This changes database storage, not retention selection. |
| `spec.schedule` | *Optional* | string; enum=["", "daily"] | Omit or set daily. Both mean the first run and subsequent runs after at least 23 hours. No time-of-day or cron expression is supported. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `targets`, `updatedAt`
