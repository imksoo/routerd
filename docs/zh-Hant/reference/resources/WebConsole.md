# WebConsole

[各資源的設定](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 選填; string
    enabled: true # 選填; boolean
    links: # 選填; array
      - # object
        description: "<string>" # 選填; string
        label: "<string>" # 必填; string
        url: "<string>" # 必填; string
    listenAddress: "<string>" # 選填; string
    listenAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    title: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
