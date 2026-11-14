# SysctlProfile

[リソース別の設定](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

ルーター向けのsysctlセットを名前で選び、必要ならキー単位で上書きします。Sysctlリソースの名前を参照する設定ではありません。

## 指定条件と参照関係

- overridesで既存キーの値を変えても、そのキーの比較方式とoptional指定は維持します。新規キーはexact比較・optional=falseで追加します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    [ overrides:
        [ <name>: <string> ]
    ]
    [ persistent: <boolean> ]
    profile: ("router-linux" | "router-freebsd")
    [ runtime: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.profile` | **必須** | string; enum=["router-linux", "router-freebsd"] | 必須の選択値router-linuxまたはrouter-freebsd。OSからの自動選択はありません。FreeBSD用はIPv4/IPv6転送、Linux用は転送に加えてキュー・ソケット・TCP・optionalなconntrack調整を含みます。正確な全キーと値はリンク先の実装を参照してください。 |
| `spec.runtime` | *任意* | boolean | セットの各項目を稼働中のカーネルへ反映します。省略時trueです。 |
| `spec.persistent` | *任意* | boolean | 各項目の永続sysctl.dファイルを作ります。省略時false。runtimeとは独立です。 |
| `spec.overrides` | *任意* | object | カーネルのsysctlキーから空白でない文字列値への対応表。省略時は空です。既存キーの値を置換し、新規キーは追加します。キーの空白や/、空白だけの値は拒否します。リソース参照ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `profile`, `dryRun`
