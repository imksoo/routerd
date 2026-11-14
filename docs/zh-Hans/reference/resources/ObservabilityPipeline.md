# ObservabilityPipeline

[各资源的配置](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

结合受管服务的OpenTelemetry环境声明与独立内置运维事件日志consumer。

## 指定条件与引用关系

- OTLP环境选择与内置日志转发是两条路径，signals控制前者，logs.enabled控制后者；输出列表为空不代表stdout。
- sampling.rate=0转为1；此实现中的Kafka输出端与OTLP证书文件字段仅有声明，没有对应执行处理。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    [ attributes:
        [ <name>: <string> ]
    ]
    [ logs:
        [ enabled: <boolean> ]
        [ sinks:
            -
              [ enabled: <boolean> ]
              [ kafka:
                  [ brokers:
                      - <string>
                  ]
                  [ topic: <string> ]
              ]
              [ labels:
                  [ <name>: <string> ]
              ]
              [ loki:
                  [ headers:
                      [ <name>: <string> ]
                  ]
                  [ tenant: <string> ]
                  [ url: <string> ]
              ]
              [ minLevel: ("debug" | "info" | "warning" | "error") ]
              [ name: <string> ]
              [ syslog:
                  [ address: <string> ]
                  [ facility: ("kern" | "user" | "mail" | "daemon" | "auth" | "syslog" | "lpr" | "news" | "uucp" | "cron" | "authpriv" | "ftp" | "local0" | "local1" | "local2" | "local3" | "local4" | "local5" | "local6" | "local7") ]
                  [ network: ("" | "unix" | "unixgram" | "tcp" | "udp") ]
                  [ tag: <string> ]
              ]
              type: ("stdout" | "syslog" | "loki" | "kafka")
        ]
    ]
    [ otlp:
        [ endpoint: <string> ]
        [ headers:
            [ <name>: <string> ]
        ]
        [ insecure: <boolean> ]
        [ tls:
            [ caFile: <string> ]
            [ certFile: <string> ]
            [ insecureSkipVerify: <boolean> ]
            [ keyFile: <string> ]
        ]
    ]
    [ sampling:
        [ rate: <number> ]
    ]
    [ serviceNamespace: <string> ]
    [ signals:
        - ("logs" | "metrics" | "traces")
    ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.serviceNamespace` | *选填* | string | OpenTelemetry服务命名空间，去除首尾空白后为空时默认routerd。 |
| `spec.attributes` | *选填* | object | 资源属性映射，默认空；按键排序并以逗号分隔key=value写入OTEL_RESOURCE_ATTRIBUTES，此生成器不转义值。 |
| `spec.otlp` | *选填* | object | 生成服务用的选填OTLP环境配置；endpoint非空白时选为遥测环境来源，内置日志输出端独立运行。 |
| `spec.otlp.endpoint` | *选填* | string | 导出端点默认空；非空白值须可解析为URI，写入OTEL_EXPORTER_OTLP_ENDPOINT。多个Telemetry或含endpoint的pipeline会使环境生成失败。 |
| `spec.otlp.insecure` | *选填* | boolean | 默认false；true生成OTEL_EXPORTER_OTLP_INSECURE=true，tls.insecureSkipVerify=true也设置相同变量。 |
| `spec.otlp.headers` | *选填* | object | exporter请求头映射，默认空；排序后以逗号分隔key=value写入OTEL_EXPORTER_OTLP_HEADERS，不转义值。与Loki请求头不同。 |
| `spec.otlp.tls` | *选填* | object | 可解码的TLS选项；当前仅insecureSkipVerify影响环境生成，三个文件字段未接入exporter配置。 |
| `spec.otlp.tls.caFile` | *选填* | string | CA文件声明默认空，当前生成器与内置exporter不读取，设置后不会配置信任库。 |
| `spec.otlp.tls.certFile` | *选填* | string | 客户端证书声明默认空，当前OTLP配置路径不使用。 |
| `spec.otlp.tls.keyFile` | *选填* | string | 客户端密钥声明默认空，当前不使用；在此成对设置certFile/keyFile不会启用双向TLS。 |
| `spec.otlp.tls.insecureSkipVerify` | *选填* | boolean | 默认false，当前将true转为OTEL_EXPORTER_OTLP_INSECURE=true，不是仅跳过证书检查的TLS client配置。 |
| `spec.signals` | *选填* | array | 生成OpenTelemetry环境时默认logs/metrics/traces全部，非空列表未列出的信号设exporter=none。内置日志consumer不使用此列表，改由logs.enabled控制。 |
| `spec.sampling` | *选填* | object | 内置事件日志采样配置，独立于生成的OTLP环境。 |
| `spec.sampling.rate` | *选填* | number | 须为0–1；省略/0转为1，发送全部符合事件。小数使用序号模1000的确定性采样，不是随机。停止consumer请用logs.enabled=false。 |
| `spec.logs` | *选填* | object | 内置DB事件日志转发配置，与OTLP环境分开。 |
| `spec.logs.enabled` | *选填* | boolean | 默认true；false停止内置日志consumer，但不禁用生成服务的OTLP变量。 |
| `spec.logs.sinks` | *选填* | array | 输出端列表默认空，没有内置目的地；不会隐含stdout。Kafka条目即使启用仍被exporter跳过。 |
| `spec.logs.sinks[].name` | *选填* | string | 选填输出标签，默认空；不是LogSink资源引用，当前exporter不用它选择目的地。 |
| `spec.logs.sinks[].type` | **条件必填** | string; enum=["stdout", "syslog", "loki", "kafka"] | 必填stdout、syslog、loki或kafka；kafka仅接受声明而不执行，仅使用所选分支配置。 |
| `spec.logs.sinks[].enabled` | *选填* | boolean | 默认true；false排除此输出端，kafka另行无条件排除。 |
| `spec.logs.sinks[].minLevel` | *选填* | string; enum=["debug", "info", "warning", "error"] | 最低严重程度默认info，接受debug/info/warning/error，在采样后判断。 |
| `spec.logs.sinks[].labels` | *选填* | object | 额外Loki stream标签默认空；键规范化后覆盖同名全局属性及内置标签，stdout/syslog不使用。 |
| `spec.logs.sinks[].loki` | *选填* | object | 仅用于type=loki的配置。 |
| `spec.logs.sinks[].loki.url` | *选填* | string | loki时必填且无默认值，须提供完整Loki push URL，exporter不补push路径；只有HTTP 2xx算成功。 |
| `spec.logs.sinks[].loki.tenant` | *选填* | string | 选填租户ID默认空，非空时设置X-Scope-OrgID。 |
| `spec.logs.sinks[].loki.headers` | *选填* | object | 额外HTTP请求头默认空，覆盖同名请求头，包括Content-Type与租户头。 |
| `spec.logs.sinks[].syslog` | *选填* | object | 仅用于type=syslog的配置。 |
| `spec.logs.sinks[].syslog.network` | *选填* | string; enum=["", "unix", "unixgram", "tcp", "udp"] | 传输方式为unix、unixgram、tcp或udp；空值由Go syslog查找本机socket。 |
| `spec.logs.sinks[].syslog.address` | *选填* | string | 目标socket路径或host:port，默认空。显式指定传输方式时应提供对应地址；验证器不强制此组合，连接错误在输出时出现。 |
| `spec.logs.sinks[].syslog.tag` | *选填* | string | syslog标签，默认routerd，不是资源引用。 |
| `spec.logs.sinks[].syslog.facility` | *选填* | string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"] | 默认local6，此exporter仅映射local0–local7，其他值也改用local6，与独立LogSink的syslog实现不同。 |
| `spec.logs.sinks[].kafka` | *选填* | object | Kafka意图配置，会验证但内置exporter跳过此输出端。 |
| `spec.logs.sinks[].kafka.brokers` | *选填* | array | type=kafka须至少一个非空白broker，当前exporter不连接。 |
| `spec.logs.sinks[].kafka.topic` | *选填* | string | type=kafka必填非空白topic，不是EventRule topic或资源引用，当前不发送Kafka。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go) · [pkg/observabilitypipeline/exporter.go](https://github.com/imksoo/routerd/blob/main/pkg/observabilitypipeline/exporter.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `signals`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
