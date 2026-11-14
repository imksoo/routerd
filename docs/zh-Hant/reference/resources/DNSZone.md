# DNSZone

[各資源的設定](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 選填; object
      ddns: true # 選填; boolean
      hostnameSuffix: "<string>" # 選填; string
      leaseFile: "<string>" # 選填; string
      sources: # 選填; array
        - "<string>" # string
      ttl: 0 # 選填; integer; minimum=0
    dnssec: # 選填; object
      enabled: true # 選填; boolean
      mode: "" # 選填; string; enum=["", "validate", "sign"]
    records: # 選填; array
      - # object
        hostname: "<string>" # 必填; string
        ipv4: "<string>" # 選填; string
        ipv4From: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        ipv6: "<string>" # 選填; string
        ipv6From: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        ttl: 0 # 選填; integer; minimum=0
    reverseZones: # 選填; array
      - # object
        name: "<string>" # 必填; string
    ttl: 0 # 選填; integer; minimum=0
    zone: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
