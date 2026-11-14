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
| `spec.retention` | Required retention interval, e.g. 7d or 24h. d means an integer number of 24-hour days; other values use Go duration syntax. No default. The validator parses the duration but does not require it to be positive. |
| `spec.signals` | Log categories to prune. Default all seven: events, accessLogs, pluginRuns, dnsQueries, trafficFlows, firewallEvents, dhcpFingerprints. Storage files are resolved from the relevant logging resources or platform defaults. |
| `spec.sinks` | Archive destinations before deletion. Default empty: no archive. Entries accept LogSink/name or plain name and select enabled system.routerd.net/v1alpha1 LogSink resources. Missing names are silently unselected; check names carefully. |
| `spec.vacuum` | Whether to VACUUM the target SQLite files after pruning. Default false. This changes database storage, not retention selection. |
| `spec.schedule` | Omit or set daily. Both mean the first run and subsequent runs after at least 23 hours. No time-of-day or cron expression is supported. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go)
