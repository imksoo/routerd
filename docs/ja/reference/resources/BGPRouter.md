# BGPRouter

[リソース別の設定](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

ローカルGoBGPインスタンス・受理経路・自起源広告を設定します。カーネルへの適用や対向疎通はBGPのEstablishedとは別です。

## 指定条件と参照関係

- 実動作はBGPRouter1件・VRFなしだけに対応します。ASN・VRF・待受が異なれば設定検証に通ることと、実動作対応は別です。
- ルーターの受信一覧・connected一覧・static一覧の3群をまたぐ重複・包含は不可です。各prefix一覧内の正規化後重複も拒否します。

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

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.importPolicy` | 独自指定のないピアが継承するルーター全体の受信ポリシーです。空ならルーター受信フィルターを作りませんが、受信した全経路のカーネルFIB適用を自動許可するものではありません。 |
| `spec.importPolicy.allowedPrefixes` | 任意のIPv4・IPv6 CIDR許可一覧です。プレフィックスと許可した詳細経路長で照合し、正規化後の同一プレフィックスは拒否します。空なら他条件で構成された受信ポリシーにprefix条件を加えません。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 0～128の整数です。省略・0は各許可プレフィックス自身の長さです。正の指定値は各prefix長以上、ファミリー上限以下、正の最大長以下が必要です。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 0～128の整数です。省略・0はIPv4が32、IPv6が128です。正の値は全許可prefix長以上かつファミリー上限以下が必要です。動的leaf受付はmin=max=32が必須です。 |
| `spec.importPolicy.requiredCommunities` | 任意のcommunity一覧です。受理する経路はすべての指定communityを持つ必要があり、空なら必須community条件はありません。 |
| `spec.importPolicy.forbiddenCommunities` | 任意の禁止community一覧です。どれか1つでも一致すれば許可判定より先に拒否します。空ならこの禁止条件はありません。 |
| `spec.importPolicy.nextHopRewrite` | 既定はpeer-addressです。unchangedなら広告されたnext hopを維持します。受理経路の選択・FIB投影前に適用します。ピアでこの項目だけを指定しても独自ポリシーとなり、ルーターの許可一覧は継承しません。 |
| `spec.importPolicy.localPreference` | 受理経路へbest-path選択前に設定するuint32のLOCAL_PREFです。既定0ならBGP実装の既定値を変更しません。 |
| `spec.exportPolicy` | 任意の送信prefix条件です。ピア側の一覧はルーター由来の静的・動的広告と合成し、RRクライアントではルーターの受信prefixも加えます。ルーター由来の送信prefix全体を絞る置換ではありません。 |
| `spec.exportPolicy.allowedPrefixes` | 明示的な自起源広告CIDRで、既定は空です。redistributeのconnected・staticと動的広告へ合成し、ピアの送信条件にも使います。OS経路の存在確認なしで広告します。 |
| `spec.timers` | 任意のタイマープロファイルです。現行コントローラーではルーターのtimersをピアへ継承しないため、必要なら各ピアで明示します。 |
| `spec.timers.profile` | default・fast・slowを受け付けますが、現行実行処理はルーター側値からピアタイマーを設定しません。BGPPeer・BGPDynamicPeerのtimers.profileに指定してください。 |
| `spec.when` | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |
| `spec.asn` | 必須の0以外のuint32ローカルASNです。設定ではBGPRouter間の再利用を拒否し、現行実行処理はBGPRouter1件だけに対応します。 |
| `spec.routerID` | 必須のIPv4形式のBGP識別子で、通信アドレスとは別です。インターフェース指定のないBFDはローカルmultihopアドレスにも使います。 |
| `spec.vrf` | 任意のVRF/nameまたはVRF名で、宣言済みVRFを参照します。設定検証は受け付けますが実動作GoBGPコントローラーは非空値をGoBGPVRFUnsupportedとして拒否します。実運用では省略します。 |
| `spec.listen` | 任意のローカルTCP待受設定で、既定は全アドレス・179番です。相手のTCPポート選択ではありません。 |
| `spec.listen.address` | 任意のIPv4・IPv6バインド先です。空ならGoBGPは全アドレスで待ち受けます。IP指定で、リソース参照・ホスト名は不可です。 |
| `spec.listen.port` | TCP待受ポート1～65535で、省略・0は179です。設定されたアドレス・ポートの競合は拒否します。 |
| `spec.redistribute` | 任意の明示広告prefix群です。現行実行処理はconnected・static一覧とexportPolicy.allowedPrefixesを合成し、OSから該当経路を検出する処理ではありません。 |
| `spec.redistribute.connected` | connected名の任意広告群で、既定は空です。この名前によってカーネルの直結経路存在を広告条件にするものではありません。 |
| `spec.redistribute.connected.allowedPrefixes` | 自起源広告するIPv4・IPv6 CIDRで、既定は空です。ルーターの受信一覧・static一覧と重複・包含不可です。対応する直結経路の存在は要求しません。 |
| `spec.redistribute.static` | static名の任意広告群で、既定は空です。IPv4StaticRoute・IPv6StaticRouteを自動列挙する指定ではありません。 |
| `spec.redistribute.static.allowedPrefixes` | 自起源広告するIPv4・IPv6 CIDRで、既定は空です。ルーターの受信一覧・connected一覧と重複・包含不可です。カーネル静的経路の存在は確認しません。 |
| `spec.communities` | 任意のcommunity設定です。現行実行処理ではルーターのset.outだけが自起源広告に作用し、send・accept・set.inは読取・検証のみで使いません。 |
| `spec.communities.send` | standard・extended・both・空を受け付けますが実動作には影響せず、GoBGPピアはこの指定によらずSendCommunity=3です。 |
| `spec.communities.accept` | 検証される任意の標準community一覧で、既定は空です。現行処理では受信を絞りません。実際の照合にはimportPolicyのrequiredCommunities・forbiddenCommunitiesを使います。 |
| `spec.communities.set` | 任意の受信・送信community一覧で、現行コントローラーはルーターのoutだけを使います。 |
| `spec.communities.set.in` | 任意の検証対象一覧で、既定は空です。現行処理では受信経路に付与しません。 |
| `spec.communities.set.out` | routerd自起源広告へ付与する標準communityで、既定は空です。全通過経路の書換ではありません。標準の16bit成分のasn:valueや対応する既知名を使い、ここでextended communityは生成しません。 |
| `spec.convergenceProfile` | 既定・省略・stableではGRを既定有効、fastでは既定無効にします。gracefulRestart.enabledを明示すれば優先します。timers.profileの自動選択ではありません。 |
| `spec.gracefulRestart` | 任意のGR設定です。収束プロファイル選択後にピアにも使い、enabled明示値はプロファイル既定より優先します。 |
| `spec.gracefulRestart.enabled` | 任意の真偽値です。fast以外は既定trueです。falseなら時間指定があってもGRを無効にします。 |
| `spec.gracefulRestart.restartTime` | 正のGo durationで、既定120秒です。秒未満を切り捨てて整数秒へ変換するため、1秒未満は0になり得ます。整数秒を使ってください。 |
| `spec.gracefulRestart.stalePathTime` | 古い経路の保持期間を指定する正のGo durationで、既定360秒です。整数秒へ変換し、GR無効時は使いません。 |
| `spec.watcher` | 任意の状態観測設定で、BGPのkeepalive・holdタイマーや接続受付制限ではありません。 |
| `spec.watcher.pollInterval` | 3秒以上のGo durationです。実間隔は15秒を起点に短い指定値だけ採用するため、15秒超を指定しても長くなりません。 |
| `spec.watcher.maxPrefixes` | 観測するprefix上限で、既定4096です。正の明示値は1000000未満です。BGPセッションのmaximum-prefix停止ポリシーを設定する項目ではありません。 |
| `spec.watcher.peerStateChangeThrottle` | ピア状態イベントの繰り返し発行を抑える0以上のGo durationです。省略・0は抑制なしです。BGPプロトコルの状態遷移を遅延させる指定ではありません。 |
| `spec.backend` | 既定gobgpで、唯一の対応バックエンドです。FRR BGP設定ファイルではなくrouterd-bgpデーモンと制御クライアントを使います。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/bgp/watcher.go](https://github.com/imksoo/routerd/blob/main/pkg/bgp/watcher.go) · [pkg/controller/bgp/fib_linux.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_linux.go) · [pkg/controller/bgp/fib_bsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_bsd.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `peers`, `prefixes`, `establishedPeers`, `acceptedPrefixes`, `observedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
