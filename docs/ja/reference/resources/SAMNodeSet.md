# SAMNodeSet

[リソース別の設定](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

トランスポート・イベントピア・WireGuard・MobilityPoolメンバーで共有するSAMのノード識別・トポロジーを宣言します。ローカルのプロバイダー取得・探索設定はここではなくMobilityPoolの自ノード上書きに置きます。

## 指定条件と参照関係

- nodesは1件以上必要で、前後空白除去後のnodeRef重複は不可です。nodeRefからrole・siteは推定せず、MobilityPool等の利用側が追加条件を課します。
- WireGuard情報を1項目でも指定すればpublicKeyと非空allowedIPsが必須です。endpoint・transportEndpointは形の検証では任意ですが、使用するトランスポートに合わせます。samEndpointとsamEndpointFromは排他です。
- placementグループはrole=cloudとグループ内で同じsiteが必要です。priority・drain=true・正のmaxSecondaryIPsはいずれも非空placement.groupが必要です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必須; array
      - # object
        eventEndpoint: "<string>" # 任意; string
        macAddresses: # 任意; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 任意; object
          drain: true # 任意; boolean
        maxSecondaryIPs: 0 # 任意; integer; minimum=0
        nodeRef: "<string>" # 必須; string
        placement: # 任意; object
          group: "<string>" # 任意; string
          priority: 0 # 任意; integer; minimum=0; maximum=1000000
        role: "" # 任意; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 任意; boolean
        samEndpoint: "<string>" # 任意; string
        samEndpointFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        site: "<string>" # 任意; string
        wireGuard: # 任意; object
          allowedIPs: # 任意; array
            - "<string>" # string
          endpoint: "<string>" # 任意; string
          persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 任意; string
          transportEndpoint: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.nodes` | 必須の非空共有ノード一覧です。自ノードの自動追加はありません。 |
| `spec.nodes[].nodeRef` | 必須の安定したノード識別子で、本NodeSet内で一意です。EventGroupのローカル識別子やSAMTransportProfile.selfNodeRefと照合し、Kind/name参照ではありません。 |
| `spec.nodes[].site` | 任意の拠点ラベルで、既定は空です。roleからは導出せず、MobilityPoolメンバー・placementの規則に応じた明示が必要です。 |
| `spec.nodes[].role` | NodeSet検証では空・onprem・cloudを指定でき、自動既定値はありません。MobilityPoolメンバーでは実際のonprem・cloudを指定し、placementはcloud限定です。 |
| `spec.nodes[].routeReflector` | 既定falseです。登録トポロジーでRR識別子として選ぶ対象を示し、これだけでBGPRouter作成・BGP接続は行いません。 |
| `spec.nodes[].eventEndpoint` | 生成するイベントピアの任意の絶対HTTP(S) URLです。hostが必要で、既定・状態値参照はありません。 |
| `spec.nodes[].samEndpoint` | ノード外側トランスポート終端の任意のIPv4アドレス・CIDRです。DNS名は使えず、samEndpointFromと排他です。WireGuard暗号化ではwireGuard.transportEndpointが優先します。 |
| `spec.nodes[].samEndpointFrom` | 共通resource・field規則に従う任意の実行時状態参照です。値がなければトランスポート参照は未解決となり、optionalで終端を補完したり不正アドレスを許容したりはしません。 |
| `spec.nodes[].samEndpointFrom.resource` | 利用時必須の宣言済み参照元Kind/nameで、固定samEndpointと排他です。 |
| `spec.nodes[].samEndpointFrom.field` | resource指定時に必須の提供状態フィールドです。例はDHCPv4ClientのcurrentAddressです。 |
| `spec.nodes[].samEndpointFrom.optional` | 既定falseです。存在・field検証を省略せず、この利用側は終端欠落を引き続き未解決とします。 |
| `spec.nodes[].macAddresses` | 任意のMACアドレス一覧で、既定は空です。ARP観測ではSAMルーターを端末として再発見しないための送信元除外に使います。MAC変更指定ではありません。 |
| `spec.nodes[].wireGuard` | 任意のWireGuard公開情報です。1つでも非空ならpublicKey・allowedIPsが必須で、秘密鍵はNodeSetトポロジーに置きません。 |
| `spec.nodes[].wireGuard.publicKey` | wireGuard利用時に必須の相手公開鍵で、生成WireGuardPeerへ渡します。自動既定値はありません。 |
| `spec.nodes[].wireGuard.endpoint` | 任意のWireGuard外側UDP host:portです。IPv6は[IPv6]:portで、ポートは1～65535です。内側transportEndpointとは別です。 |
| `spec.nodes[].wireGuard.transportEndpoint` | encryption=wireguard時に使う内側SAMトンネル終端の任意のIPv4アドレス・CIDRです。samEndpoint・samEndpointFromより優先し、UDPソケットのアドレスとは別です。 |
| `spec.nodes[].wireGuard.allowedIPs` | WireGuard利用時必須の非空IPv4・IPv6 CIDR一覧で、生成ピアへ渡します。各prefixの通常経路を自動生成する指定ではありません。 |
| `spec.nodes[].wireGuard.persistentKeepalive` | 0～65535の整数秒です。既定0では継続キープアライブを無効にし、生成ピアへ値を渡します。 |
| `spec.nodes[].placement` | 任意の共有取得配置情報です。グループ指定はcloudロール限定で、メンバーのsite・roleと利用側のプロバイダー識別に整合が必要です。 |
| `spec.nodes[].placement.group` | 任意の配置グループ名で、既定は空（グループ選択なし）です。priority・drain・正の容量指定時は必須です。 |
| `spec.nodes[].placement.priority` | 0～1000000の整数です。省略・0は解決したメンバー順に10・20…を割り当て、明示値を避けます。single-active選択は有効な小さい優先度が勝ち、非0はgroupが必要です。 |
| `spec.nodes[].maintenance` | 任意の宣言的保守指定です。既定はdrainなしです。共有トポロジー情報で、ローカルプロバイダー認証情報ではありません。 |
| `spec.nodes[].maintenance.drain` | 既定falseです。trueはグループメンバーをactive取得候補から除き、placement.groupが必要です。 |
| `spec.nodes[].maxSecondaryIPs` | 0以上の整数で、既定0です。正の値はplacement.groupが必要で、そのグループの容量考慮分散取得を有効にします。0はメンバー容量上限なしで、クラウド側のクォータ変更ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `nodeCount`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
