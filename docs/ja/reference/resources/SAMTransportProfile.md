# SAMTransportProfile

[リソース別の設定](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

共有トポロジーや許可済みRR・directスナップショットからノード間SAMトンネル・終端/32経路・任意のBGP/BFDを生成します。

## 指定条件と参照関係

- directには必須SAMRRSet後の最後のdirect SAMPeerGroup・pair-stable・BGP生成・routeReflectorClient:false・IPv4受信prefixとmin=max=32・direct未満の正のRR LOCAL_PREF・BGPRouter.communities.set.outの自ノード識別communityが必要です。directが使えなくてもRR経路を維持します。
- 生成RRクライアント・direct受信は/32と相手識別communityを必須化し、他既知ノード識別を禁止します。directのprefixはそのleafの許可所有/32へ置換し、空なら全拒否・nextHopRewriteはpeer-addressです。directプロファイルではRR側もpeer-addressに正規化します。
- ローカルMobilityPoolのないRRクライアントでは明示IPv4受信prefixと、長さ省略または両方32が必要です。FreeBSDはgifN・greNと明示peerAddress、Linuxは短縮トンネル名を生成します。生成成功はトンネル・BGP疎通の証明ではありません。

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

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.selfNodeRef` | 必須の安定した自ノードnodeRefです。ホスト名・router IDから推定しません。 |
| `spec.mode` | 必須のipip・gre・fou・gueです。FreeBSDはipip/gif・greのみで、fou・gueはLinuxが必要です。生成トンネル自体は暗号化しません。 |
| `spec.encryption` | 既定・省略はnoneです。wireguardならノードのwireGuard.transportEndpointを優先します。秘密鍵・ローカルWireGuardInterfaceは生成せず、下位WireGuardは別途設定します。 |
| `spec.innerPrefix` | 必須の長さ31以下のIPv4 CIDRです。/31の辺へ分割するため、トポロジーに十分な枠が必要です。pair-stableの衝突時は終端を移動せずエラーにします。 |
| `spec.addressingMode` | 既定edge-indexは並べ替えた辺へ/31を割り当て、トポロジー変更で番号が変わり得ます。pair-stableはinnerPrefixと順不同ノード対のハッシュで固定枠を選び、direct参照で必須です。 |
| `spec.underlayInterface` | 必須の宣言済み論理インターフェースです。生成トンネルとリモート終端/32デバイス経路に使い、生成トンネルはtrustedUnderlay:trueです。 |
| `spec.localEndpoint` | ローカル外側IPv4の固定値です。localEndpointFromとどちらか一方が必須で、インターフェースアドレスの自動選択はありません。 |
| `spec.localEndpointFrom` | localEndpoint固定値の代わりの状態参照です。共通resource・field規則に従い、未解決なら依存トランスポートは保留です。 |
| `spec.localEndpointFrom.resource` | 参照利用時に必須の既存Kind/nameです。 |
| `spec.localEndpointFrom.field` | currentAddress等の参照元提供フィールドが必須です。 |
| `spec.localEndpointFrom.optional` | 既定falseです。参照検証を省略せず、未解決時の終端補完もしません。 |
| `spec.encapSport` | fou・gueで必須のUDP送信元ポート1～65535です。ipip・greでは禁止（省略・0）です。 |
| `spec.encapDport` | fou・gueで必須のUDP宛先ポート1～65535です。ipip・greでは禁止で、自動既定ポートはありません。 |
| `spec.peersFrom` | 順序付きトポロジー参照元です。publishPeerGroup:true以外は1件以上必須です。自身はピアから除きますが共有トポロジーには含め、後の重複ノードが先の情報を置換し得ます。 |
| `spec.peersFrom[].resource` | 必須のSAMNodeSet/name・SAMPeerGroup/name・SAMEnrollmentPolicy/name・SAMRRSet/nameです。NodeSetは静的で、実行時参照は許可済み・共有スナップショットを供給します。 |
| `spec.peersFrom[].optional` | 既定falseです。非optional参照元欠落は保留です。directの利用不能・非互換でも利用可能なRRピアは除きません。 |
| `spec.peersFrom[].direct` | 既定falseです。trueは必須SAMRRSetに続く最後のSAMPeerGroupのみで、後述のdirect条件が適用されます。 |
| `spec.peersFrom[].nodeRefs` | 任意の空・重複なしnodeRef選択一覧で、省略時は参照元の有効ノード全体です。ローカル隣接を絞るだけで、共有トポロジー自体の置換ではありません。 |
| `spec.publishPeerGroup` | 既定falseです。trueなら自終端を実行時SAMPeerGroup/<本プロファイル名>として公開し、peersFrom空を許可します。秘密認証情報は公開しません。 |
| `spec.bgp` | 必須の生成ピア設定です。参照BGPRouterは既存のものが必要です。 |
| `spec.bgp.generatePeers` | 既定trueです。falseはBGP・BFDを生成せずトランスポートのみ作り、direct参照時は禁止です。 |
| `spec.bgp.timersPreset` | 任意のdefault・fast・slow短縮指定で、bgp.timers.profileより低優先です。両方空なら生成ピアはBGPRouter.timersを継承します。 |
| `spec.bgp.directLocalPreference` | directピアのuint32 LOCAL_PREFで、省略・0は200です。direct指定時は正のRR bgp.importPolicy.localPreferenceを超える必要があります。 |
| `spec.bgp.bfd` | 任意の生成BFD設定で、generatePeers・bfd.enabledがともにtrueの場合だけ使います。 |
| `spec.bgp.bfd.enabled` | 既定falseです。trueなら生成BGPPeerと連携するBFDを導出し、FRR bfdd・vtyshが必要です。 |
| `spec.bgp.routerRef` | 同じRouter設定内のBGPRouterを指すBGPRouter/nameが必須です。名前だけの指定は不可です。 |
| `spec.bgp.peerASN` | 必須の0以外のuint32相手ASNです。ローカルASNと一致すればiBGPになり、routeReflectorClientでは一致が必要です。 |
| `spec.bgp.ebgpMultihop` | 0～255の整数で既定0です。2以上の場合だけGoBGPのeBGP multihopを有効にしてTTLを設定し、0・1では無効です。 |
| `spec.bgp.routeReflectorClient` | 既定falseです。trueはiBGPのRRクライアント指定で、peerASNとルーターASNの一致が必要です。実効送信prefixにはルーターの受信prefixも加えます。 |
| `spec.bgp.routeReflectorClusterID` | 任意のIPv4形式クラスタIDで、routeReflectorClient=trueの場合だけ渡します。空ならGoBGPに任せ、これだけ指定しても反射は有効になりません。 |
| `spec.bgp.convergenceProfile` | 任意のdefault・fast・stableです。省略時はBGPRouterから継承します。timers.profileではなくGR既定値を選び、ルーターのgracefulRestart.enabled明示値がさらに優先します。 |
| `spec.bgp.importPolicy` | 任意の受信ポリシーです。ピアに非空のprefix・community条件・nextHopRewrite・localPreferenceがなければルーター側を全体継承します。長さ制限だけでは上書きと認識しません。 |
| `spec.bgp.importPolicy.allowedPrefixes` | 任意のIPv4・IPv6 CIDR許可一覧です。プレフィックスと許可した詳細経路長で照合し、正規化後の同一プレフィックスは拒否します。空なら他条件で構成された受信ポリシーにprefix条件を加えません。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMin` | 0～128の整数です。省略・0は各許可プレフィックス自身の長さです。正の指定値は各prefix長以上、ファミリー上限以下、正の最大長以下が必要です。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMax` | 0～128の整数です。省略・0はIPv4が32、IPv6が128です。正の値は全許可prefix長以上かつファミリー上限以下が必要です。動的leaf受付はmin=max=32が必須です。 |
| `spec.bgp.importPolicy.requiredCommunities` | 任意のcommunity一覧です。受理する経路はすべての指定communityを持つ必要があり、空なら必須community条件はありません。 |
| `spec.bgp.importPolicy.forbiddenCommunities` | 任意の禁止community一覧です。どれか1つでも一致すれば許可判定より先に拒否します。空ならこの禁止条件はありません。 |
| `spec.bgp.importPolicy.nextHopRewrite` | 既定はpeer-addressです。unchangedなら広告されたnext hopを維持します。受理経路の選択・FIB投影前に適用します。ピアでこの項目だけを指定しても独自ポリシーとなり、ルーターの許可一覧は継承しません。 |
| `spec.bgp.importPolicy.localPreference` | 受理経路へbest-path選択前に設定するuint32のLOCAL_PREFです。既定0ならBGP実装の既定値を変更しません。 |
| `spec.bgp.exportPolicy` | 任意の送信prefix条件です。ピア側の一覧はルーター由来の静的・動的広告と合成し、RRクライアントではルーターの受信prefixも加えます。ルーター由来の送信prefix全体を絞る置換ではありません。 |
| `spec.bgp.exportPolicy.allowedPrefixes` | 任意のCIDR一覧で既定は空です。合成後に非空なら各prefixとその詳細経路（/32・/128まで）を許可し、非一致を拒否します。合成後も空なら送信フィルターを設定しません。 |
| `spec.bgp.timers` | 生成ピアのタイマーで、優先順は本profileのtimers.profile・timersPreset・参照BGPRouter.timersです。 |
| `spec.bgp.timers.profile` | 既定・省略はkeepalive 30秒・hold 90秒・connectRetry 10秒、fastは3秒・9秒・1秒、slowは60秒・180秒・30秒です。個別のkeepalive・holdTime・connectRetryのYAML指定は非対応です。 |
| `spec.bgp.bfd.interface` | 任意のsingle-hopインターフェースです。Interface/nameなら宣言したInterface.ifnameへ解決します。名前だけの場合は論理インターフェースの存在を検証しますが、実行時はその文字列をFRRへ渡すため、Interface/nameを推奨します。省略時はmultihopです。 |
| `spec.bgp.bfd.profile` | fast・省略はminRx=minTx=300ms・倍率3、normalは500ms・500ms・3、slowは1000ms・1000ms・5です。個別指定があれば優先します。 |
| `spec.bgp.bfd.minRx` | 任意のGo durationで50ms～60000msです。プロファイルの受信間隔を上書きして整数msに変換し、省略時はプロファイル値です。BFDプロトコルタイマーであり、ルーター再起動の期限ではありません。 |
| `spec.bgp.bfd.minTx` | 任意のGo durationで50ms～60000msです。プロファイルの送信間隔を上書きし、整数msで出力します。 |
| `spec.bgp.bfd.detectMultiplier` | 任意の整数1～50です。省略・デコード値0ならプロファイル既定（通常3、slowは5）です。交渉された検出間隔の倍率で、routerdの再調整回数ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/transport_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport_support.go) · [pkg/mobilityconfig/sam_transport_slots.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/sam_transport_slots.go) · [pkg/mobilityconfig/transport_mesh.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/transport_mesh.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `reason`, `peersFrom`, `pendingSources`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
