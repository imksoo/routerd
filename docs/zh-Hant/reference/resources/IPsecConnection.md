# IPsecConnection

[各資源的設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 選填; string
    cloudProviderHint: "" # 選填; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必填; string
    localAddress: "<string>" # 必填; string
    phase1Proposals: # 選填; array
      - "<string>" # string
    phase2Proposals: # 選填; array
      - "<string>" # string
    preSharedKey: "<string>" # 選填; string
    remoteAddress: "<string>" # 必填; string
    rightSubnet: "<string>" # 必填; string
```
