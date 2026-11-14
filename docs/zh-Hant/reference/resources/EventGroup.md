# EventGroup

[各資源的設定](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 選填; object
      mode: "" # 選填; string; enum=["", "hmac"]
      secretFile: "<string>" # 選填; string
      secretRef: "<string>" # 選填; string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 0 # 選填; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必填; string
    peersFrom: # 選填; array
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    replayWindow: "<string>" # 選填; string
    retention: # 選填; object
      maxAge: "<string>" # 選填; string
      maxEvents: 0 # 選填; integer; minimum=0
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
