# DerivedEvent

[各資源的設定](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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
