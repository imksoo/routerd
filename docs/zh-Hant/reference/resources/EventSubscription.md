# EventSubscription

[各資源的設定](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

從持久化資料庫比對尚未到期的聯邦事件，呼叫本機 Plugin，將結果儲存為 DynamicConfigPart。

## 指定條件與參照關係

- groupRef、至少一個 match.types 與 trigger.pluginRef 必填。群組與 Plugin 使用單純 metadata 名稱，不是 Kind/name。此驗證不檢查存在性：Plugin 遺失時執行期為 Pending，沒有符合保存事件的群組為 Idle。
- 比對區分大小寫。型別、subject 前綴、各 payload 項與來源節點限制以 AND 組合；清單選項以 OR 組合。事件型別完整比對，不是本機主題萬用字元。
- 控制器通常每 5 秒輪詢保存事件。成功的訂閱/事件組合略過，失敗最多嘗試三次；中斷後的 pending 可重試。處理為至少一次，並不保證 Plugin 外部副作用恰好一次。
- 兩個批次選項都非正值時，一次呼叫接收當前合格批次，不一定逐事件呼叫。批次計時器在記憶體中，事件與執行紀錄持久化。排程批次達 1024 件或約 4 MiB 的選定資料時提早執行。
- Plugin 接收符合事件，以及僅由 Plugin.context.resources 明確允許的設定資源；密鑰會遮蔽。TTL 與產生資源由 PluginResult 決定，不由此訂閱決定。不同事件批次產生不同的有效 DynamicConfigPart 來源。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    match: # 必填; object
      payload: # 選填; object
        "<key>": "<string>" # 選填; string
      sourceNodes: # 選填; array
        - "<string>" # string
      subjectPrefixes: # 選填; array
        - "<string>" # string
      types: # 必填; array
        - "<string>" # string
    trigger: # 必填; object
      batchWindow: "<string>" # 選填; string
      debounce: "<string>" # 選填; string
      pluginRef: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.groupRef` | 必填單純 EventGroup 名稱，用於選擇保存事件的 group_name。無預設，不會自動建立或啟動群組。 |
| `spec.match` | 必填比對物件，types 不可為空。其他選用條件為空時不限制。 |
| `spec.match.types` | 必填完整事件型別清單，至少一個非空項目。符合任一型別即可；萬用字元視為一般字串。 |
| `spec.match.subjectPrefixes` | 選用 subject 前綴清單，預設空允許全部。使用字串前綴比對，不是路徑區段或萬用字元比對。 |
| `spec.match.payload` | 選用的鍵/字串完整比對映射，每個項目都須符合，鍵不可空白。遺失鍵讀為空字串，因此要求空值也會符合遺失鍵。 |
| `spec.match.sourceNodes` | 選用 sourceNode 完整比對允許清單，預設空允許全部。不會自動排除自身事件，請指定所需來源識別。 |
| `spec.trigger` | 必填的本機 Plugin 呼叫設定。與 Plugin.triggers 不同，由訂閱控制器執行。 |
| `spec.trigger.pluginRef` | 必填單純 Plugin metadata.name，不是 Plugin/name。Plugin 遺失時為 Pending。使用該 Plugin 的執行檔、逾時、環境與 context 限制。 |
| `spec.trigger.batchWindow` | 選用 Go 時間長度，自首個選定事件起算。預設空/0 無批次上限；但 debounce 為正時預設上限為 1m。正 batchWindow 限制反覆 debounce。負值可通過驗證，視為非正值。 |
| `spec.trigger.debounce` | 選用 Go 時間長度，自最後新增選定事件起算，預設空/0 停用。重讀相同事件不延長。debounce 與 batchWindow 較早期限觸發；無正 batchWindow 時上限 1m。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventsubscription/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventsubscription/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。
