# DerivedEvent

[各資源的設定](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

資源狀態條件改變時，發送成立或撤回事件。與 EventRule 不同，此處評估目前狀態而非事件序列。

## 指定條件與參照關係

- topic 與非空 inputs 清單必填。輸入使用 field/equals/not_empty/any_of 結構，不是共用 resource/field 來源、dependsOn 或 when。
- 目前以 Kind/name.status.field（可包在 ${...} 中）讀取 net.routerd.net/v1alpha1 狀態。不含 .status. 的字串視為固定值，不是狀態參照。一般狀態來源驗證不會檢查這些輸入的來源存在性或提供欄位相容性。
- 控制器在啟動、收到自身以外的本機 routerd.** 事件及定期處理時評估（預設 5 秒）。程序重啟會重設轉換狀態與遲滯計時器，不會從已存狀態還原。
- retractWhen 會驗證，但目前評估器不讀取。撤回依 emitWhen 判定的反值，因此改變 retractWhen 不會改變行為。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # 選填; boolean
    emitWhen: "all_true" # 選填; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 選填; string
    inputs: # 必填; array
      - # object
        any_of: # 選填; array
          - # array
            - # object
              equals: "<string>" # 選填; string
              field: "<string>" # 選填; string
              not_empty: true # 選填; boolean
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        not_empty: true # 選填; boolean
    retractWhen: "any_false" # 選填; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.topic` | 必填的基底事件主題。成立發送 <topic>.asserted，不成立發送 <topic>.retracted；不發送裸主題事件。 |
| `spec.inputs` | 必填且非空的條件清單，由 emitWhen 組合。空條件物件會評估為 true，請明確填寫條件。 |
| `spec.inputs[].field` | 選用字串，預設空。Kind/name.status.field 讀取狀態頂層成員；來源或成員不存在則為空。其他字串視為固定值。陣列轉為 JSON 字串，equals 比較完整序列化值。 |
| `spec.inputs[].equals` | 選用、區分大小寫的完整比對。預設空表示不比較。field 非空且未設 equals/not_empty 時，隱含要求值非空白。 |
| `spec.inputs[].not_empty` | 預設 false。true 拒絕空值或純空白；equals 亦非空時，兩個條件都須成立。 |
| `spec.inputs[].any_of` | 選用的 AND 群組 OR 清單。非空時優先於此輸入的 field/equals/not_empty。每組至少一個條件且全部成立；空群組為 false。 |
| `spec.inputs[].any_of[][].field` | 群組條件欄位，狀態或固定值規則同 inputs[].field。 |
| `spec.inputs[].any_of[][].equals` | 群組完整比對；空值與預設語意同 inputs[].equals。 |
| `spec.inputs[].any_of[][].not_empty` | 群組非空白檢查，預設 false，語意同 inputs[].not_empty。 |
| `spec.emitWhen` | 預設 all_true 要求所有輸入成立；any_true 要求至少一項成立。此目標變為 false 也會決定撤回。 |
| `spec.retractWhen` | 接受 any_false（預設）與 all_false，但驗證後未使用。不可依賴獨立的撤回條件。 |
| `spec.hysteresis` | 選用 Go 時間長度，預設 0 為立即轉換。新目標須持續至計時到期再檢查；回復舊目標會取消。初始狀態不等待。驗證接受負值，會立即觸發。 |
| `spec.emitInitial` | 預設 false，只儲存初始狀態而不發送事件。true 在每次新控制器狀態建立時發送初始結果，包含重啟後。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/derived/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/derived/engine.go)
