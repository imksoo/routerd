# DSLiteTunnel

[各資源的設定](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 選填; integer; minimum=1
    aftrAddressSelection: "ordinal" # 選填; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 選填; array
      - "<string>" # string
    aftrFQDN: "<string>" # 選填; string
    aftrFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    aftrIPv6: "<string>" # 選填; string
    defaultRoute: true # 選填; boolean
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    enabled: true # 選填; boolean
    encapsulationLimit: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    localAddress: "<string>" # 選填; string
    localAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    localAddressSource: "interface" # 選填; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 選填; string
    localDelegatedAddress: "<string>" # 選填; string
    mtu: 1280 # 選填; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 選填; string
    routeMetric: 0 # 選填; integer; minimum=0
    tunnelName: "<string>" # 選填; string
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
