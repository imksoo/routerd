# PPPoESession

[各資源的設定](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 選填; string
    authMethod: "chap" # 選填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 選填; boolean
    enabled: true # 選填; boolean
    ifname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    ipv6: true # 選填; boolean
    lcpEchoFailure: 0 # 選填; integer; minimum=0
    lcpEchoInterval: 0 # 選填; integer; minimum=0
    lcpFailure: 0 # 選填; integer; minimum=0
    lcpInterval: 0 # 選填; integer; minimum=0
    managed: true # 選填; boolean
    mru: 576 # 選填; integer; minimum=576; maximum=1500
    mtu: 576 # 選填; integer; minimum=576; maximum=1500
    password: "<string>" # 選填; string
    passwordFile: "<string>" # 選填; string
    persist: true # 選填; boolean
    secretEncoding: "plain" # 選填; string; enum=["plain"]
    serviceName: "<string>" # 選填; string
    usePeerDNS: true # 選填; boolean
    username: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
