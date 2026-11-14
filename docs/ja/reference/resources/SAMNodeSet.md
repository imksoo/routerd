# SAMNodeSet

[リソース別の設定](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

トランスポート・イベントピア・WireGuard・MobilityPoolメンバーで共有するSAMのノード識別・トポロジーを宣言します。ローカルのプロバイダー取得・探索設定はここではなくMobilityPoolの自ノード上書きに置きます。

## 指定条件と参照関係

- nodesは1件以上必要で、前後空白除去後のnodeRef重複は不可です。nodeRefからrole・siteは推定せず、MobilityPool等の利用側が追加条件を課します。
- WireGuard情報を1項目でも指定すればpublicKeyと非空allowedIPsが必須です。endpoint・transportEndpointは形の検証では任意ですが、使用するトランスポートに合わせます。samEndpointとsamEndpointFromは排他です。
- placementグループはrole=cloudとグループ内で同じsiteが必要です。priority・drain=true・正のmaxSecondaryIPsはいずれも非空placement.groupが必要です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes:
      -
        [ eventEndpoint: <string> ]
        [ macAddresses:
            - <string>
        ]
        [ maintenance:
            [ drain: <boolean> ]
        ]
        [ maxSecondaryIPs: <integer> ]
        nodeRef: <string>
        [ placement:
            [ group: <string> ]
            [ priority: <integer> ]
        ]
        [ role: ("" | "onprem" | "cloud") ]
        [ routeReflector: <boolean> ]
        [ samEndpoint: <string> ]
        [ samEndpointFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ site: <string> ]
        [ wireGuard:
            [ allowedIPs:
                - <string>
            ]
            [ endpoint: <string> ]
            [ persistentKeepalive: <integer> ]
            [ publicKey: <string> ]
            [ transportEndpoint: <string> ]
        ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.nodes` | **必須** | array | 必須の非空共有ノード一覧です。自ノードの自動追加はありません。 |
| `spec.nodes[].nodeRef` | **必須** | string | 必須の安定したノード識別子で、本NodeSet内で一意です。EventGroupのローカル識別子やSAMTransportProfile.selfNodeRefと照合し、Kind/name参照ではありません。 |
| `spec.nodes[].site` | *任意* | string | 任意の拠点ラベルで、既定は空です。roleからは導出せず、MobilityPoolメンバー・placementの規則に応じた明示が必要です。 |
| `spec.nodes[].role` | *任意* | string; enum=["", "onprem", "cloud"] | NodeSet検証では空・onprem・cloudを指定でき、自動既定値はありません。MobilityPoolメンバーでは実際のonprem・cloudを指定し、placementはcloud限定です。 |
| `spec.nodes[].routeReflector` | *任意* | boolean | 既定falseです。登録トポロジーでRR識別子として選ぶ対象を示し、これだけでBGPRouter作成・BGP接続は行いません。 |
| `spec.nodes[].eventEndpoint` | *任意* | string | 生成するイベントピアの任意の絶対HTTP(S) URLです。hostが必要で、既定・状態値参照はありません。 |
| `spec.nodes[].samEndpoint` | *任意* | string | ノード外側トランスポート終端の任意のIPv4アドレス・CIDRです。DNS名は使えず、samEndpointFromと排他です。WireGuard暗号化ではwireGuard.transportEndpointが優先します。 |
| `spec.nodes[].samEndpointFrom` | *任意* | object | 共通resource・field規則に従う任意の実行時状態参照です。値がなければトランスポート参照は未解決となり、optionalで終端を補完したり不正アドレスを許容したりはしません。 |
| `spec.nodes[].samEndpointFrom.resource` | **条件付き必須** | string | 利用時必須の宣言済み参照元Kind/nameで、固定samEndpointと排他です。 |
| `spec.nodes[].samEndpointFrom.field` | **条件付き必須** | string | resource指定時に必須の提供状態フィールドです。例はDHCPv4ClientのcurrentAddressです。 |
| `spec.nodes[].samEndpointFrom.optional` | *任意* | boolean | 既定falseです。存在・field検証を省略せず、この利用側は終端欠落を引き続き未解決とします。 |
| `spec.nodes[].macAddresses` | *任意* | array | 任意のMACアドレス一覧で、既定は空です。ARP観測ではSAMルーターを端末として再発見しないための送信元除外に使います。MAC変更指定ではありません。 |
| `spec.nodes[].wireGuard` | *任意* | object | 任意のWireGuard公開情報です。1つでも非空ならpublicKey・allowedIPsが必須で、秘密鍵はNodeSetトポロジーに置きません。 |
| `spec.nodes[].wireGuard.publicKey` | *任意* | string | wireGuard利用時に必須の相手公開鍵で、生成WireGuardPeerへ渡します。自動既定値はありません。 |
| `spec.nodes[].wireGuard.endpoint` | *任意* | string | 任意のWireGuard外側UDP host:portです。IPv6は[IPv6]:portで、ポートは1～65535です。内側transportEndpointとは別です。 |
| `spec.nodes[].wireGuard.transportEndpoint` | *任意* | string | encryption=wireguard時に使う内側SAMトンネル終端の任意のIPv4アドレス・CIDRです。samEndpoint・samEndpointFromより優先し、UDPソケットのアドレスとは別です。 |
| `spec.nodes[].wireGuard.allowedIPs` | *任意* | array | WireGuard利用時必須の非空IPv4・IPv6 CIDR一覧で、生成ピアへ渡します。各prefixの通常経路を自動生成する指定ではありません。 |
| `spec.nodes[].wireGuard.persistentKeepalive` | *任意* | integer; minimum=0; maximum=65535 | 0～65535の整数秒です。既定0では継続キープアライブを無効にし、生成ピアへ値を渡します。 |
| `spec.nodes[].placement` | *任意* | object | 任意の共有取得配置情報です。グループ指定はcloudロール限定で、メンバーのsite・roleと利用側のプロバイダー識別に整合が必要です。 |
| `spec.nodes[].placement.group` | *任意* | string | 任意の配置グループ名で、既定は空（グループ選択なし）です。priority・drain・正の容量指定時は必須です。 |
| `spec.nodes[].placement.priority` | *任意* | integer; minimum=0; maximum=1000000 | 0～1000000の整数です。省略・0は解決したメンバー順に10・20…を割り当て、明示値を避けます。single-active選択は有効な小さい優先度が勝ち、非0はgroupが必要です。 |
| `spec.nodes[].maintenance` | *任意* | object | 任意の宣言的保守指定です。既定はdrainなしです。共有トポロジー情報で、ローカルプロバイダー認証情報ではありません。 |
| `spec.nodes[].maintenance.drain` | *任意* | boolean | 既定falseです。trueはグループメンバーをactive取得候補から除き、placement.groupが必要です。 |
| `spec.nodes[].maxSecondaryIPs` | *任意* | integer; minimum=0 | 0以上の整数で、既定0です。正の値はplacement.groupが必要で、そのグループの容量考慮分散取得を有効にします。0はメンバー容量上限なしで、クラウド側のクォータ変更ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `nodeCount`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
