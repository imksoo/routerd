# NAT44SessionSync

[各資源的設定](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 選填; string
    conntrackd: # 選填; object
      ignoreIPv4: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      localAddress: "<string>" # 必填; string
      peerAddress: "<string>" # 必填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    excludeNatRules: # 選填; array
      - "<string>" # string
    interval: "<string>" # 選填; string
    mode: "event-stream" # 選填; string; enum=["event-stream", "conntrackd"]
    natRules: # 選填; array
      - "<string>" # string
    snatAddresses: # 選填; array
      - "<string>" # string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        restoreCommand: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
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
