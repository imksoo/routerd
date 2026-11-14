# WireGuardInterface

[各資源的設定](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 選填; string
    listenPort: 1 # 選填; integer; minimum=1; maximum=65535
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
    peersFrom: # 選填; array
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    privateKey: "<string>" # 選填; string
    privateKeyFile: "<string>" # 選填; string
    selfNodeRef: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
