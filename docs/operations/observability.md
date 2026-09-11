# Observability pipeline

![Diagram showing Telemetry, LogSink, and ObservabilityPipeline resources feeding routerd OpenTelemetry SDK signals and routerd event exporter output to OTLP, syslog, stdout, or Loki sinks](/img/diagrams/operations-observability.png)

`Telemetry` remains the small OTLP-only resource for routerd's own metrics,
traces, and logs. `LogSink` describes log forwarding routes for operational
events and observed network logs; an OTLP `LogSink` should reference a
`Telemetry` resource rather than duplicating collector endpoints. Use
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

`kafka` is accepted as documented metadata only so configs can record the
intended external pipeline, but routerd does not publish to Kafka directly yet.

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

| Consumer | Authoritative input | Recovery and limit |
| --- | --- | --- |
| Route and other notification controllers | Current configuration, dynamic parts, and object status | Reread current inputs on an event or periodic reconcile. The IPv4 route controller's maximum normal period is 30 seconds. Duplicate or stale event payloads do not replace its desired state. Other controllers have their own periods. |
| DerivedEvent | Current referenced status | Reevaluate periodically (five seconds by default); intermediate historical transitions cannot be reconstructed. |
| EventRule | Committed event journal | Count, sequence, and window patterns require recorded history. A new consumer starts at the current journal cursor; it does not replay all older history. Rule correlation state is process-local. |
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

Journal consumption is at least once. If processing succeeds but cursor saving
fails, the callback runs again for the same event ID. EventRule emissions and
log sinks may therefore repeat. Successful delivery to multiple sinks is not
atomic, and a cursor is not an exactly-once guarantee for an external API.
The separate [event federation protocol](../reference/event-federation.md) has
its own delivery records and does not use the local status notification queue
as its durable source of truth.
