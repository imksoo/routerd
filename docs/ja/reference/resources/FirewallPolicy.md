# FirewallPolicy

[リソース別の設定](../resources.md#firewallpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

ゾーン・明示ルールと併用する共通の暗黙ファイアウォール設定です。複数定義は独立せず集約します。

## 指定条件と参照関係

- 必須のspec項目はありません。logDenyは複数定義のORです。描画側のsameRoleAcceptはtrueで開始しfalseを代入する経路がないため、現行実装ではfalseを指定しても同じ区分間の転送を無効化できません。
- Linux nftablesとFreeBSD PFでは暗黙・明示ルールの順序が違います。PFは許可経路の広い暗黙許可を明示拒否より前に出し、Linuxはゾーン間の明示ルールをその組の暗黙判定より先に出します。明示拒否が暗黙許可・制御通信・状態追跡の全例外を上書きするとは限りません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    [ logDeny: <boolean> ]
    [ sameRoleAccept: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.logDeny` | *任意* | boolean | 既定falseです。いずれかがtrueならLinuxの暗黙input・forward拒否ログを有効にします。明示dropにはそのルールのlogが必要です。現行PFは本項目を読みません。 |
| `spec.sameRoleAccept` | *任意* | boolean | デコード時の既定はfalseですが、実効値は常にtrueでfalseの効果はありません。Linuxではuntrust同士も含みます。PFはuntrust同士の広い暗黙転送許可を出しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
