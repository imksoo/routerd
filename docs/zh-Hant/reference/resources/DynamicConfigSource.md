# DynamicConfigSource

[各資源的設定](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

宣告以外掛取得的動態設定來源。目前有型別解碼與驗證，但沒有讀取DynamicConfigSourceSpec的runtime處理，宣告本身不會執行外掛。

## 指定條件與參照關係

- pluginRef只宣告名稱，驗證時不檢查目標是否存在。此處ttl必填，但實際外掛結果合併另用PluginResult.status.ttl，不能依此資源覆寫結果TTL。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    [ mergePolicy:
        [ conflict: ("" | "reject") ]
    ]
    pluginRef: <string>
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
    ttl: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.pluginRef` | **必填** | string | 預定來源Plugin的metadata.name；必填且不可為空白，無預設值。不加Plugin/；目前沒有此宣告的執行期解析器。 |
| `spec.ttl` | **必填** | string | 必須為正的Go duration，無預設值。目前僅驗證此宣告，沒有runtime將它套用至外掛執行或結果期限。 |
| `spec.mergePolicy` | *選填* | object | 選填合併策略物件；目前沒有讀取此來源宣告的runtime處理。 |
| `spec.mergePolicy.conflict` | *選填* | string; enum=["", "reject"] | 驗證只接受空值或reject；兩者都不會透過此宣告啟用既有資源替換。 |
| `spec.triggers` | *選填* | array | 觸發宣告清單，預設空；目前runtime僅驗證與顯示，請參照上方限制。 |
| `spec.triggers[].type` | **條件必填** | string; enum=["interval", "event"] | 觸發類型；必須指定interval或event，無預設值。 |
| `spec.triggers[].every` | *選填* | string | interval時必填，須為正的Go duration；event時此欄位不產生週期排程，無預設值。 |
| `spec.triggers[].topic` | *選填* | string | 事件主題宣告；選填，預設空，event時驗證器也不強制要求。這不是資源名稱。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。
