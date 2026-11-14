# TunnelInterface

[各資源的設定](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 選填; string
    encapDport: 1 # 選填; integer; minimum=1; maximum=65535
    encapSport: 1 # 選填; integer; minimum=1; maximum=65535
    key: 0 # 選填; integer; minimum=0; maximum=4294967295
    local: "<string>" # 選填; string
    localFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
    pathMTU: # 選填; object
      forceFragmentIPv4: true # 選填; boolean
    peerAddress: "<string>" # 選填; string
    remote: "<string>" # 選填; string
    remoteFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    trustedUnderlay: true # 必填; boolean
    ttl: 1 # 選填; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
