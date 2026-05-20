# Observability pipeline

`Telemetry` は routerd 自身の metrics / traces / logs を OTLP へ出す小さな
resource です。`LogSink` は operational event や観測ログの転送経路を表します。
OTLP の `LogSink` は collector endpoint を重複して書かず、`Telemetry` resource を
参照します。routerd の event log を Loki など pipeline 型の遠隔 sink に送りたい場合は
`ObservabilityPipeline` を使います。

`ObservabilityPipeline` は bundled `otelcol` process ではなく、routerd 内蔵の
pipeline です。OTLP logs / metrics / traces は通常の OpenTelemetry SDK を使い、
設定された log sink には軽量 event exporter が送信します。

現在の log sink:

- `stdout`: JSON event line。
- `syslog`: 既存 `LogSink` と同じ syslog shape。
- `loki`: `/loki/api/v1/push` への HTTP push。

`kafka` は意図する外部 pipeline を config に残すための metadata として受け付けます。
routerd はまだ Kafka へ直接 publish しません。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ObservabilityPipeline
metadata:
  name: remote-observability
spec:
  otlp:
    endpoint: http://otel-collector.lan:4317
    insecure: true
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

OTLP field は routerd-managed unit の標準 OpenTelemetry environment variable に
描画されます。log sink exporter は process 内 bus の `routerd.**` event を購読するため、
`journalctl` を scrape せずに controller status change と daemon event を転送できます。

`examples/observability-loki.yaml` に完全な例があります。
