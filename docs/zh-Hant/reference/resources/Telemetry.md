# Telemetry

[各資源的設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 選填; object
      "<key>": "<string>" # 選填; string
    otlp: # 必填; object
      endpoint: "<string>" # 必填; string
      insecure: true # 選填; boolean
    serviceNamespace: "<string>" # 選填; string
    signals: # 選填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
