# SAMEnrollmentClaim

[各資源的設定](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 選填; object
      asn: 1 # 選填; integer; minimum=1
      routerID: "<string>" # 選填; string
    directMesh: true # 選填; boolean
    endpoint: "<string>" # 選填; string
    expiresAt: "<string>" # 選填; string
    joinAudience: "<string>" # 選填; string
    joinHMAC: "<string>" # 選填; string
    joinNonce: "<string>" # 選填; string
    joinTimestamp: "<string>" # 選填; string
    leafID: "<string>" # 必填; string
    mobility: # 選填; object
      ownedAddresses: # 選填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 選填; boolean
    rrSetRef: "<string>" # 選填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 選填; object
      allowedIPs: # 選填; array
        - "<string>" # string
      endpoint: "<string>" # 選填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
