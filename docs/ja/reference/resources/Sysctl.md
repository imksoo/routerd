# Sysctl

[リソース別の設定](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

カーネルのsysctlを1項目反映し、必要ならsysctl.dへ永続設定を書きます。他のrouterdリソースへの参照項目はありません。

## 指定条件と参照関係

- runtime=falseかつpersistent=falseならSkipped/RuntimeDisabledとなり、値を反映しません。runtimeとpersistentは両方trueにできます。
- 同じRouterでのSysctlキーの重複は拒否します。LinuxのNAT44SessionSyncでconntrackdを使う場合、net.netfilter.nf_conntrack_tcp_be_liberalについてruntime有効・optional=false・valueと実効expectedValueが1の明示的Sysctlが別途必要です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    [ compare: ("" | "exact" | "atLeast") ]
    [ expectedValue: <string> ]
    key: <string>
    [ optional: <boolean> ]
    [ persistent: <boolean> ]
    [ runtime: <boolean> ]
    value: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.key` | **必須** | string | net.ipv4.ip_forwardなどのカーネルキー。必須で既定値はなく、空白・タブ・改行・/は使えません。リソース参照ではありません。 |
| `spec.value` | **必須** | string | sysctl -wへ渡す必須の空でない文字列。永続化時もこの値を保存します。数値に見えるYAML値も引用符で囲みます。既定値はありません。 |
| `spec.expectedValue` | *任意* | string | 実行中の値との比較に使う期待値。省略時はvalueです。書込値と観測される表現が異なる場合に使います。空白だけの指定は拒否します。 |
| `spec.compare` | *任意* | string; enum=["", "exact", "atLeast"] | 省略時exactで、空白を正規化した文字列として比較します。atLeastは対応する整数要素を比較し、要素数が同じで、各観測値が期待値以上であることを要求します。 |
| `spec.runtime` | *任意* | boolean | 稼働中のカーネルへ反映する指定。省略時true。falseでもpersistent=trueなら永続設定を書けます。 |
| `spec.persistent` | *任意* | boolean | routerd用sysctl.dファイルへkey=valueを保存します。省略時false。runtimeとは独立した指定です。 |
| `spec.optional` | *任意* | boolean | 省略時false。trueなら実行時の読取り失敗をSkipped/ReadFailedOptionalとして扱い、その項目の以降の反映を省きます。任意の書込・永続化エラーを隠す指定ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `key`, `value`, `dryRun`
