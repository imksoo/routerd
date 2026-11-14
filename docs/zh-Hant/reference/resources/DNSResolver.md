# DNSResolver

[各資源的設定](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 選填; object
      enabled: true # 選填; boolean
      maxEntries: 0 # 選填; integer; minimum=0
      maxTTL: "<string>" # 選填; string
      minTTL: "<string>" # 選填; string
      negativeTTL: "<string>" # 選填; string
    listen: # 必填; array
      - # object
        addressFrom: # 選填; array
          - # object
            field: "<string>" # 選填; string
            optional: true # 選填; boolean
            resource: "<string>" # 必填; string
        addresses: # 選填; array
          - "<string>" # string
        name: "<string>" # 選填; string
        port: 1 # 選填; integer; minimum=1; maximum=65535
        sources: # 選填; array
          - "<string>" # string
    metrics: # 選填; object
      perUpstream: true # 選填; boolean
    queryLog: # 選填; object
      enabled: true # 選填; boolean
      path: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
