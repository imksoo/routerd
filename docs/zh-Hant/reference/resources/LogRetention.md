# LogRetention

[各資源的設定](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

清除操作記錄的SQLite資料列，並可在刪除前轉送保存；不管理任意OS記錄檔。

## 指定條件與參照關係

- 首次協調執行，之後距lastRunAt至少23小時才再次執行。省略schedule與daily都依經過時間判斷，不是午夜cron；dry-run不開啟或清除外部記錄DB。
- 事件日誌另有24小時及資料列數、容量上限；此處設定較長期間不會延長事件日誌上限。
- 指定sinks時，每列刪除前先轉送；保存端錯誤會阻止該列成功刪除。目前LogSink的otlp不會傳送，不能當作有效保存目的地。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必填; string
    schedule: "" # 選填; string; enum=["", "daily"]
    signals: # 選填; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 選填; array
      - "<string>" # string
    vacuum: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.retention` | 保留期間，必填且無預設值，例如7d或24h。d為整數天數乘24小時，其他使用Go duration。驗證器只解析，不要求正值。 |
| `spec.signals` | 要清除的記錄類別；預設包含events、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprints全部七種。DB路徑來自相關記錄資源或OS預設值。 |
| `spec.sinks` | 刪除前保存目的地；預設空，不保存。接受LogSink/名稱或單純名稱，選取啟用的system.routerd.net/v1alpha1 LogSink。不存在的名稱不會被選取，也不報錯，須確認名稱。 |
| `spec.vacuum` | 刪除後是否對目標SQLite執行VACUUM；預設false，用於整理DB空間而非選擇保留對象。 |
| `spec.schedule` | 省略或daily；皆為首次及之後至少23小時才執行。不支援時刻或cron表示式。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go)
