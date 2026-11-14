# BGPPeer

[リソース別の設定](../resources.md#bgppeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣言したBGPRouterに1件以上の静的BGPピアを設定します。

## 指定条件と参照関係

- ピアの受信ポリシーが設定済みと判断されるとルーター側を全体置換し、項目ごとのマージは行いません。一方、送信prefixはルーター・実行時由来の広告prefixと合成します。
- 現行コントローラーはIPv4・IPv6 unicastを両方有効にします。Establishedだけでは全経路のFIB適用許可や実通信の疎通成功を意味しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    [ bfd: <string> ]
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
    [ ebgpMultihop: <integer> ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
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
    [ passiveMode: <boolean> ]
    [ password: <string> ]
    [ passwordFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    peerASN: <integer>
    peers:
      - <string>
    [ routeReflectorClient: <boolean> ]
    [ routeReflectorClusterID: <string> ]
    routerRef: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
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
| `spec.importPolicy` | *任意* | object | 任意の受信ポリシーです。ピアに非空のprefix・community条件・nextHopRewrite・localPreferenceがなければルーター側を全体継承します。長さ制限だけでは上書きと認識しません。 |
| `spec.importPolicy.allowedPrefixes` | *任意* | array | 任意のIPv4・IPv6 CIDR許可一覧です。プレフィックスと許可した詳細経路長で照合し、正規化後の同一プレフィックスは拒否します。空なら他条件で構成された受信ポリシーにprefix条件を加えません。 |
| `spec.importPolicy.allowedPrefixLengthMin` | *任意* | integer; minimum=0; maximum=128 | 0～128の整数です。省略・0は各許可プレフィックス自身の長さです。正の指定値は各prefix長以上、ファミリー上限以下、正の最大長以下が必要です。 |
| `spec.importPolicy.allowedPrefixLengthMax` | *任意* | integer; minimum=0; maximum=128 | 0～128の整数です。省略・0はIPv4が32、IPv6が128です。正の値は全許可prefix長以上かつファミリー上限以下が必要です。動的leaf受付はmin=max=32が必須です。 |
| `spec.importPolicy.requiredCommunities` | *任意* | array | 任意のcommunity一覧です。受理する経路はすべての指定communityを持つ必要があり、空なら必須community条件はありません。 |
| `spec.importPolicy.forbiddenCommunities` | *任意* | array | 任意の禁止community一覧です。どれか1つでも一致すれば許可判定より先に拒否します。空ならこの禁止条件はありません。 |
| `spec.importPolicy.nextHopRewrite` | *任意* | string; enum=["", "peer-address", "unchanged"] | 既定はpeer-addressです。unchangedなら広告されたnext hopを維持します。受理経路の選択・FIB投影前に適用します。ピアでこの項目だけを指定しても独自ポリシーとなり、ルーターの許可一覧は継承しません。 |
| `spec.importPolicy.localPreference` | *任意* | integer | 受理経路へbest-path選択前に設定するuint32のLOCAL_PREFです。既定0ならBGP実装の既定値を変更しません。 |
| `spec.exportPolicy` | *任意* | object | 任意の送信prefix条件です。ピア側の一覧はルーター由来の静的・動的広告と合成し、RRクライアントではルーターの受信prefixも加えます。ルーター由来の送信prefix全体を絞る置換ではありません。 |
| `spec.exportPolicy.allowedPrefixes` | *任意* | array | 任意のCIDR一覧で既定は空です。合成後に非空なら各prefixとその詳細経路（/32・/128まで）を許可し、非一致を拒否します。合成後も空なら送信フィルターを設定しません。 |
| `spec.timers` | *任意* | object | 任意のタイマープロファイルです。現行コントローラーではルーターのtimersをピアへ継承しないため、必要なら各ピアで明示します。 |
| `spec.timers.profile` | *任意* | string; enum=["", "default", "fast", "slow"] | 既定・省略はkeepalive 30秒・hold 90秒・connectRetry 10秒、fastは3秒・9秒・1秒、slowは60秒・180秒・30秒です。個別のkeepalive・holdTime・connectRetryのYAML指定は非対応です。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |
| `spec.routerRef` | **必須** | string | 同じRouter設定内のBGPRouterを指すBGPRouter/nameが必須です。名前だけの指定は不可です。 |
| `spec.peerASN` | **必須** | integer; minimum=1 | 必須の0以外のuint32相手ASNです。ローカルASNと一致すればiBGPになり、routeReflectorClientでは一致が必要です。 |
| `spec.peers` | **必須** | array | 必須の非空アドレス・ホスト名一覧です。前後空白除去後の重複・空白入りは不可です。ホスト名は設定検証で解決せずGoBGPへ渡すため、適用可否は実行時に決まります。別BGPPeer間の同一アドレスはここでは拒否せず、実動作では同じアドレスキーを共有します。 |
| `spec.passiveMode` | *任意* | boolean | 既定falseなら通常の接続開始を行い、trueならGoBGPのpassive transportで着信を待ちます。動的source-prefix受付規則ではありません。 |
| `spec.password` | *任意* | string | 任意のBGP TCP認証鍵です。既定なしで、非空passwordFrom参照と排他です。SAM登録トークンではありません。 |
| `spec.passwordFrom` | *任意* | object | 任意の秘密値参照で、利用時はfile・envのどちらか一方を指定します。省略時は外部参照なしです。状態値・Kind/name参照ではなく、実行時のファイル・環境変数欠落はピア調整を失敗させます。 |
| `spec.passwordFrom.file` | *任意* | string | 生の鍵を格納したルーター上のファイルです。env・直接passwordと排他で、末尾CR・LFだけを除去し、その他の空白は保持します。 |
| `spec.passwordFrom.env` | *任意* | string | 秘密値を格納するrouterdプロセス環境変数の名前です。値そのものではありません。file・直接passwordとは排他で、環境変数ファイルの自動読込は行いません。 |
| `spec.passwordFrom.base64` | *任意* | boolean | 既定falseです。trueならfile・env値を標準Base64デコードし、末尾CR・LFを除去します。暗号化ではなく符号化です。 |
| `spec.ebgpMultihop` | *任意* | integer; minimum=0; maximum=255 | 0～255の整数で既定0です。2以上の場合だけGoBGPのeBGP multihopを有効にしてTTLを設定し、0・1では無効です。 |
| `spec.routeReflectorClient` | *任意* | boolean | 既定falseです。trueはiBGPのRRクライアント指定で、peerASNとルーターASNの一致が必要です。実効送信prefixにはルーターの受信prefixも加えます。 |
| `spec.routeReflectorClusterID` | *任意* | string | 任意のIPv4形式クラスタIDで、routeReflectorClient=trueの場合だけ渡します。空ならGoBGPに任せ、これだけ指定しても反射は有効になりません。 |
| `spec.convergenceProfile` | *任意* | string; enum=["", "default", "fast", "stable"] | 任意のdefault・fast・stableです。省略時はBGPRouterから継承します。timers.profileではなくGR既定値を選び、ルーターのgracefulRestart.enabled明示値がさらに優先します。 |
| `spec.bfd` | *任意* | string | 任意の宣言済みBFD/name参照です。BFDのpeerは本BGPPeerかその相手アドレスに一致する必要があり、Up・Established後のDownでBGPリセットを行います。省略時はBFD連携なしです。 |
| `spec.communities` | *任意* | object | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |
| `spec.communities.send` | *任意* | string; enum=["", "standard", "extended", "both"] | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |
| `spec.communities.accept` | *任意* | array | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |
| `spec.communities.set` | *任意* | object | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |
| `spec.communities.set.in` | *任意* | array | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |
| `spec.communities.set.out` | *任意* | array | 現行BGPPeerでは読取・検証のみで使いません。GoBGP SendCommunityはboth固定で、受信照合はimportPolicyを使います。ピアのcommunity指定はルーターの自起源広告communityを継承・変更しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `address`, `state`, `acceptedPrefixes`, `observedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
