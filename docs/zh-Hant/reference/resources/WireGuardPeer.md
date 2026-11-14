# WireGuardPeer

[各資源的設定](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 選填; string
    presharedKeyFile: "<string>" # 選填; string
    publicKey: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
