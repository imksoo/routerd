# 观测管线

`Telemetry` 是一个小型资源，用于将 routerd 自身的指标、追踪与日志输出至 OTLP。`LogSink` 代表运维事件与观测日志的转发路径。OTLP 类型的 `LogSink` 通过引用 `Telemetry` 资源来避免重复填写采集器端点。若需将 routerd 的事件日志传送至 Loki 等管线型远端 sink，请使用 `ObservabilityPipeline`。

`ObservabilityPipeline` 是 routerd 内置的管线，而非随附的 `otelcol` 进程。OTLP 的日志、指标与追踪使用标准 OpenTelemetry SDK 送出，配置的日志 sink 则由轻量事件导出器负责传送。

目前支持的日志 sink 如下：

- `stdout`：JSON 格式的事件行。
- `syslog`：与现有 `LogSink` 相同的 syslog 格式。
- `loki`：通过 HTTP push 至 `/loki/api/v1/push`。

`kafka` 作为元数据接受，以便在配置中保留预期的外部管线意图。
routerd 目前尚未直接发布至 Kafka。

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

OTLP 字段会展开为 routerd 所管理单元的标准 OpenTelemetry 环境变量。日志 sink 的导出器会订阅进程内总线的 `routerd.**` 事件，因此无需 scrape `journalctl`，即可转发控制器状态变更与守护进程事件。

完整示例请参阅 `examples/observability-loki.yaml`。
