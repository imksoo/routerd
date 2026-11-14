# LogSink

[各资源的配置](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

按type选择运维事件日志的输出目的地，与ObservabilityPipeline内的日志输出配置不同。

## 指定条件与引用关系

- type选择一种实现，仅使用该分支；其他分支不会因互斥而报错，也不会合并。otlp验证要求telemetryRef与endpoint恰好一项，但当前OTLPSink.Emit不会发送事件。
- journald分支使用Go的log.Printf，是否进入journald取决于进程日志目的地及服务管理器，不直接调用journald API。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    file: # 选填; object
      name: "<string>" # 选填; string
    journald: # 选填; object
      identifier: "<string>" # 选填; string
    minLevel: "debug" # 选填; string; enum=["debug", "info", "warning", "error"]
    otlp: # 选填; object
      endpoint: "<string>" # 选填; string
      telemetryRef: "<string>" # 选填; string
    syslog: # 选填; object
      address: "<string>" # 选填; string
      facility: "kern" # 选填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 选填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 选填; string
    type: "syslog" # 必填; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 选填; object
      headers: # 选填; object
        "<key>": "<string>" # 选填; string
      timeout: "<string>" # 选填; string
      url: "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.type` | 必填目的地类型：syslog、otlp、webhook、file或journald，无默认值。 |
| `spec.enabled` | 是否选用此输出端，默认true。false时即使LogRetention引用也跳过。 |
| `spec.minLevel` | 最低严重程度，默认info；接受debug、info、warning、error，丢弃较低程度事件。 |
| `spec.syslog` | 仅在type=syslog使用，省略时采用本机syslog、local6与标签routerd。 |
| `spec.syslog.network` | 传输方式为unix、unixgram、tcp或udp；空值由Go syslog查找本机socket。 |
| `spec.syslog.address` | 目标socket路径或host:port，默认空。显式指定传输方式时应提供对应地址；验证器不强制此组合，连接错误在输出时出现。 |
| `spec.syslog.facility` | syslog facility，默认local6；可用值见YAML枚举。 |
| `spec.syslog.tag` | syslog标签，默认routerd，不是资源引用。 |
| `spec.otlp` | OTLP声明；type=otlp时telemetryRef与endpoint恰好一项必填，此输出端尚无实际发送处理。 |
| `spec.otlp.telemetryRef` | Telemetry/名称或纯Telemetry名称，与endpoint互斥，默认空。即使选择其他type，Router整体验证仍检查目标存在。当前OTLPSink不使用它发送。 |
| `spec.otlp.endpoint` | 预定目标URI，与telemetryRef互斥，无默认值；验证URI语法，但此LogSink不创建exporter。 |
| `spec.webhook` | type=webhook使用的JSON HTTP POST目标配置。 |
| `spec.webhook.url` | type=webhook时必填且无默认值。请使用HTTP(S)绝对URL；验证器仅检查URI语法，只有HTTP 2xx视为发送成功。 |
| `spec.webhook.headers` | 额外请求头，默认空；会覆盖同名头，包括默认Content-Type: application/json。 |
| `spec.webhook.timeout` | HTTP请求超时，默认5s，使用Go duration。验证器也接受零及负值；HTTP client中不大于零表示不设超时。 |
| `spec.file` | type=file的文件配置，以JSON Lines追加写入。 |
| `spec.file.name` | 逻辑文件名，默认routerd-events，路径为/var/lib/routerd/logs/<name>.jsonl。禁止路径分隔符、NUL及换行，不是任意文件路径。 |
| `spec.journald` | type=journald的配置，通过进程logger输出。 |
| `spec.journald.identifier` | 加在进程日志消息前的前缀，默认routerd，不直接设置journald的SYSLOG_IDENTIFIER。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `type`
