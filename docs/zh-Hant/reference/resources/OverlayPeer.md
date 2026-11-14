# OverlayPeer

[各資源的設定](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必填; string
    pathMTU: # 選填; object
      forceFragmentIPv4: true # 選填; boolean
    remote: # 選填; object
      address: "<string>" # 選填; string
      nodeID: "<string>" # 選填; string
    role: "onprem" # 必填; string; enum=["onprem", "cloud"]
    underlay: # 必填; object
      address: "<string>" # 選填; string
      interface: "<string>" # 選填; string
      type: "wireguard" # 必填; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
