# Observability pipeline

![Diagram showing Telemetry, LogSink, and ObservabilityPipeline resources feeding routerd OpenTelemetry SDK signals and routerd event exporter output to OTLP, syslog, stdout, or Loki sinks](/img/diagrams/operations-observability.png)

`Telemetry` remains the small OTLP-only resource for routerd's own metrics,
traces, and logs. `LogSink` describes log forwarding routes for operational
events and observed network logs. OTLP export uses `Telemetry` or the OTLP
fields of `ObservabilityPipeline`; an `otlp` LogSink is rejected because it
has no delivery implementation. Use
`ObservabilityPipeline` when the router should also forward routerd event logs
to pipeline-style remote sinks such as Loki.

`ObservabilityPipeline` is a built-in pipeline, not a bundled `otelcol`
process. routerd still uses the normal OpenTelemetry SDK for OTLP logs,
metrics, and traces, and it starts a lightweight event exporter for configured
log sinks.

Supported log sinks today:

- `stdout`: JSON event lines, useful for supervised service logs.
- `syslog`: local or remote syslog using the existing `LogSink` syslog shape.
- `loki`: HTTP push to `/loki/api/v1/push`.

`kafka` is rejected because routerd has no direct Kafka publisher. The 1.0
implementation skipped these sink entries rather than delivering their events.

Example:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ObservabilityPipeline
metadata:
  name: remote-observability
spec:
  otlp:
    endpoint: http://otel-collector.lan:4317
    insecure: true
    headers:
      authorization: Bearer example-token
  serviceNamespace: routerd
  attributes:
    site: edge
  signals: [logs, metrics, traces]
  sampling:
    rate: 1
  logs:
    sinks:
      - name: loki
        type: loki
        minLevel: info
        loki:
          url: http://loki.lan:3100/loki/api/v1/push
          tenant: routerd
```

The OTLP fields render to the standard OpenTelemetry environment variables for
routerd-managed units. The log sink exporter subscribes to `routerd.**` events
on the in-process bus, so it forwards controller status changes and daemon
events without scraping `journalctl`.

Sampling is deterministic per pipeline and applies before sink fan-out. Keep it
at `1` for operational event logs unless a high-volume source is intentionally
being downsampled.

See `examples/observability-loki.yaml` for a complete config.

## Event delivery and recovery contracts

The local bus uses bounded subscriber queues. A full queue drops wakeups; it
does not remove events already committed to SQLite. The exporter and EventRule
engine use the bus only to wake their journal drains, with a default one-second
poll when no wakeup arrives. Failed drains back off from one second to one minute.
Polling periods exclude execution time, database outages, and a stalled process.
A pipeline initializes its durable cursor before acknowledging startup. A journal
initialization error prevents `Running`; successful startup does not by itself
mean that a remote destination has accepted a log.

| Consumer | Authoritative input | Recovery and limit |
| --- | --- | --- |
| Route and other notification controllers | Current configuration, dynamic parts, and object status | Reread current inputs on an event or periodic reconcile. The IPv4 route controller's maximum normal period is 30 seconds. Duplicate or stale event payloads do not replace its desired state. Other controllers have their own periods. |
| DerivedEvent | Current referenced status | Reevaluate periodically (five seconds by default); intermediate historical transitions cannot be reconstructed. |
| EventRule | Committed event journal and durable per-rule checkpoint | A new or changed rule starts at the journal position captured during configuration preparation. An unchanged rule resumes its count, sequence, window, throttle and pending timers after restart. Missing retained input history stops that rule with an error; it cannot establish an absence. |
| ObservabilityPipeline | Committed event journal | Retry stored events; sampling and sink severity filters still apply. This is not a complete audit log of every attempted status change. |
| Provider action executor | ActionExecution journal and stable idempotency key | A durably succeeded action is skipped after retry/restart. A remote success before local outcome recording still requires executor idempotency or observation of provider state. |

When a configured EventRule pattern matches `routerd.resource.status.changed`,
the standard SQLite-backed status store commits each meaningful status change
and its event in one transaction. A journal failure rolls back that status write,
so a producer retry can record the transition. Local delivery happens after
commit using the same event ID. A crash before local delivery is recovered by
journal polling. The status store and bus must share this transactional store;
an unsupported or mismatched store returns an error before saving the status.
Rules for unrelated topics and configurations without such rules do not require
this extra store capability.

Freshness checks and transition attributes use the transaction's current status,
including fields retained by a concurrent merge. An identical status in a new
generation refreshes its observed-generation metadata without emitting a new
transition event.

Without a matching history-consuming EventRule, status notifications retain
their existing contract: status can commit before event persistence fails,
the error is returned, and local delivery is still attempted. Saving an identical
status later does not reconstruct missing history. Current-state consumers can
recover by rescanning; a log exporter cannot recover an event that never reached
the journal. The transaction guarantee above applies specifically to status
transition events, not every daemon or derived-event producer.

Each EventRule commits its correlation state, input cursor, public status and
resulting local events in one SQLite transaction. A failed transaction advances
none of them. After a lost acknowledgement, rereading the checkpoint prevents
repeating an already committed result. Bus notification follows the commit;
journal consumers can recover a missed notification. This guarantee applies to
the local journal, not an external API or log sink. A log sink can repeat an
event when delivery succeeds before its cursor is saved, and delivery to
multiple sinks is not atomic.

EventRule supports `all_of`, `any_of`, `sequence`, `window`, `absence`,
`throttle`, `debounce` and `count`. Use `interval` for the throttle duration;
nonzero `rate` is rejected because it has no implemented semantics. Correlations
are bounded to 4096 keys and an 8 MiB serialized checkpoint per rule. When either
bound is exceeded, the oldest keys by last-seen time are evicted until the
checkpoint fits, and `status.warningCount` increases in that same transaction.
Patterns spanning an evicted key cannot be completed. Configuration
selection initializes the active rules and retires removed checkpoints; preparing
an unselected candidate does not change consumption state.
The separate [event federation protocol](../reference/event-federation.md) has
its own delivery records and does not use the local status notification queue
as its durable source of truth.

## Log retention archives

`LogRetention.sinks` selects the `LogSink` destinations that must accept each
expired record before its local transaction can delete it. Missing, disabled or
severity-filtered selected sinks fail the run and preserve the local rows.
Runtime log-level overrides affect operational messages, not required archive
records.
For `events`, `accessLogs` and `pluginRuns`, retention uses the opened state
database selected by `--state-file`, including a nondefault path. Its raw data
source is separate from the generation's evented status adapter. An unavailable
state source stops the run rather than selecting the platform's default file.
Only registered operational-log tables are pruned; configuration and lease state
are outside this policy.

The opened state file's identity is checked at the separate retention connection
and before deletion, commit and vacuum. A same-named replacement is an ownership
error, not a new retention target. Relative SQLite filenames resolve when opened;
`:memory:` retains its database semantics and cannot authorize file retention.

An explicit `events` retention policy owns idle expiry, including its archive
selection and schedule. Without one, idle cleanup uses the 24-hour default.
The state writer independently enforces its existing safety bounds: 24 hours,
100,000 rows and 64 MiB of logical payload. These limits take precedence over
forensic retention and can evict history before an exporter or archive consumes
it. An event policy requesting more than 24 hours is `Degraded` with reason
`EventJournalSafetyBound`. Event policy status declares `archiveScope: retainedRows`
and discloses these limits. It does not claim a complete or lossless audit archive.
 A sink error rolls back deletion; retry can deliver a record again if
an earlier destination already accepted it. Delivery to multiple sinks is not
atomic, and syslog transport success is not an application-level storage receipt.

Relevant local checks are:

```sh
go test ./pkg/eventlog ./pkg/logstore ./pkg/observabilitypipeline
go test ./pkg/controller/chain -run '^(TestLogRetention|TestRetention|TestReviewArchive)'
```
