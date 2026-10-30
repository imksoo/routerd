# 观测管线

![Diagram showing Telemetry, LogSink, and ObservabilityPipeline resources feeding routerd OpenTelemetry SDK signals and routerd event exporter output to OTLP, syslog, stdout, or Loki sinks](/img/diagrams/operations-observability.png)

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

## 事件投递与恢复契约

进程内总线的订阅队列有容量上限；队列满时丢弃唤醒通知，不会删除已提交至 SQLite 的事件。导出器和 EventRule 引擎仅用总线通知唤醒历史读取；没有通知时默认每秒轮询，读取失败后按一秒至一分钟的间隔退避重试。这些周期不包含执行耗时、数据库故障和进程停顿。

| 消费者 | 权威输入 | 恢复方式与限制 |
| --- | --- | --- |
| EventRule | 已提交的事件历史及每条规则的持久化 checkpoint | 新规则或已修改的规则从配置准备时记录的历史位置开始；未修改的规则在重启后恢复 count、sequence、window、throttle 和待执行的 timer。若保留策略已删除未读输入，则该规则报错停止，不能据此判定事件不存在。 |
| 日志 sink | 已提交的事件历史及配送 cursor | 失败时重试；配送成功后若 cursor 保存失败，可能重复配送。sampling 和 severity filter 仍然适用。 |

每条 EventRule 在同一 SQLite 事务中提交关联状态、唯一的规则输入 cursor、公开 status 和由此产生的本地输出事件。事务失败时，这些状态均不推进。若提交确认丢失，重新读取 checkpoint 可避免重复生成已提交的结果。总线通知在提交后发送；消费者可通过历史读取恢复遗漏的通知。这一保证仅适用于本地事件历史；外部 API 或日志 sink 的配送仍可能重复（at-least-once），多个 sink 之间的配送也不是原子的。

当 EventRule 的 pattern 匹配 `routerd.resource.status.changed` 时，标准 SQLite status store 将有意义的 status 变更及其事件作为同一事务保存。事件历史写入失败会回滚 status 写入，提交后再以同一事件 ID 本地投递。status store 和总线必须连接到同一事务存储；不支持该能力或存储不一致时，在保存 status 前报错。没有匹配规则时，不适用此额外保证；不能将其推广到所有守护进程或 derived-event producer。

EventRule 支持 `all_of`、`any_of`、`sequence`、`window`、`absence`、`throttle`、`debounce` 和 `count`。throttle 的时长使用 `interval`；非零 `rate` 没有已实现的语义，因此会被拒绝。每条规则最多保留4096个关联键，序列化后的 checkpoint 上限为8 MiB。超过任一上限时，按最后匹配输入的时间从旧到新淘汰关联键，直到满足上限。跨越已淘汰键的模式无法完成。每次淘汰都会增加 `status.warningCount`，并与 checkpoint 在同一事务中公开。选用配置时初始化有效规则并移除已删除规则的 checkpoint；仅准备未选用的候选配置不会改变消费状态。

[Event federation](../reference/event-federation.md) 使用独立的投递记录，不把本地 status 通知队列作为持久化权威来源。
