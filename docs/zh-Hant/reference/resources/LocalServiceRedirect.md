# LocalServiceRedirect

[各資源的設定](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必填; string
    rules: # 必填; array
      - # object
        destinationPort: 1 # 必填; integer; minimum=1; maximum=65535
        destinationSetRef: "<string>" # 必填; string
        name: "<string>" # 選填; string
        protocols: # 必填; array
          - "<string>" # string
        redirectPort: 1 # 必填; integer; minimum=1; maximum=65535
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
