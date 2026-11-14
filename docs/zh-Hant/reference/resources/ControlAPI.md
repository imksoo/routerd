# ControlAPI

[各資源的設定](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 選填; array
      - "<string>" # string
    enabled: true # 選填; boolean
    listenAddress: "<string>" # 選填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    tls: # 選填; object
      certFile: "<string>" # 選填; string
      clientCAFile: "<string>" # 選填; string
      keyFile: "<string>" # 選填; string
    tokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
