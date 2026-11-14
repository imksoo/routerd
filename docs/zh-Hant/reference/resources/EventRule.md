# EventRule

[各資源的設定](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

從持久化的本機事件紀錄比對模式並產生事件，可依關聯鍵分開維護狀態。

## 指定條件與參照關係

- pattern.operator 與 emit.topic 必填。topic、topics、trigger、expected 至少提供一項，但寬鬆的驗證不保證模式可運作：只有 expected 不構成輸入清單，absence 需要 trigger/topic。不會檢查主題對應的資源是否存在。
- 輸入順序為 topics、topic、trigger。all_of 與 sequence 使用完整主題字串比對；any_of、window、count、throttle、debounce、absence 支援以點分隔的 *（一段）與 **（零段以上）。
- all_of 觸發後仍保留已見主題，全部見過後每個符合事件都會再次觸發。sequence 完成後重設；strict 時清單內亂序事件也會重設。window 達門檻後清空時間桶，count 達門檻後重設計數。
- 計數、未完成序列與計時器僅存於記憶體，程序重啟後從空狀態開始。事件讀取位置會持久化；新讀取者從目前紀錄尾端開始。位置儲存失敗可能重複處理輸入。所有規則都排除 routerd-eventrule 產生的事件，以避免連鎖迴圈。
- 每條規則最多保留 4096 個關聯鍵，會移除最久未觀測鍵的計數與計時器。window/throttle 比較輸入事件時間；absence/debounce 使用程序經過時間計時。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必填; object
      attributes: # 選填; object
        "<key>": "<string>" # 選填; string
      topic: "<string>" # 必填; string
    pattern: # 必填; object
      allow_missing_correlation: true # 選填; boolean
      correlate_by: "<string>" # 選填; string
      duration: "<string>" # 選填; string
      expected: "<string>" # 選填; string
      interval: "<string>" # 選填; string
      operator: "all_of" # 必填; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 選填; string
      rate: 0 # 選填; integer; minimum=0
      strict: true # 選填; boolean
      threshold: 0 # 選填; integer; minimum=0
      topic: "<string>" # 選填; string
      topics: # 選填; array
        - "<string>" # string
      trigger: "<string>" # 選填; string
      window: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.pattern` | 必填的模式物件；不會拒絕所選運算子未使用的欄位。 |
| `spec.pattern.operator` | 必填：all_of、any_of、sequence、window、absence、throttle、debounce 或 count，無預設值。 |
| `spec.pattern.topic` | 選用的單一輸入主題，附加於 topics 後。absence 的 trigger 為空時以此作為啟動主題。預設空。 |
| `spec.pattern.topics` | 選用的有序輸入清單，預設空。sequence 依此順序；all_of 要求每個完整主題都曾出現。 |
| `spec.pattern.trigger` | 選用的 absence 啟動主題，優先於 topic，會啟動或重新啟動計時器。其他運算子的輸入清單也會在 topic 後加入此值。預設空。 |
| `spec.pattern.expected` | 選用的 absence 取消主題，同鍵符合時取消待處理計時器，優先於 trigger 比對。不是一般輸入主題。預設空，表示不取消。 |
| `spec.pattern.duration` | 選用 Go 時間長度；window/throttle/absence 預設 1m，debounce 預設 1s。專用 window/interval/quiet 優先。驗證也接受零或負值，延遲行為請使用正值。 |
| `spec.pattern.window` | window 的回溯期間，優先於 duration，皆省略時為 1m。含起始邊界；達門檻後清空時間桶。其他運算子不使用。 |
| `spec.pattern.quiet` | debounce 的安靜期間，優先於 duration，皆省略時為 1s。每次符合輸入都重啟計時器，依最後輸入產生事件。 |
| `spec.pattern.interval` | throttle 每個關聯鍵的最短發送間隔，優先於 duration，皆省略時為 1m。第一個符合事件立即發送。 |
| `spec.pattern.rate` | 驗證接受的非負整數，預設 0，但目前引擎不讀取。無法控制 throttle；請使用 interval。 |
| `spec.pattern.threshold` | window/count 的事件門檻，非負整數；省略或 0 表示 1。其他運算子不使用。 |
| `spec.pattern.correlate_by` | 選用 attributes.<key>、resource.name/kind/apiVersion 或 daemon.instance/kind 以分開狀態。空值使用共用鍵 default。輸入主題是字串，並非 Kind/name 參照。 |
| `spec.pattern.allow_missing_correlation` | 預設 false：略過缺少關聯值的輸入並增加 warningCount。true 將缺值歸入空鍵。但 resource 物件存在而所選成員為空時，false 也會接受。 |
| `spec.pattern.strict` | 預設 false，僅供 sequence。清單內主題亂序時進度歸零，該事件不會重新作為首項；忽略清單外主題。 |
| `spec.emit` | 必填的輸出事件物件。嚴重性固定 info，resource 為此 EventRule。 |
| `spec.emit.topic` | 必填且不可為空的輸出事件型別，無預設值。規則引擎不會消費自身產生的事件。 |
| `spec.emit.attributes` | 選用字串映射。展開 ${event.type}、${correlation}、${count}、${resource.name/kind/apiVersion}、${attributes.<key>}；無法解析的標記保持原樣。正 count 與非空 correlation 會覆寫同名輸出鍵。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/eventrule/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/eventrule/engine.go) · [pkg/eventconsumer/consumer.go](https://github.com/imksoo/routerd/blob/main/pkg/eventconsumer/consumer.go) · [pkg/bus/bus.go](https://github.com/imksoo/routerd/blob/main/pkg/bus/bus.go)
