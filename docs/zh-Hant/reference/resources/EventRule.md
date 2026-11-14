# EventRule

[各資源的設定](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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
