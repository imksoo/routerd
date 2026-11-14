# ObservabilityPipeline

[リソース別の設定](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    logs: # 任意; object
      enabled: true # 任意; boolean
      sinks: # 任意; array
        - # object
          enabled: true # 任意; boolean
          kafka: # 任意; object
            brokers: # 任意; array
              - "<string>" # string
            topic: "<string>" # 任意; string
          labels: # 任意; object
            "<key>": "<string>" # 任意; string
          loki: # 任意; object
            headers: # 任意; object
              "<key>": "<string>" # 任意; string
            tenant: "<string>" # 任意; string
            url: "<string>" # 任意; string
          minLevel: "debug" # 任意; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 任意; string
          syslog: # 任意; object
            address: "<string>" # 任意; string
            facility: "kern" # 任意; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 任意; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 任意; string
          type: "stdout" # 必須; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 任意; object
      endpoint: "<string>" # 任意; string
      headers: # 任意; object
        "<key>": "<string>" # 任意; string
      insecure: true # 任意; boolean
      tls: # 任意; object
        caFile: "<string>" # 任意; string
        certFile: "<string>" # 任意; string
        insecureSkipVerify: true # 任意; boolean
        keyFile: "<string>" # 任意; string
    sampling: # 任意; object
      rate: 0 # 任意; number
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
