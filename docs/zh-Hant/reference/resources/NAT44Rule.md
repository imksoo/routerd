# NAT44Rule

[各資源的設定](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    egressInterface: "<string>" # 選填; string
    egressPolicyRef: "<string>" # 選填; string
    excludeDestinationCIDRs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    outboundInterface: "<string>" # 選填; string
    snatAddress: "<string>" # 選填; string
    snatAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    sourceCIDRs: # 選填; array
      - "<string>" # string
    sourceRanges: # 選填; array
      - "<string>" # string
    translation: # 選填; object
      address: "<string>" # 選填; string
      addresses: # 選填; array
        - "<string>" # string
      portMapping: # 選填; object
        end: 1 # 選填; integer; minimum=1; maximum=65535
        start: 1 # 選填; integer; minimum=1; maximum=65535
        type: "auto" # 選填; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # 必填; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # 選填; string; enum=["masquerade", "snat"]
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
