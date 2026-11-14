# CloudProviderProfile

[リソース別の設定](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

プロバイダー操作の計画で使う識別情報・対象メタデータの既定値です。この宣言自体はクラウド認証や資源作成を行いません。

## 指定条件と参照関係

- provider、空でないcapabilities、auth.mode・auth.commandが必須です。認証設定は宣言を検証するだけで、現行実行処理はauth.commandを実行しません。プロバイダー変更はProviderActionPolicyに従う別の実行用Pluginが行います。
- Mobilityのcaptureなどの利用側はmetadata.nameでプロファイルを選び、共通の状態値参照ではありません。subscriptionID・resourceGroupは操作対象の値が未指定の場合にだけ補い、明示値を上書きしません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth:
      command: <string>
      mode: ("external-command")
    capabilities:
      - <string>
    provider: ("azure" | "aws" | "oci" | "gcp")
    [ resourceGroup: <string> ]
    [ subscriptionID: <string> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.provider` | **必須** | string; enum=["azure", "aws", "oci", "gcp"] | azure・aws・oci・gcpが必須で、既定値はありません。計画・実行Plugin選択用の識別子で、認証情報を自動検出する指定ではありません。 |
| `spec.subscriptionID` | *任意* | string | 対象サブスクリプションIDで、既定は空です。対象のsubscriptionIdが未設定の場合に補い、実在確認やログインは行いません。 |
| `spec.resourceGroup` | *任意* | string | 操作対象のresourceGroupの補完値で、既定は空です。資源グループの作成・検証は行いません。 |
| `spec.capabilities` | **必須** | array | 空でない文字列一覧が必須で、空要素は不可です。現行の検証は既知の語彙に制限せず、実行ポリシーの操作許可リストとしても扱いません。 |
| `spec.auth` | **必須** | object | 必須の認証宣言です。現行実装は検証だけを行い、コマンドを呼び出しません。 |
| `spec.auth.mode` | **必須** | string; enum=["external-command"] | external-commandが必須で、唯一の許可値です。auth.commandを常駐サービスとして起動するものではありません。 |
| `spec.auth.command` | **必須** | string | 空でない宣言が必須で、既定値はありません。現行の実行処理は実行せず、そこから取得した認証情報を渡す処理もありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `provider`, `capabilities`
