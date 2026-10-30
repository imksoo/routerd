# 觀測管線

![Diagram showing Telemetry, LogSink, and ObservabilityPipeline resources feeding routerd OpenTelemetry SDK signals and routerd event exporter output to OTLP, syslog, stdout, or Loki sinks](/img/diagrams/operations-observability.png)

`Telemetry` 是一個小型資源，用於將 routerd 自身的指標、追蹤與日誌輸出至 OTLP。`LogSink` 代表運維事件與觀測日誌的轉發路徑。OTLP 類型的 `LogSink` 透過參照 `Telemetry` 資源來避免重複填寫收集器端點。若需將 routerd 的事件日誌傳送至 Loki 等管線型遠端 sink，請使用 `ObservabilityPipeline`。

`ObservabilityPipeline` 是 routerd 內建的管線，而非隨附的 `otelcol` 程序。OTLP 的日誌、指標與追蹤使用標準 OpenTelemetry SDK 送出，設定的日誌 sink 則由輕量事件匯出器負責傳送。

目前支援的日誌 sink 如下：

- `stdout`：JSON 格式的事件行。
- `syslog`：與現有 `LogSink` 相同的 syslog 格式。
- `loki`：透過 HTTP push 至 `/loki/api/v1/push`。

`kafka` 作為中繼資料接受，以便在設定中保留預期的外部管線意圖。
routerd 目前尚未直接發布至 Kafka。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: ObservabilityPipeline
metadata:
  name: remote-observability
spec:
  otlp:
    endpoint: http://otel-collector.lan:4317
    insecure: true
  serviceNamespace: routerd
  attributes:
    site: edge
  signals: [logs, metrics, traces]
  sampling:
    rate: 1
  logs:
    sinks:
      - name: loki
        type: loki
        minLevel: info
        loki:
          url: http://loki.lan:3100/loki/api/v1/push
          tenant: routerd
```

OTLP 欄位會展開為 routerd 所管理單元的標準 OpenTelemetry 環境變數。日誌 sink 的匯出器會訂閱程序內匯流排的 `routerd.**` 事件，因此無需 scrape `journalctl`，即可轉發控制器狀態變更與常駐程式事件。

完整範例請參閱 `examples/observability-loki.yaml`。

## 事件投遞與復原契約

程序內匯流排的訂閱佇列有容量上限；佇列滿時捨棄喚醒通知，不會刪除已提交至 SQLite 的事件。匯出器和 EventRule 引擎僅用匯流排通知喚醒歷史讀取；沒有通知時預設每秒輪詢，讀取失敗後按一秒至一分鐘的間隔退避重試。這些週期不包含執行耗時、資料庫故障和程序停頓。

| 消費者 | 權威輸入 | 復原方式與限制 |
| --- | --- | --- |
| EventRule | 已提交的事件歷史及每條規則的持久化 checkpoint | 新規則或已修改的規則從設定準備時記錄的歷史位置開始；未修改的規則在重啟後恢復 count、sequence、window、throttle 和待執行的 timer。若保留原則已刪除未讀輸入，則該規則回報錯誤並停止，不能據此判定事件不存在。 |
| 日誌 sink | 已提交的事件歷史及配送 cursor | 失敗時重試；配送成功後若 cursor 儲存失敗，可能重複配送。sampling 和 severity filter 仍然適用。 |

每條 EventRule 在同一 SQLite 交易中提交關聯狀態、唯一的規則輸入 cursor、公開 status 和由此產生的本機輸出事件。交易失敗時，這些狀態均不推進。若提交確認遺失，重新讀取 checkpoint 可避免重複產生已提交的結果。匯流排通知在提交後傳送；消費者可透過歷史讀取恢復遺漏的通知。這項保證僅適用於本機事件歷史；外部 API 或日誌 sink 的配送仍可能重複（at-least-once），多個 sink 之間的配送也不是原子的。

當 EventRule 的 pattern 匹配 `routerd.resource.status.changed` 時，標準 SQLite status store 將有意義的 status 變更及其事件作為同一交易儲存。事件歷史寫入失敗會撤銷 status 寫入，提交後再以同一事件 ID 本機投遞。status store 和匯流排必須連接到同一交易儲存；不支援該能力或儲存不一致時，在儲存 status 前回報錯誤。沒有匹配規則時，不適用此額外保證；不能將其推廣到所有常駐程式或 derived-event producer。

EventRule 支援 `all_of`、`any_of`、`sequence`、`window`、`absence`、`throttle`、`debounce` 和 `count`。throttle 的時長使用 `interval`；非零 `rate` 沒有已實作的語意，因此會被拒絕。每條規則最多保留4096個關聯鍵，序列化後的 checkpoint 上限為8 MiB。超過任一上限時，按最後匹配輸入的時間由舊至新淘汰關聯鍵，直到滿足上限。跨越已淘汰鍵的模式無法完成。每次淘汰都會增加 `status.warningCount`，並與 checkpoint 在同一交易中公開。選用設定時初始化有效規則並移除已刪除規則的 checkpoint；僅準備未選用的候選設定不會改變消費狀態。

[Event federation](../reference/event-federation.md) 使用獨立的投遞記錄，不把本機 status 通知佇列作為持久化權威來源。
