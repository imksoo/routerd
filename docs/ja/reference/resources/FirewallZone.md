# FirewallZone

[リソース別の設定](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

設定済みインターフェースを名前付きの信頼区分にまとめ、ファイアウォール生成に使います。OSインターフェースや独立した経路・ブリッジ領域は作りません。

## 指定条件と参照関係

- roleと1件以上のinterfacesが必須で、同一ゾーン内の同じインターフェース文字列の重複は禁止です。FirewallRuleはmetadata.nameを参照します。selfは通常のインターフェースゾーンでなく、ルーター自身を示す特別な宛先です。
- 暗黙方針はmgmtから全区分・self、trustからself・mgmt以外を許可します。untrustは原則拒否ですが、現行の共通方針では同一区分間の転送を許可します。確立済み・関連通信と制御プロトコルの例外は別に評価します。OSごとの順序制約はFirewallPolicy・FirewallRuleを参照してください。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    role: ("untrust" | "trust" | "mgmt")
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.role` | **必須** | string; enum=["untrust", "trust", "mgmt"] | untrust・trust・mgmtのいずれかが必須で、既定値はありません。暗黙の通信方針を選び、所有権・認証の設定ではありません。 |
| `spec.interfaces` | **必須** | array | 1件以上の論理インターフェース参照が必須です。裸の名前はInterfaceとして扱い、Interface/name・PPPoESession/name・WireGuardInterface/name・DSLiteTunnel/nameを受け付けます。設定内のインターフェース・トンネル索引で解決でき、描画時にOS名が必要です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interfaces`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
