# ObservabilityPipeline

[各資源的設定](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

結合受管服務的OpenTelemetry環境宣告與獨立內建操作事件記錄consumer。

## 指定條件與參照關係

- OTLP環境選擇與內建記錄轉送是兩條路徑，signals控制前者，logs.enabled控制後者；輸出清單空白不代表stdout。
- sampling.rate=0轉為1；此實作中的Kafka輸出端與OTLP憑證檔欄位只有宣告，沒有相應執行處理。

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

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.serviceNamespace` | OpenTelemetry服務命名空間，去除前後空白後為空時預設routerd。 |
| `spec.attributes` | 資源屬性對照，預設空；按鍵排序並以逗號分隔key=value寫入OTEL_RESOURCE_ATTRIBUTES，此產生器不跳脫值。 |
| `spec.otlp` | 產生服務用的選填OTLP環境設定；endpoint非空白時選為遙測環境來源，內建記錄輸出端另行運作。 |
| `spec.otlp.endpoint` | 匯出端點預設空；非空白值須可解析為URI，寫入OTEL_EXPORTER_OTLP_ENDPOINT。多個Telemetry或含endpoint的pipeline會使環境產生失敗。 |
| `spec.otlp.insecure` | 預設false；true產生OTEL_EXPORTER_OTLP_INSECURE=true，tls.insecureSkipVerify=true也設定相同變數。 |
| `spec.otlp.headers` | exporter請求標頭對照，預設空；排序後以逗號分隔key=value寫入OTEL_EXPORTER_OTLP_HEADERS，不跳脫值。與Loki標頭不同。 |
| `spec.otlp.tls` | 可解碼的TLS選項；目前只有insecureSkipVerify影響環境產生，三個檔案欄位未接至exporter設定。 |
| `spec.otlp.tls.caFile` | CA檔案宣告預設空，目前產生器與內建exporter不讀取，設定後不會配置信任庫。 |
| `spec.otlp.tls.certFile` | 用戶端憑證宣告預設空，目前OTLP設定路徑不使用。 |
| `spec.otlp.tls.keyFile` | 用戶端金鑰宣告預設空，目前不使用；在此成對設定certFile/keyFile不會啟用雙向TLS。 |
| `spec.otlp.tls.insecureSkipVerify` | 預設false，目前將true轉成OTEL_EXPORTER_OTLP_INSECURE=true，不是只略過憑證檢查的TLS client設定。 |
| `spec.signals` | 產生OpenTelemetry環境時預設logs/metrics/traces全部，非空清單未列出的訊號設exporter=none。內建記錄consumer不使用此清單，改由logs.enabled控制。 |
| `spec.sampling` | 內建事件記錄抽樣設定，獨立於產生的OTLP環境。 |
| `spec.sampling.rate` | 須為0–1；省略/0轉為1，傳送全部符合事件。小數使用序號模1000的確定性抽樣，不是隨機。停止consumer請用logs.enabled=false。 |
| `spec.logs` | 內建DB事件記錄轉送設定，與OTLP環境分開。 |
| `spec.logs.enabled` | 預設true；false停止內建記錄consumer，但不停用產生服務的OTLP變數。 |
| `spec.logs.sinks` | 輸出端清單預設空，沒有內建目的地；不會隱含stdout。Kafka項目即使啟用仍被exporter略過。 |
| `spec.logs.sinks[].name` | 選填輸出標籤，預設空；不是LogSink資源參照，目前exporter不用它選目的地。 |
| `spec.logs.sinks[].type` | 必填stdout、syslog、loki或kafka；kafka只接受宣告而不執行，僅使用所選分支設定。 |
| `spec.logs.sinks[].enabled` | 預設true；false排除此輸出端，kafka另行無條件排除。 |
| `spec.logs.sinks[].minLevel` | 最低嚴重程度預設info，接受debug/info/warning/error，在抽樣後判斷。 |
| `spec.logs.sinks[].labels` | 額外Loki stream標籤預設空；鍵正規化後覆寫同名全域屬性及內建標籤，stdout/syslog不使用。 |
| `spec.logs.sinks[].loki` | 只用於type=loki的設定。 |
| `spec.logs.sinks[].loki.url` | loki時必填且無預設值，須提供完整Loki push URL，exporter不補push路徑；只有HTTP 2xx算成功。 |
| `spec.logs.sinks[].loki.tenant` | 選填租戶ID預設空，非空時設定X-Scope-OrgID。 |
| `spec.logs.sinks[].loki.headers` | 額外HTTP標頭預設空，覆寫同名標頭，包括Content-Type與租戶標頭。 |
| `spec.logs.sinks[].syslog` | 只用於type=syslog的設定。 |
| `spec.logs.sinks[].syslog.network` | 傳輸方式為unix、unixgram、tcp或udp；空值由Go syslog探索本機socket。 |
| `spec.logs.sinks[].syslog.address` | 目的socket路徑或host:port，預設空。明確指定傳輸方式時應提供對應位址；驗證器不強制此組合，連線錯誤在輸出時出現。 |
| `spec.logs.sinks[].syslog.tag` | syslog標籤，預設routerd，不是資源參照。 |
| `spec.logs.sinks[].syslog.facility` | 預設local6，此exporter只對應local0–local7，其他值也改用local6，與獨立LogSink的syslog實作不同。 |
| `spec.logs.sinks[].kafka` | Kafka意圖設定，會驗證但內建exporter略過此輸出端。 |
| `spec.logs.sinks[].kafka.brokers` | type=kafka須至少一個非空白broker，目前exporter不連線。 |
| `spec.logs.sinks[].kafka.topic` | type=kafka必填非空白topic，不是EventRule topic或資源參照，目前不傳送Kafka。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go) · [pkg/observabilitypipeline/exporter.go](https://github.com/imksoo/routerd/blob/main/pkg/observabilitypipeline/exporter.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
