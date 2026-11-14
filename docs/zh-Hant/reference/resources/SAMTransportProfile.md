# SAMTransportProfile

[各資源的設定](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 選填; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必填; object
      bfd: # 選填; object
        detectMultiplier: 1 # 選填; integer; minimum=1; maximum=50
        enabled: true # 選填; boolean
        interface: "<string>" # 選填; string
        minRx: "<string>" # 選填; string
        minTx: "<string>" # 選填; string
        profile: "" # 選填; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 選填; integer
      ebgpMultihop: 0 # 選填; integer; minimum=0; maximum=255
      exportPolicy: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
      generatePeers: true # 選填; boolean
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
      peerASN: 1 # 必填; integer; minimum=1
      routeReflectorClient: true # 選填; boolean
      routeReflectorClusterID: "<string>" # 選填; string
      routerRef: "<string>" # 必填; string
      timers: # 選填; object
        profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 選填; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 選填; integer; minimum=1; maximum=65535
    encapSport: 1 # 選填; integer; minimum=1; maximum=65535
    encryption: "" # 選填; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必填; string
    localEndpoint: "<string>" # 選填; string
    localEndpointFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 選填; array
      - # object
        direct: true # 選填; boolean
        nodeRefs: # 選填; array
          - "<string>" # string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    publishPeerGroup: true # 選填; boolean
    selfNodeRef: "<string>" # 必填; string
    underlayInterface: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
