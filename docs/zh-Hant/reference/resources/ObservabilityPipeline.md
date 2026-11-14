# ObservabilityPipeline

[各資源的設定](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 選填; object
      "<key>": "<string>" # 選填; string
    logs: # 選填; object
      enabled: true # 選填; boolean
      sinks: # 選填; array
        - # object
          enabled: true # 選填; boolean
          kafka: # 選填; object
            brokers: # 選填; array
              - "<string>" # string
            topic: "<string>" # 選填; string
          labels: # 選填; object
            "<key>": "<string>" # 選填; string
          loki: # 選填; object
            headers: # 選填; object
              "<key>": "<string>" # 選填; string
            tenant: "<string>" # 選填; string
            url: "<string>" # 選填; string
          minLevel: "debug" # 選填; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 選填; string
          syslog: # 選填; object
            address: "<string>" # 選填; string
            facility: "kern" # 選填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 選填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 選填; string
          type: "stdout" # 必填; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 選填; object
      endpoint: "<string>" # 選填; string
      headers: # 選填; object
        "<key>": "<string>" # 選填; string
      insecure: true # 選填; boolean
      tls: # 選填; object
        caFile: "<string>" # 選填; string
        certFile: "<string>" # 選填; string
        insecureSkipVerify: true # 選填; boolean
        keyFile: "<string>" # 選填; string
    sampling: # 選填; object
      rate: 0 # 選填; number
    serviceNamespace: "<string>" # 選填; string
    signals: # 選填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
