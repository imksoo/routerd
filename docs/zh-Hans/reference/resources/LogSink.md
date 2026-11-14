# LogSink

[各资源的配置](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

按type选择运维事件日志的输出目的地，与ObservabilityPipeline内的日志输出配置不同。

## 指定条件与引用关系

- type选择一种实现，仅使用该分支；其他分支不会因互斥而报错，也不会合并。otlp验证要求telemetryRef与endpoint恰好一项，但当前OTLPSink.Emit不会发送事件。
- journald分支使用Go的log.Printf，是否进入journald取决于进程日志目的地及服务管理器，不直接调用journald API。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ file:
        [ name: <string> ]
    ]
    [ journald:
        [ identifier: <string> ]
    ]
    [ minLevel: ("debug" | "info" | "warning" | "error") ]
    [ otlp:
        [ endpoint: <string> ]
        [ telemetryRef: <string> ]
    ]
    [ syslog:
        [ address: <string> ]
        [ facility: ("kern" | "user" | "mail" | "daemon" | "auth" | "syslog" | "lpr" | "news" | "uucp" | "cron" | "authpriv" | "ftp" | "local0" | "local1" | "local2" | "local3" | "local4" | "local5" | "local6" | "local7") ]
        [ network: ("" | "unix" | "unixgram" | "tcp" | "udp") ]
        [ tag: <string> ]
    ]
    type: ("syslog" | "otlp" | "webhook" | "file" | "journald")
    [ webhook:
        [ headers:
            [ <name>: <string> ]
        ]
        [ timeout: <string> ]
        [ url: <string> ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.type` | **必填** | string; enum=["syslog", "otlp", "webhook", "file", "journald"] | 必填目的地类型：syslog、otlp、webhook、file或journald，无默认值。 |
| `spec.enabled` | *选填* | boolean | 是否选用此输出端，默认true。false时即使LogRetention引用也跳过。 |
| `spec.minLevel` | *选填* | string; enum=["debug", "info", "warning", "error"] | 最低严重程度，默认info；接受debug、info、warning、error，丢弃较低程度事件。 |
| `spec.syslog` | *选填* | object | 仅在type=syslog使用，省略时采用本机syslog、local6与标签routerd。 |
| `spec.syslog.network` | *选填* | string; enum=["", "unix", "unixgram", "tcp", "udp"] | 传输方式为unix、unixgram、tcp或udp；空值由Go syslog查找本机socket。 |
| `spec.syslog.address` | *选填* | string | 目标socket路径或host:port，默认空。显式指定传输方式时应提供对应地址；验证器不强制此组合，连接错误在输出时出现。 |
| `spec.syslog.facility` | *选填* | string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"] | syslog facility，默认local6；可用值见YAML枚举。 |
| `spec.syslog.tag` | *选填* | string | syslog标签，默认routerd，不是资源引用。 |
| `spec.otlp` | **条件必填** | object | OTLP声明；type=otlp时telemetryRef与endpoint恰好一项必填，此输出端尚无实际发送处理。 |
| `spec.otlp.telemetryRef` | **条件必填** | string | Telemetry/名称或纯Telemetry名称，与endpoint互斥，默认空。即使选择其他type，Router整体验证仍检查目标存在。当前OTLPSink不使用它发送。 |
| `spec.otlp.endpoint` | **条件必填** | string | 预定目标URI，与telemetryRef互斥，无默认值；验证URI语法，但此LogSink不创建exporter。 |
| `spec.webhook` | **条件必填** | object | type=webhook使用的JSON HTTP POST目标配置。 |
| `spec.webhook.url` | **条件必填** | string | type=webhook时必填且无默认值。请使用HTTP(S)绝对URL；验证器仅检查URI语法，只有HTTP 2xx视为发送成功。 |
| `spec.webhook.headers` | *选填* | object | 额外请求头，默认空；会覆盖同名头，包括默认Content-Type: application/json。 |
| `spec.webhook.timeout` | *选填* | string | HTTP请求超时，默认5s，使用Go duration。验证器也接受零及负值；HTTP client中不大于零表示不设超时。 |
| `spec.file` | *选填* | object | type=file的文件配置，以JSON Lines追加写入。 |
| `spec.file.name` | *选填* | string | 逻辑文件名，默认routerd-events，路径为/var/lib/routerd/logs/<name>.jsonl。禁止路径分隔符、NUL及换行，不是任意文件路径。 |
| `spec.journald` | *选填* | object | type=journald的配置，通过进程logger输出。 |
| `spec.journald.identifier` | *选填* | string | 加在进程日志消息前的前缀，默认routerd，不直接设置journald的SYSLOG_IDENTIFIER。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `type`
