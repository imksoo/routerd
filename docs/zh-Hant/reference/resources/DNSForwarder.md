# DNSForwarder

[各資源的設定](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 選填; boolean
    healthcheck: # 選填; object
      failThreshold: 1 # 選填; integer; minimum=1
      interval: "<string>" # 選填; string
      passThreshold: 1 # 選填; integer; minimum=1
      timeout: "<string>" # 選填; string
    match: # 必填; array
      - "<string>" # string
    queryTimeout: "<string>" # 選填; string
    resolver: "<string>" # 必填; string
    upstreams: # 選填; array
      - "<string>" # string
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
    zoneRefs: # 選填; array
      - "<string>" # string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
