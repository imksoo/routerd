# LogRetention

[各资源的配置](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

清理运维日志的SQLite记录，并可在删除前转发保存；不管理任意OS日志文件。

## 指定条件与引用关系

- 首次协调执行，之后距lastRunAt至少23小时才再次执行。省略schedule与daily都按经过时间判断，不是午夜cron；dry-run不开启或清理外部日志DB。
- 事件日志另有24小时及记录数、容量上限；此处设置更长时间不会延长事件日志上限。
- 指定sinks时，每条记录删除前先转发；保存端错误会阻止该条记录成功删除。当前LogSink的otlp不会发送，不能作为有效保存目的地。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: <string>
    [ schedule: ("" | "daily") ]
    [ signals:
        - ("events" | "accessLogs" | "pluginRuns" | "dnsQueries" | "trafficFlows" | "firewallEvents" | "dhcpFingerprints")
    ]
    [ sinks:
        - <string>
    ]
    [ vacuum: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.retention` | **必填** | string | 保留期限，必填且无默认值，例如7d或24h。d为整数天数乘24小时，其他使用Go duration。验证器仅解析，不要求正值。 保留期限请使用正值。 |
| `spec.signals` | *选填* | array | 要清理的日志类别；默认包含events、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprints全部七种。DB路径来自相关日志资源或OS默认值。 |
| `spec.sinks` | *选填* | array | 删除前保存目的地，默认空表示不保存。可用LogSink/名称或纯名称；Router整体验证要求目标存在，执行时排除禁用输出端。 |
| `spec.vacuum` | *选填* | boolean | 删除后是否对目标SQLite执行VACUUM；默认false，用于整理DB空间而非选择保留对象。 |
| `spec.schedule` | *选填* | string; enum=["", "daily"] | 省略或daily；均为首次及之后至少23小时才执行。不支持时刻或cron表达式。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `targets`, `updatedAt`
