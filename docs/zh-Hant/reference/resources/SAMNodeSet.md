# SAMNodeSet

[各資源的設定](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必填; array
      - # object
        eventEndpoint: "<string>" # 選填; string
        macAddresses: # 選填; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 選填; object
          drain: true # 選填; boolean
        maxSecondaryIPs: 0 # 選填; integer; minimum=0
        nodeRef: "<string>" # 必填; string
        placement: # 選填; object
          group: "<string>" # 選填; string
          priority: 0 # 選填; integer; minimum=0; maximum=1000000
        role: "" # 選填; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 選填; boolean
        samEndpoint: "<string>" # 選填; string
        samEndpointFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        site: "<string>" # 選填; string
        wireGuard: # 選填; object
          allowedIPs: # 選填; array
            - "<string>" # string
          endpoint: "<string>" # 選填; string
          persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 選填; string
          transportEndpoint: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
