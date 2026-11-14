# BGPRouter

[リソース別の設定](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必須; integer; minimum=1
    backend: "" # 任意; string; enum=["", "gobgp"]
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
    exportPolicy: # 任意; object
      allowedPrefixes: # 任意; array
        - "<string>" # string
    gracefulRestart: # 任意; object
      enabled: true # 任意; boolean
      restartTime: "<string>" # 任意; string
      stalePathTime: "<string>" # 任意; string
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
    listen: # 任意; object
      address: "<string>" # 任意; string
      port: 1 # 任意; integer; minimum=1; maximum=65535
    redistribute: # 任意; object
      connected: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
      static: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
    routerID: "<string>" # 必須; string
    timers: # 任意; object
      profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 任意; string
    watcher: # 任意; object
      maxPrefixes: 1 # 任意; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 任意; string
      pollInterval: "<string>" # 任意; string
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
