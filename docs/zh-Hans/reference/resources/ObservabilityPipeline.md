# ObservabilityPipeline

[各资源的配置](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 选填; object
      "<key>": "<string>" # 选填; string
    logs: # 选填; object
      enabled: true # 选填; boolean
      sinks: # 选填; array
        - # object
          enabled: true # 选填; boolean
          kafka: # 选填; object
            brokers: # 选填; array
              - "<string>" # string
            topic: "<string>" # 选填; string
          labels: # 选填; object
            "<key>": "<string>" # 选填; string
          loki: # 选填; object
            headers: # 选填; object
              "<key>": "<string>" # 选填; string
            tenant: "<string>" # 选填; string
            url: "<string>" # 选填; string
          minLevel: "debug" # 选填; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 选填; string
          syslog: # 选填; object
            address: "<string>" # 选填; string
            facility: "kern" # 选填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 选填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 选填; string
          type: "stdout" # 必填; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 选填; object
      endpoint: "<string>" # 选填; string
      headers: # 选填; object
        "<key>": "<string>" # 选填; string
      insecure: true # 选填; boolean
      tls: # 选填; object
        caFile: "<string>" # 选填; string
        certFile: "<string>" # 选填; string
        insecureSkipVerify: true # 选填; boolean
        keyFile: "<string>" # 选填; string
    sampling: # 选填; object
      rate: 0 # 选填; number
    serviceNamespace: "<string>" # 选填; string
    signals: # 选填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
