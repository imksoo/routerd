# EventSubscription

[各資源的設定](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

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
