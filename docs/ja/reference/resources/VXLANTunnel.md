# VXLANTunnel

[リソース別の設定](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

静的なフラッド先と任意のブリッジ接続を持つLinuxのVXLANデバイスを管理します。マルチキャスト探索や動的MAC学習は設定しません。

## 指定条件と参照関係

- 実動作コントローラーはLinux以外では適用せず終了します。本リソースはLinuxを対象にし、スキーマに通ることをFreeBSD対応と解釈しないでください。
- whenは成立を確認できない場合に転送を止めます。偽・未解決・古い状態・起動前の状態では更新を省略するだけでなく、デバイスをdown後に削除します。参照状態の有効期間は既定30秒でmaxAgeで変更できます。unknownを許容する条件は検証で拒否します。
- tcpMSSClampにはbridgeと明示的なmtu 1280以上が必要です。生成時には実際のBridgeリソースと解決可能なメンバーが必要で、全体の参照検証だけでは共通インターフェース索引の存在しか確認しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    [ bridge: <string> ]
    [ ifname: <string> ]
    localAddress: <string>
    [ mtu: <integer> ]
    [ outerDF: ("" | "inherit" | "set" | "unset") ]
    [ peers:
        - <string>
    ]
    [ tcpMSSClamp: <boolean> ]
    [ udpPort: <integer> ]
    underlayInterface: <string>
    vni: <integer>
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
| `spec.ifname` | *任意* | string | OS上のVXLAN名です。既定はmetadata.nameで、15文字以内・空白とスラッシュ不可です。 |
| `spec.vni` | **必須** | integer; minimum=1; maximum=16777215 | 必須のVXLANネットワーク識別子で、1～16777215の整数です。対向のL2セグメントと合わせます。 |
| `spec.localAddress` | **必須** | string | 必須の外側ローカルIPアドレスです。動的状態参照はありません。検証はIP形式のみで、ローカルへの割当・対向とのファミリー一致までは確認しません。 |
| `spec.peers` | *任意* | array | 任意の外側対向IP一覧で、既定は空です。00:00:00:00:00:00のpermanent self FDBエントリーとして同期し、不要なフラッド先は削除します。空なら静的な対向フラッド先はありません。 |
| `spec.underlayInterface` | **必須** | string | 同じRouterで宣言した論理インターフェース名が必須で、OSデバイス名へ解決します。状態参照や自動選択はありません。 |
| `spec.udpPort` | *任意* | integer; minimum=1; maximum=65535 | 外側UDP宛先ポートです。省略・0では4789、明示値は1～65535です。 |
| `spec.mtu` | **条件付き必須** | integer; minimum=0 | 任意の内側デバイスMTUで576～9216です。省略・0ならOS既定・既存値を使います。スキーマ下限は0ですが、tcpMSSClamp時は1280以上の明示が必要です。 |
| `spec.bridge` | **条件付き必須** | string | 任意の宣言済みブリッジ名で、OS名へ解決してVXLANのmasterに接続します。既定は未接続です。tcpMSSClampでは必須で、他種のインターフェースではなくBridgeリソースを使います。 |
| `spec.outerDF` | *任意* | string; enum=["", "inherit", "set", "unset"] | 外側IPv4のDF指定で、既定inherit、またはset・unsetです。ip linkのVXLAN dfへ渡します。内側アドレス・経路設定ではありません。 |
| `spec.when` | *任意* | object | 任意の共通state・all・any条件です。前述の厳密な鮮度確認も適用します。省略時は無条件で、unknownを受け入れる条件は使えません。 |
| `spec.tcpMSSClamp` | *任意* | boolean | 既定falseです。trueならVXLANとBridgeメンバーポート間にLinux bridgeファミリーのIPv4・IPv6 TCP SYN MSS規則を生成します。正のVXLAN・bridge・メンバーMTUの最小値からIPv4は40、IPv6は60を引き、任意のルーティング通信全般には適用しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `ifname`, `vni`
