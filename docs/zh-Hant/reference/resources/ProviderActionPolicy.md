# ProviderActionPolicy

[各資源的設定](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 選填; boolean
    allowedActions: # 選填; array
      - "<string>" # string
    allowedCIDRs: # 選填; array
      - "<string>" # string
    allowedProviderRefs: # 選填; array
      - "<string>" # string
    allowedProviders: # 選填; array
      - "<string>" # string
    dryRunOnly: true # 選填; boolean
    enabled: true # 選填; boolean
    executionWindow: "<string>" # 選填; string
    maxActionsPerRun: 0 # 選填; integer
    requireApproval: true # 選填; boolean
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
