# CloudProviderProfile

[リソース別の設定](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

プロバイダー操作の計画で使う識別情報・対象メタデータの既定値です。この宣言自体はクラウド認証や資源作成を行いません。

## 指定条件と参照関係

- provider、空でないcapabilities、auth.mode・auth.commandが必須です。認証設定は宣言を検証するだけで、現行実行処理はauth.commandを実行しません。プロバイダー変更はProviderActionPolicyに従う別の実行用Pluginが行います。
- Mobilityのcaptureなどの利用側はmetadata.nameでプロファイルを選び、共通の状態値参照ではありません。subscriptionID・resourceGroupは操作対象の値が未指定の場合にだけ補い、明示値を上書きしません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # 必須; object
      command: "<string>" # 任意; string
      mode: "external-command" # 必須; string; enum=["external-command"]
    capabilities: # 必須; array
      - "<string>" # string
    provider: "azure" # 必須; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # 任意; string
    subscriptionID: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.provider` | azure・aws・oci・gcpが必須で、既定値はありません。計画・実行Plugin選択用の識別子で、認証情報を自動検出する指定ではありません。 |
| `spec.subscriptionID` | 対象サブスクリプションIDで、既定は空です。対象のsubscriptionIdが未設定の場合に補い、実在確認やログインは行いません。 |
| `spec.resourceGroup` | 操作対象のresourceGroupの補完値で、既定は空です。資源グループの作成・検証は行いません。 |
| `spec.capabilities` | 空でない文字列一覧が必須で、空要素は不可です。現行の検証は既知の語彙に制限せず、実行ポリシーの操作許可リストとしても扱いません。 |
| `spec.auth` | 必須の認証宣言です。現行実装は検証だけを行い、コマンドを呼び出しません。 |
| `spec.auth.mode` | external-commandが必須で、唯一の許可値です。auth.commandを常駐サービスとして起動するものではありません。 |
| `spec.auth.command` | 空でない宣言が必須で、既定値はありません。現行の実行処理は実行せず、そこから取得した認証情報を渡す処理もありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
