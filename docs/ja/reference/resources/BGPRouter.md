# BGPRouter

[リソース別の設定](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

ローカルGoBGPインスタンス・受理経路・自起源広告を設定します。カーネルへの適用や対向疎通はBGPのEstablishedとは別です。

## 指定条件と参照関係

- 実動作はBGPRouter1件・VRFなしだけに対応します。ASN・VRF・待受が異なれば設定検証に通ることと、実動作対応は別です。
- ルーターの受信一覧・connected一覧・static一覧の3群をまたぐ重複・包含は不可です。各prefix一覧内の正規化後重複も拒否します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: <integer>
    [ backend: ("" | "gobgp") ]
    [ communities:
        [ accept:
            - <string>
        ]
        [ send: ("" | "standard" | "extended" | "both") ]
        [ set:
            [ in:
                - <string>
            ]
            [ out:
                - <string>
            ]
        ]
    ]
    [ convergenceProfile: ("" | "default" | "fast" | "stable") ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
    ]
    [ gracefulRestart:
        [ enabled: <boolean> ]
        [ restartTime: <string> ]
        [ stalePathTime: <string> ]
    ]
    [ importPolicy:
        [ allowedPrefixLengthMax: <integer> ]
        [ allowedPrefixLengthMin: <integer> ]
        [ allowedPrefixes:
            - <string>
        ]
        [ forbiddenCommunities:
            - <string>
        ]
        [ localPreference: <integer> ]
        [ nextHopRewrite: ("" | "peer-address" | "unchanged") ]
        [ requiredCommunities:
            - <string>
        ]
    ]
    [ listen:
        [ address: <string> ]
        [ port: <integer> ]
    ]
    [ redistribute:
        [ connected:
            [ allowedPrefixes:
                - <string>
            ]
        ]
        [ static:
            [ allowedPrefixes:
                - <string>
            ]
        ]
    ]
    routerID: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
    ]
    [ vrf: <string> ]
    [ watcher:
        [ maxPrefixes: <integer> ]
        [ peerStateChangeThrottle: <string> ]
        [ pollInterval: <string> ]
    ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.importPolicy` | *任意* | object | 独自指定のないピアが継承するルーター全体の受信ポリシーです。空ならルーター受信フィルターを作りませんが、受信した全経路のカーネルFIB適用を自動許可するものではありません。 |
| `spec.importPolicy.allowedPrefixes` | *任意* | array | 任意のIPv4・IPv6 CIDR許可一覧です。プレフィックスと許可した詳細経路長で照合し、正規化後の同一プレフィックスは拒否します。空なら他条件で構成された受信ポリシーにprefix条件を加えません。 |
| `spec.importPolicy.allowedPrefixLengthMin` | *任意* | integer; minimum=0; maximum=128 | 0～128の整数です。省略・0は各許可プレフィックス自身の長さです。正の指定値は各prefix長以上、ファミリー上限以下、正の最大長以下が必要です。 |
| `spec.importPolicy.allowedPrefixLengthMax` | *任意* | integer; minimum=0; maximum=128 | 0～128の整数です。省略・0はIPv4が32、IPv6が128です。正の値は全許可prefix長以上かつファミリー上限以下が必要です。動的leaf受付はmin=max=32が必須です。 |
| `spec.importPolicy.requiredCommunities` | *任意* | array | 任意のcommunity一覧です。受理する経路はすべての指定communityを持つ必要があり、空なら必須community条件はありません。 |
| `spec.importPolicy.forbiddenCommunities` | *任意* | array | 任意の禁止community一覧です。どれか1つでも一致すれば許可判定より先に拒否します。空ならこの禁止条件はありません。 |
| `spec.importPolicy.nextHopRewrite` | *任意* | string; enum=["", "peer-address", "unchanged"] | 既定はpeer-addressです。unchangedなら広告されたnext hopを維持します。受理経路の選択・FIB投影前に適用します。ピアでこの項目だけを指定しても独自ポリシーとなり、ルーターの許可一覧は継承しません。 |
| `spec.importPolicy.localPreference` | *任意* | integer | 受理経路へbest-path選択前に設定するuint32のLOCAL_PREFです。既定0ならBGP実装の既定値を変更しません。 |
| `spec.exportPolicy` | *任意* | object | 任意の送信prefix条件です。ピア側の一覧はルーター由来の静的・動的広告と合成し、RRクライアントではルーターの受信prefixも加えます。ルーター由来の送信prefix全体を絞る置換ではありません。 |
| `spec.exportPolicy.allowedPrefixes` | *任意* | array | 明示的な自起源広告CIDRで、既定は空です。redistributeのconnected・staticと動的広告へ合成し、ピアの送信条件にも使います。OS経路の存在確認なしで広告します。 |
| `spec.timers` | *任意* | object | 任意のタイマープロファイルです。現行コントローラーではルーターのtimersをピアへ継承しないため、必要なら各ピアで明示します。 |
| `spec.timers.profile` | *任意* | string; enum=["", "default", "fast", "slow"] | default・fast・slowを受け付けますが、現行実行処理はルーター側値からピアタイマーを設定しません。BGPPeer・BGPDynamicPeerのtimers.profileに指定してください。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |
| `spec.asn` | **必須** | integer; minimum=1 | 必須の0以外のuint32ローカルASNです。設定ではBGPRouter間の再利用を拒否し、現行実行処理はBGPRouter1件だけに対応します。 |
| `spec.routerID` | **必須** | string | 必須のIPv4形式のBGP識別子で、通信アドレスとは別です。インターフェース指定のないBFDはローカルmultihopアドレスにも使います。 |
| `spec.vrf` | *任意* | string | 任意のVRF/nameまたはVRF名で、宣言済みVRFを参照します。設定検証は受け付けますが実動作GoBGPコントローラーは非空値をGoBGPVRFUnsupportedとして拒否します。実運用では省略します。 |
| `spec.listen` | *任意* | object | 任意のローカルTCP待受設定で、既定は全アドレス・179番です。相手のTCPポート選択ではありません。 |
| `spec.listen.address` | *任意* | string | 任意のIPv4・IPv6バインド先です。空ならGoBGPは全アドレスで待ち受けます。IP指定で、リソース参照・ホスト名は不可です。 |
| `spec.listen.port` | *任意* | integer; minimum=1; maximum=65535 | TCP待受ポート1～65535で、省略・0は179です。設定されたアドレス・ポートの競合は拒否します。 |
| `spec.redistribute` | *任意* | object | 任意の明示広告prefix群です。現行実行処理はconnected・static一覧とexportPolicy.allowedPrefixesを合成し、OSから該当経路を検出する処理ではありません。 |
| `spec.redistribute.connected` | *任意* | object | connected名の任意広告群で、既定は空です。この名前によってカーネルの直結経路存在を広告条件にするものではありません。 |
| `spec.redistribute.connected.allowedPrefixes` | *任意* | array | 自起源広告するIPv4・IPv6 CIDRで、既定は空です。ルーターの受信一覧・static一覧と重複・包含不可です。対応する直結経路の存在は要求しません。 |
| `spec.redistribute.static` | *任意* | object | static名の任意広告群で、既定は空です。IPv4StaticRoute・IPv6StaticRouteを自動列挙する指定ではありません。 |
| `spec.redistribute.static.allowedPrefixes` | *任意* | array | 自起源広告するIPv4・IPv6 CIDRで、既定は空です。ルーターの受信一覧・connected一覧と重複・包含不可です。カーネル静的経路の存在は確認しません。 |
| `spec.communities` | *任意* | object | 任意のcommunity設定です。現行実行処理ではルーターのset.outだけが自起源広告に作用し、send・accept・set.inは読取・検証のみで使いません。 |
| `spec.communities.send` | *任意* | string; enum=["", "standard", "extended", "both"] | standard・extended・both・空を受け付けますが実動作には影響せず、GoBGPピアはこの指定によらずSendCommunity=3です。 |
| `spec.communities.accept` | *任意* | array | 検証される任意の標準community一覧で、既定は空です。現行処理では受信を絞りません。実際の照合にはimportPolicyのrequiredCommunities・forbiddenCommunitiesを使います。 |
| `spec.communities.set` | *任意* | object | 任意の受信・送信community一覧で、現行コントローラーはルーターのoutだけを使います。 |
| `spec.communities.set.in` | *任意* | array | 任意の検証対象一覧で、既定は空です。現行処理では受信経路に付与しません。 |
| `spec.communities.set.out` | *任意* | array | routerd自起源広告へ付与する標準communityで、既定は空です。全通過経路の書換ではありません。標準の16bit成分のasn:valueや対応する既知名を使い、ここでextended communityは生成しません。 |
| `spec.convergenceProfile` | *任意* | string; enum=["", "default", "fast", "stable"] | 既定・省略・stableではGRを既定有効、fastでは既定無効にします。gracefulRestart.enabledを明示すれば優先します。timers.profileの自動選択ではありません。 |
| `spec.gracefulRestart` | *任意* | object | 任意のGR設定です。収束プロファイル選択後にピアにも使い、enabled明示値はプロファイル既定より優先します。 |
| `spec.gracefulRestart.enabled` | *任意* | boolean | 任意の真偽値です。fast以外は既定trueです。falseなら時間指定があってもGRを無効にします。 |
| `spec.gracefulRestart.restartTime` | *任意* | string | 正のGo durationで、既定120秒です。秒未満を切り捨てて整数秒へ変換するため、1秒未満は0になり得ます。整数秒を使ってください。 |
| `spec.gracefulRestart.stalePathTime` | *任意* | string | 古い経路の保持期間を指定する正のGo durationで、既定360秒です。整数秒へ変換し、GR無効時は使いません。 |
| `spec.watcher` | *任意* | object | 任意の状態観測設定で、BGPのkeepalive・holdタイマーや接続受付制限ではありません。 |
| `spec.watcher.pollInterval` | *任意* | string | 3秒以上のGo durationです。実間隔は15秒を起点に短い指定値だけ採用するため、15秒超を指定しても長くなりません。 |
| `spec.watcher.maxPrefixes` | *任意* | integer; minimum=1; maximum=999999 | 観測するprefix上限で、既定4096です。正の明示値は1000000未満です。BGPセッションのmaximum-prefix停止ポリシーを設定する項目ではありません。 |
| `spec.watcher.peerStateChangeThrottle` | *任意* | string | ピア状態イベントの繰り返し発行を抑える0以上のGo durationです。省略・0は抑制なしです。BGPプロトコルの状態遷移を遅延させる指定ではありません。 |
| `spec.backend` | *任意* | string; enum=["", "gobgp"] | 既定gobgpで、唯一の対応バックエンドです。FRR BGP設定ファイルではなくrouterd-bgpデーモンと制御クライアントを使います。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/bgp/watcher.go](https://github.com/imksoo/routerd/blob/main/pkg/bgp/watcher.go) · [pkg/controller/bgp/fib_linux.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_linux.go) · [pkg/controller/bgp/fib_bsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_bsd.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `peers`, `prefixes`, `establishedPeers`, `acceptedPrefixes`, `observedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
