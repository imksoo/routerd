# LogRetention

[各资源的配置](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

清理运维日志的SQLite记录，并可在删除前转发保存；不管理任意OS日志文件。

## 指定条件与引用关系

- 首次协调执行，之后距lastRunAt至少23小时才再次执行。省略schedule与daily都按经过时间判断，不是午夜cron；dry-run不开启或清理外部日志DB。
- 事件日志另有24小时及记录数、容量上限；此处设置更长时间不会延长事件日志上限。
- 指定sinks时，每条记录删除前先转发；保存端错误会阻止该条记录成功删除。当前LogSink的otlp不会发送，不能作为有效保存目的地。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必填; string
    schedule: "" # 选填; string; enum=["", "daily"]
    signals: # 选填; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 选填; array
      - "<string>" # string
    vacuum: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.retention` | 保留期限，必填且无默认值，例如7d或24h。d为整数天数乘24小时，其他使用Go duration。验证器仅解析，不要求正值。 保留期限请使用正值。 |
| `spec.signals` | 要清理的日志类别；默认包含events、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprints全部七种。DB路径来自相关日志资源或OS默认值。 |
| `spec.sinks` | 删除前保存目的地，默认空表示不保存。可用LogSink/名称或纯名称；Router整体验证要求目标存在，执行时排除禁用输出端。 |
| `spec.vacuum` | 删除后是否对目标SQLite执行VACUUM；默认false，用于整理DB空间而非选择保留对象。 |
| `spec.schedule` | 省略或daily；均为首次及之后至少23小时才执行。不支持时刻或cron表达式。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)
