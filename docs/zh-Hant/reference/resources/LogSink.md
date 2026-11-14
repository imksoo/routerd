# LogSink

[各資源的設定](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

依type選擇操作事件記錄的輸出目的地，與ObservabilityPipeline內的記錄輸出設定不同。

## 指定條件與參照關係

- type選擇一種實作，只使用該分支；其他分支不會因互斥而報錯，也不會合併。otlp驗證要求telemetryRef與endpoint恰好一項，但目前OTLPSink.Emit不會傳送事件。
- journald分支使用Go的log.Printf，是否進入journald取決於程序記錄目的地及服務管理器，不直接呼叫journald API。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # 選填; boolean
    file: # 選填; object
      name: "<string>" # 選填; string
    journald: # 選填; object
      identifier: "<string>" # 選填; string
    minLevel: "debug" # 選填; string; enum=["debug", "info", "warning", "error"]
    otlp: # 選填; object
      endpoint: "<string>" # 選填; string
      telemetryRef: "<string>" # 選填; string
    syslog: # 選填; object
      address: "<string>" # 選填; string
      facility: "kern" # 選填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 選填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 選填; string
    type: "syslog" # 必填; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 選填; object
      headers: # 選填; object
        "<key>": "<string>" # 選填; string
      timeout: "<string>" # 選填; string
      url: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.type` | 必填目的地類型：syslog、otlp、webhook、file或journald，無預設值。 |
| `spec.enabled` | 是否選用此輸出端，預設true。false時即使LogRetention參照也略過。 |
| `spec.minLevel` | 最低嚴重程度，預設info；接受debug、info、warning、error，略過較低程度事件。 |
| `spec.syslog` | 只在type=syslog使用，省略時採本機syslog、local6與標籤routerd。 |
| `spec.syslog.network` | 傳輸方式為unix、unixgram、tcp或udp；空值由Go syslog探索本機socket。 |
| `spec.syslog.address` | 目的socket路徑或host:port，預設空。明確指定傳輸方式時應提供對應位址；驗證器不強制此組合，連線錯誤在輸出時出現。 |
| `spec.syslog.facility` | syslog facility，預設local6；可用值見YAML列舉。 |
| `spec.syslog.tag` | syslog標籤，預設routerd，不是資源參照。 |
| `spec.otlp` | OTLP宣告；type=otlp時telemetryRef與endpoint恰好一項必填，此輸出端尚無實際傳送處理。 |
| `spec.otlp.telemetryRef` | Telemetry/名稱或單純Telemetry名稱，與endpoint互斥，預設空。即使選其他type，Router整體驗證仍檢查目標存在。目前OTLPSink不用它傳送。 |
| `spec.otlp.endpoint` | 預定目的URI，與telemetryRef互斥，無預設值；驗證URI語法，但此LogSink不建立exporter。 |
| `spec.webhook` | type=webhook使用的JSON HTTP POST目的地設定。 |
| `spec.webhook.url` | type=webhook時必填且無預設值。請使用HTTP(S)絕對URL；驗證器只檢查URI語法，只有HTTP 2xx視為傳送成功。 |
| `spec.webhook.headers` | 額外請求標頭，預設空；會覆寫同名標頭，包括預設Content-Type: application/json。 |
| `spec.webhook.timeout` | HTTP請求逾時，預設5s，使用Go duration。驗證器也接受零及負值；HTTP client中不大於零表示不設逾時。 |
| `spec.file` | type=file的檔案設定，以JSON Lines附加寫入。 |
| `spec.file.name` | 邏輯檔名，預設routerd-events，路徑為/var/lib/routerd/logs/<name>.jsonl。禁止路徑分隔符、NUL及換行，不是任意檔案路徑。 |
| `spec.journald` | type=journald的設定，透過程序logger輸出。 |
| `spec.journald.identifier` | 加在程序記錄訊息前的前綴，預設routerd，不直接設定journald的SYSLOG_IDENTIFIER。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `type`
