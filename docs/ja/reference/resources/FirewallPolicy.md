# FirewallPolicy

[リソース別の設定](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

ゾーン・明示ルールと併用する共通の暗黙ファイアウォール設定です。複数定義は独立せず集約します。

## 指定条件と参照関係

- 必須のspec項目はありません。logDenyは複数定義のORです。描画側のsameRoleAcceptはtrueで開始しfalseを代入する経路がないため、現行実装ではfalseを指定しても同じ区分間の転送を無効化できません。
- Linux nftablesとFreeBSD PFでは暗黙・明示ルールの順序が違います。PFは許可経路の広い暗黙許可を明示拒否より前に出し、Linuxはゾーン間の明示ルールをその組の暗黙判定より先に出します。明示拒否が暗黙許可・制御通信・状態追跡の全例外を上書きするとは限りません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # 任意; boolean
    sameRoleAccept: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.logDeny` | 既定falseです。いずれかがtrueならLinuxの暗黙input・forward拒否ログを有効にします。明示dropにはそのルールのlogが必要です。現行PFは本項目を読みません。 |
| `spec.sameRoleAccept` | デコード時の既定はfalseですが、実効値は常にtrueでfalseの効果はありません。Linuxではuntrust同士も含みます。PFはuntrust同士の広い暗黙転送許可を出しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
