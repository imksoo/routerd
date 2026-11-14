# FirewallZone

[リソース別の設定](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

設定済みインターフェースを名前付きの信頼区分にまとめ、ファイアウォール生成に使います。OSインターフェースや独立した経路・ブリッジ領域は作りません。

## 指定条件と参照関係

- roleと1件以上のinterfacesが必須で、同一ゾーン内の同じインターフェース文字列の重複は禁止です。FirewallRuleはmetadata.nameを参照します。selfは通常のインターフェースゾーンでなく、ルーター自身を示す特別な宛先です。
- 暗黙方針はmgmtから全区分・self、trustからself・mgmt以外を許可します。untrustは原則拒否ですが、現行の共通方針では同一区分間の転送を許可します。確立済み・関連通信と制御プロトコルの例外は別に評価します。OSごとの順序制約はFirewallPolicy・FirewallRuleを参照してください。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必須; array
      - "<string>" # string
    role: "untrust" # 必須; string; enum=["untrust", "trust", "mgmt"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.role` | untrust・trust・mgmtのいずれかが必須で、既定値はありません。暗黙の通信方針を選び、所有権・認証の設定ではありません。 |
| `spec.interfaces` | 1件以上の論理インターフェース参照が必須です。裸の名前はInterfaceとして扱い、Interface/name・PPPoESession/name・WireGuardInterface/name・DSLiteTunnel/nameを受け付けます。設定内のインターフェース・トンネル索引で解決でき、描画時にOS名が必要です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
