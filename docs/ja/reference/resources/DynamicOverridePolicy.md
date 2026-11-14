# DynamicOverridePolicy

[リソース別の設定](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

動的設定の有効期間中、指定した取得元が起動設定の特定リソースをmaskできるよう許可します。保存済みの起動設定自体は書き換えません。

## 指定条件と参照関係

- 取得元・操作・対象のapiVersion/kind/nameを完全一致で照合します。該当するallowがなければmaskは拒否します。同じ対象の許可済みmaskが複数あれば、最も遅い期限まで統合します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # 必須; array
      - # object
        operations: # 必須; array
          - "<string>" # string
        source: "<string>" # 必須; string
        targets: # 必須; array
          - # object
            apiVersion: "<string>" # 必須; string
            kind: "<string>" # 必須; string
            name: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.allow` | 許可ルール一覧。空の場合は権限を付与せず、効果はありません。 |
| `spec.allow[].source` | 必須の取得元識別子。DynamicConfigPart.spec.sourceと完全一致で照合します。PluginやDynamicConfigSourceのリソース名とは限りません。ワイルドカード・既定値はありません。 |
| `spec.allow[].operations` | 許可する操作。受理する値はmaskだけで、空の場合は操作を許可しません。 |
| `spec.allow[].targets` | maskの対象にできる起動設定リソースの完全識別子一覧。空なら対象を許可しません。 |
| `spec.allow[].targets[].apiVersion` | 対象のAPIグループとバージョン。各要素で必須、既定値はありません。 |
| `spec.allow[].targets[].kind` | 必須の対象リソース種類。ワイルドカード展開はありません。 |
| `spec.allow[].targets[].name` | 対象のmetadata.nameとの完全一致で使う必須値。ワイルドカード展開はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
