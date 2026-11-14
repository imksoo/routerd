# LogRetention

[各資源的設定](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

清除操作記錄的SQLite資料列，並可在刪除前轉送保存；不管理任意OS記錄檔。

## 指定條件與參照關係

- 首次協調執行，之後距lastRunAt至少23小時才再次執行。省略schedule與daily都依經過時間判斷，不是午夜cron；dry-run不開啟或清除外部記錄DB。
- 事件日誌另有24小時及資料列數、容量上限；此處設定較長期間不會延長事件日誌上限。
- 指定sinks時，每列刪除前先轉送；保存端錯誤會阻止該列成功刪除。目前LogSink的otlp不會傳送，不能當作有效保存目的地。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

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

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.retention` | **必填** | string | 保留期間，必填且無預設值，例如7d或24h。d為整數天數乘24小時，其他使用Go duration。驗證器只解析，不要求正值。 保留期間請使用正值。 |
| `spec.signals` | *選填* | array | 要清除的記錄類別；預設包含events、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprints全部七種。DB路徑來自相關記錄資源或OS預設值。 |
| `spec.sinks` | *選填* | array | 刪除前保存目的地，預設空表示不保存。可用LogSink/名稱或單純名稱；Router整體驗證要求目標存在，執行時排除停用輸出端。 |
| `spec.vacuum` | *選填* | boolean | 刪除後是否對目標SQLite執行VACUUM；預設false，用於整理DB空間而非選擇保留對象。 |
| `spec.schedule` | *選填* | string; enum=["", "daily"] | 省略或daily；皆為首次及之後至少23小時才執行。不支援時刻或cron表示式。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `targets`, `updatedAt`
