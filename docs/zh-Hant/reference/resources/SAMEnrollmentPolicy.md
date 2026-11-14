# SAMEnrollmentPolicy

[各資源的設定](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 選填; object
      pattern: "<string>" # 選填; string
    directMesh: # 選填; object
      peerGroupRef: "<string>" # 選填; string
    endpointPrefixes: # 選填; array
      - "<string>" # string
    joinAudience: "<string>" # 選填; string
    joinTokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    mobilityPoolRefs: # 選填; array
      - "<string>" # string
    mobilityPrefixes: # 選填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 選填; string
    rrSetRef: "<string>" # 選填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 選填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 選填; object
      allowedExtraIPPrefixes: # 選填; array
        - "<string>" # string
      endpointPrefixes: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
