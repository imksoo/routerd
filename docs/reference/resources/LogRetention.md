# LogRetention

[Resource configuration reference](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Prunes operational-log SQLite records and optionally archives them first. It does not control arbitrary system log files.

## Conditions and references

- Runs on the first reconciliation, then when at least 23 hours have elapsed since lastRunAt. Both omitted schedule and daily use this elapsed-time rule, not a midnight cron schedule. Dry-run does not open or prune external log databases.
- The event journal independently enforces a 24-hour age bound, plus its own row/payload bounds. A longer retention here cannot extend the event journal beyond that bound.
- When sinks are set, each row is sent before deletion. A sink error prevents successful deletion of that row. LogSink type otlp currently sends nothing, so it must not be treated as a working archive destination.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # Required; string
    schedule: "" # optional; string; enum=["", "daily"]
    signals: # optional; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # optional; array
      - "<string>" # string
    vacuum: true # optional; boolean
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.retention` | Required retention interval, e.g. 7d or 24h. d means an integer number of 24-hour days; other values use Go duration syntax. No default. The validator parses the duration but does not require it to be positive. Use a positive interval for retention. |
| `spec.signals` | Log categories to prune. Default all seven: events, accessLogs, pluginRuns, dnsQueries, trafficFlows, firewallEvents, dhcpFingerprints. Storage files are resolved from the relevant logging resources or platform defaults. |
| `spec.sinks` | Archive destinations before deletion, default empty (no archive). Use LogSink/name or a plain name. Router-wide validation requires each LogSink to exist; disabled sinks are excluded at execution. |
| `spec.vacuum` | Whether to VACUUM the target SQLite files after pruning. Default false. This changes database storage, not retention selection. |
| `spec.schedule` | Omit or set daily. Both mean the first run and subsequent runs after at least 23 hours. No time-of-day or cron expression is supported. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `targets`, `updatedAt`
