# 観測パイプライン

`Telemetry` は、routerd 自身のメトリクス・トレース・ログを OTLP へ出すための小さなリソースです。`LogSink` は、運用イベントや観測ログの転送経路を表します。OTLP の `LogSink` は、コレクターのエンドポイントを重複して書かず、`Telemetry` リソースを参照します。routerd のイベントログを Loki などパイプライン型のリモート sink に送りたい場合は、`ObservabilityPipeline` を使います。

`ObservabilityPipeline` は、同梱の `otelcol` プロセスではなく、routerd 内蔵のパイプラインです。OTLP のログ・メトリクス・トレースは通常の OpenTelemetry SDK を使い、設定したログ sink には軽量なイベントエクスポーターが送信します。

現在のログ sink は次のとおりです。

- `stdout`: JSON 形式のイベント行。
- `syslog`: 既存の `LogSink` と同じ syslog 形式。
- `loki`: `/loki/api/v1/push` への HTTP push。

`kafka` は、意図する外部パイプラインを設定に残すためのメタデータとして受け付けます。
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

OTLP のフィールドは、routerd が管理するユニットの標準的な OpenTelemetry 環境変数に展開されます。ログ sink のエクスポーターはプロセス内バスの `routerd.**` イベントを購読するため、`journalctl` を scrape せずに、コントローラーの状態変化やデーモンのイベントを転送できます。

完全な例は `examples/observability-loki.yaml` にあります。
