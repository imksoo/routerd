# 觀測管線

`Telemetry` 是一個小型資源，用於將 routerd 自身的指標、追蹤與日誌輸出至 OTLP。`LogSink` 代表運維事件與觀測日誌的轉發路徑。OTLP 類型的 `LogSink` 透過參照 `Telemetry` 資源來避免重複填寫收集器端點。若需將 routerd 的事件日誌傳送至 Loki 等管線型遠端 sink，請使用 `ObservabilityPipeline`。

`ObservabilityPipeline` 是 routerd 內建的管線，而非隨附的 `otelcol` 程序。OTLP 的日誌、指標與追蹤使用標準 OpenTelemetry SDK 送出，設定的日誌 sink 則由輕量事件匯出器負責傳送。

目前支援的日誌 sink 如下：

- `stdout`：JSON 格式的事件行。
- `syslog`：與現有 `LogSink` 相同的 syslog 格式。
- `loki`：透過 HTTP push 至 `/loki/api/v1/push`。

`kafka` 作為中繼資料接受，以便在設定中保留預期的外部管線意圖。
routerd 目前尚未直接發布至 Kafka。

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

OTLP 欄位會展開為 routerd 所管理單元的標準 OpenTelemetry 環境變數。日誌 sink 的匯出器會訂閱程序內匯流排的 `routerd.**` 事件，因此無需 scrape `journalctl`，即可轉發控制器狀態變更與常駐程式事件。

完整範例請參閱 `examples/observability-loki.yaml`。
