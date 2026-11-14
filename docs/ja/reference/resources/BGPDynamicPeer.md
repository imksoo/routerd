# BGPDynamicPeer

[リソース別の設定](../resources.md#bgpdynamicpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

GoBGPの動的着信ピア受付グループを定義します。個々のピアやWireGuard・SAMの認証識別子ではありません。

## 指定条件と参照関係

- ピアの受信ポリシーが設定済みと判断されるとルーター側を全体置換し、項目ごとのマージは行いません。一方、送信prefixはルーター・実行時由来の広告prefixと合成します。
- 現行コントローラーはIPv4・IPv6 unicastを両方有効にします。Establishedだけでは全経路のFIB適用許可や実通信の疎通成功を意味しません。
- Readyはピアグループと送信元受付規則を適用した状態で、動的ピアのEstablishedを意味しません。動的ピアのGR既定値はルーターの収束・GR設定を使います。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
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
    listen: # 必須; object
      sourcePrefixes: # 必須; array
        - "<string>" # string
    password: "<string>" # 任意; string
    passwordFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    peerASN: 1 # 必須; integer; minimum=1
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

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.importPolicy` | 動的leafの受信にはIPv4許可一覧とmin=max=32が必須です。ピアの許可一覧が空なら検証はルーター側を全体採用し、許可一覧なしのピアnextHopRewriteを拒否します。補助項目だけの上書きでなく、ここにポリシー全体を指定してください。 |
| `spec.importPolicy.allowedPrefixes` | 任意のIPv4・IPv6 CIDR許可一覧です。プレフィックスと許可した詳細経路長で照合し、正規化後の同一プレフィックスは拒否します。空なら他条件で構成された受信ポリシーにprefix条件を加えません。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 0～128の整数です。省略・0は各許可プレフィックス自身の長さです。正の指定値は各prefix長以上、ファミリー上限以下、正の最大長以下が必要です。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 0～128の整数です。省略・0はIPv4が32、IPv6が128です。正の値は全許可prefix長以上かつファミリー上限以下が必要です。動的leaf受付はmin=max=32が必須です。 |
| `spec.importPolicy.requiredCommunities` | 任意のcommunity一覧です。受理する経路はすべての指定communityを持つ必要があり、空なら必須community条件はありません。 |
| `spec.importPolicy.forbiddenCommunities` | 任意の禁止community一覧です。どれか1つでも一致すれば許可判定より先に拒否します。空ならこの禁止条件はありません。 |
| `spec.importPolicy.nextHopRewrite` | 既定はpeer-addressです。unchangedなら広告されたnext hopを維持します。受理経路の選択・FIB投影前に適用します。ピアでこの項目だけを指定しても独自ポリシーとなり、ルーターの許可一覧は継承しません。 |
| `spec.importPolicy.localPreference` | 受理経路へbest-path選択前に設定するuint32のLOCAL_PREFです。既定0ならBGP実装の既定値を変更しません。 |
| `spec.exportPolicy` | 任意の送信prefix条件です。ピア側の一覧はルーター由来の静的・動的広告と合成し、RRクライアントではルーターの受信prefixも加えます。ルーター由来の送信prefix全体を絞る置換ではありません。 |
| `spec.exportPolicy.allowedPrefixes` | 任意のCIDR一覧で既定は空です。合成後に非空なら各prefixとその詳細経路（/32・/128まで）を許可し、非一致を拒否します。合成後も空なら送信フィルターを設定しません。 |
| `spec.timers` | 任意のタイマープロファイルです。現行コントローラーではルーターのtimersをピアへ継承しないため、必要なら各ピアで明示します。 |
| `spec.timers.profile` | 既定・省略はkeepalive 30秒・hold 90秒・connectRetry 10秒、fastは3秒・9秒・1秒、slowは60秒・180秒・30秒です。個別のkeepalive・holdTime・connectRetryのYAML指定は非対応です。 |
| `spec.when` | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |
| `spec.routerRef` | 同じRouter設定内のBGPRouterを指すBGPRouter/nameが必須です。名前だけの指定は不可です。 |
| `spec.peerASN` | 必須の0以外のuint32相手ASNです。ローカルASNと一致すればiBGPになり、routeReflectorClientでは一致が必要です。 |
| `spec.password` | 任意のBGP TCP認証鍵です。既定なしで、非空passwordFrom参照と排他です。SAM登録トークンではありません。 |
| `spec.passwordFrom` | 任意の秘密値参照で、利用時はfile・envのどちらか一方を指定します。省略時は外部参照なしです。状態値・Kind/name参照ではなく、実行時のファイル・環境変数欠落はピア調整を失敗させます。 |
| `spec.passwordFrom.file` | 生の鍵を格納したルーター上のファイルです。env・直接passwordと排他で、末尾CR・LFだけを除去し、その他の空白は保持します。 |
| `spec.passwordFrom.env` | 秘密値を格納するrouterdプロセス環境変数の名前です。値そのものではありません。file・直接passwordとは排他で、環境変数ファイルの自動読込は行いません。 |
| `spec.passwordFrom.base64` | 既定falseです。trueならfile・env値を標準Base64デコードし、末尾CR・LFを除去します。暗号化ではなく符号化です。 |
| `spec.ebgpMultihop` | 0～255の整数で既定0です。2以上の場合だけGoBGPのeBGP multihopを有効にしてTTLを設定し、0・1では無効です。 |
| `spec.routeReflectorClient` | 既定falseです。trueはiBGPのRRクライアント指定で、peerASNとルーターASNの一致が必要です。実効送信prefixにはルーターの受信prefixも加えます。 |
| `spec.routeReflectorClusterID` | 任意のIPv4形式クラスタIDで、routeReflectorClient=trueの場合だけ渡します。空ならGoBGPに任せ、これだけ指定しても反射は有効になりません。 |
| `spec.listen` | 必須の着信受付規則で、発見したピア・SAM登録オブジェクトではありません。 |
| `spec.listen.sourcePrefixes` | GoBGP動的ピアの送信元IPv4・IPv6 CIDRの非空一覧が必須で、正規化後の重複は拒否します。着信TCPの相手アドレスを制限するもので、広告できる経路は別のimportPolicyで制限します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `peerGroup`, `sourcePrefixes`, `sourcePrefixCount`, `discoveredPeers`, `discoveredPeerCount`, `acceptedRouteCount`, `rejectedRouteCount`, `rejectedRouteSummary`, `observedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
