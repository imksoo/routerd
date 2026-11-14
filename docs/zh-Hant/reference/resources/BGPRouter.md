# BGPRouter

[各資源的設定](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 選填; string; enum=["", "gobgp"]
    communities: # 選填; object
      accept: # 選填; array
        - "<string>" # string
      send: "" # 選填; string; enum=["", "standard", "extended", "both"]
      set: # 選填; object
        in: # 選填; array
          - "<string>" # string
        out: # 選填; array
          - "<string>" # string
    convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
    gracefulRestart: # 選填; object
      enabled: true # 選填; boolean
      restartTime: "<string>" # 選填; string
      stalePathTime: "<string>" # 選填; string
    importPolicy: # 選填; object
      allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixes: # 選填; array
        - "<string>" # string
      forbiddenCommunities: # 選填; array
        - "<string>" # string
      localPreference: 0 # 選填; integer
      nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 選填; array
        - "<string>" # string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    redistribute: # 選填; object
      connected: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
      static: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 選填; string
    watcher: # 選填; object
      maxPrefixes: 1 # 選填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 選填; string
      pollInterval: "<string>" # 選填; string
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
