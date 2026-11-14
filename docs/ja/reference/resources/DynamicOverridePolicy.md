# DynamicOverridePolicy

[リソース別の設定](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

動的設定の有効期間中、指定した取得元が起動設定の特定リソースをmaskできるよう許可します。保存済みの起動設定自体は書き換えません。

## 指定条件と参照関係

- 取得元・操作・対象のapiVersion/kind/nameを完全一致で照合します。該当するallowがなければmaskは拒否します。同じ対象の許可済みmaskが複数あれば、最も遅い期限まで統合します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow:
      -
        operations:
          - <string>
        source: <string>
        targets:
          -
            apiVersion: <string>
            kind: <string>
            name: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.allow` | **必須** | array | 許可ルール一覧。空の場合は権限を付与せず、効果はありません。 |
| `spec.allow[].source` | **必須** | string | 必須の取得元識別子。DynamicConfigPart.spec.sourceと完全一致で照合します。PluginやDynamicConfigSourceのリソース名とは限りません。ワイルドカード・既定値はありません。 |
| `spec.allow[].operations` | **必須** | array | 許可する操作。受理する値はmaskだけで、空の場合は操作を許可しません。 |
| `spec.allow[].targets` | **必須** | array | maskの対象にできる起動設定リソースの完全識別子一覧。空なら対象を許可しません。 |
| `spec.allow[].targets[].apiVersion` | **必須** | string | 対象のAPIグループとバージョン。各要素で必須、既定値はありません。 |
| `spec.allow[].targets[].kind` | **必須** | string | 必須の対象リソース種類。ワイルドカード展開はありません。 |
| `spec.allow[].targets[].name` | **必須** | string | 対象のmetadata.nameとの完全一致で使う必須値。ワイルドカード展開はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
