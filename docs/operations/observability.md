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
