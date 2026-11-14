# BGPPeer

[リソース別の設定](../resources.md#bgppeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 任意; string
    communities: # 任意; object
      accept: # 任意; array
        - "<string>" # string
      send: "" # 任意; string; enum=["", "standard", "extended", "both"]
      set: # 任意; object
        in: # 任意; array
          - "<string>" # string
        out: # 任意; array
          - "<string>" # string
    convergenceProfile: "" # 任意; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # 任意; integer; minimum=0; maximum=255
    exportPolicy: # 任意; object
      allowedPrefixes: # 任意; array
        - "<string>" # string
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
    passiveMode: true # 任意; boolean
    password: "<string>" # 任意; string
    passwordFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    peerASN: 1 # 必須; integer; minimum=1
    peers: # 任意; array
      - "<string>" # string
    routeReflectorClient: true # 任意; boolean
    routeReflectorClusterID: "<string>" # 任意; string
    routerRef: "<string>" # 必須; string
    timers: # 任意; object
      profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
