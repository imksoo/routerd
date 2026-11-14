# SAMTransportProfile

[リソース別の設定](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 任意; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必須; object
      bfd: # 任意; object
        detectMultiplier: 1 # 任意; integer; minimum=1; maximum=50
        enabled: true # 任意; boolean
        interface: "<string>" # 任意; string
        minRx: "<string>" # 任意; string
        minTx: "<string>" # 任意; string
        profile: "" # 任意; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 任意; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 任意; integer
      ebgpMultihop: 0 # 任意; integer; minimum=0; maximum=255
      exportPolicy: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
      generatePeers: true # 任意; boolean
      importPolicy: # 任意; object
        allowedPrefixLengthMax: 0 # 任意; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # 任意; integer; minimum=0; maximum=128
        allowedPrefixes: # 任意; array
          - "<string>" # string
        forbiddenCommunities: # 任意; array
          - "<string>" # string
        localPreference: 0 # 任意; integer
        nextHopRewrite: "" # 任意; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # 任意; array
          - "<string>" # string
      peerASN: 1 # 必須; integer; minimum=1
      routeReflectorClient: true # 任意; boolean
      routeReflectorClusterID: "<string>" # 任意; string
      routerRef: "<string>" # 必須; string
      timers: # 任意; object
        profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 任意; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 任意; integer; minimum=1; maximum=65535
    encapSport: 1 # 任意; integer; minimum=1; maximum=65535
    encryption: "" # 任意; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必須; string
    localEndpoint: "<string>" # 任意; string
    localEndpointFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    mode: "ipip" # 必須; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 任意; array
      - # object
        direct: true # 任意; boolean
        nodeRefs: # 任意; array
          - "<string>" # string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    publishPeerGroup: true # 任意; boolean
    selfNodeRef: "<string>" # 必須; string
    underlayInterface: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
