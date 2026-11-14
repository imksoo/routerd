# DynamicConfigSource

[各資源的設定](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

宣告以外掛取得的動態設定來源。目前有型別解碼與驗證，但沒有讀取DynamicConfigSourceSpec的runtime處理，宣告本身不會執行外掛。

## 指定條件與參照關係

- pluginRef只宣告名稱，驗證時不檢查目標是否存在。此處ttl必填，但實際外掛結果合併另用PluginResult.status.ttl，不能依此資源覆寫結果TTL。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # 選填; object
      conflict: "" # 選填; string; enum=["", "reject"]
    pluginRef: "<string>" # 必填; string
    triggers: # 選填; array
      - # object
        every: "<string>" # 選填; string
        topic: "<string>" # 選填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
    ttl: "<string>" # 選填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.pluginRef` | 預定來源Plugin的metadata.name；必填且不可為空白，無預設值。不加Plugin/；目前沒有此宣告的執行期解析器。 |
| `spec.ttl` | 必須為正的Go duration，無預設值。目前僅驗證此宣告，沒有runtime將它套用至外掛執行或結果期限。 |
| `spec.mergePolicy` | 選填合併策略物件；目前沒有讀取此來源宣告的runtime處理。 |
| `spec.mergePolicy.conflict` | 驗證只接受空值或reject；兩者都不會透過此宣告啟用既有資源替換。 |
| `spec.triggers` | 觸發宣告清單，預設空；目前runtime僅驗證與顯示，請參照上方限制。 |
| `spec.triggers[].type` | 觸發類型；必須指定interval或event，無預設值。 |
| `spec.triggers[].every` | interval時必填，須為正的Go duration；event時此欄位不產生週期排程，無預設值。 |
| `spec.triggers[].topic` | 事件主題宣告；選填，預設空，event時驗證器也不強制要求。這不是資源名稱。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)
